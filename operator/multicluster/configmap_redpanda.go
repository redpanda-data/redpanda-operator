// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"
	"strconv"
	"strings"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// redpandaConfigFile generates the redpanda.yaml configuration template.
// When includeSeedServers is true, the full config (seed servers, client configs,
// rpk config) is emitted — this is the version baked into the ConfigMap that pods
// mount. When false, a minimal config without seed servers is generated for the
// checksum annotation, so that replica count changes don't trigger rolling restarts.
func redpandaConfigFile(state *RenderState, includeSeedServers bool, pool *redpandav1alpha2.NodePool) (string, error) {
	redpanda := map[string]any{
		"empty_seed_starts_cluster": false,
		"crash_loop_limit":          5,
	}

	if includeSeedServers {
		var servers []map[string]any
		for _, server := range state.seedServers {
			address, port, _ := strings.Cut(server, ":")
			portInt, err := strconv.ParseInt(port, 10, 0)
			if err != nil {
				return "", err
			}
			servers = append(servers, map[string]any{
				"host": map[string]any{
					"address": address,
					"port":    portInt,
				},
			})
		}
		redpanda["seed_servers"] = servers
	}

	// Merge node config from CRD.
	for k, v := range state.Spec().GetNodeConfig() {
		redpanda[k] = v
	}

	// Configure listeners.
	configureListeners(redpanda, state)

	redpandaYaml := map[string]any{
		"redpanda":        redpanda,
		"schema_registry": schemaRegistryConfig(state),
		"pandaproxy":      pandaProxyConfig(state),
		"config_file":     redpandaConfigMountPath + "/redpanda.yaml",
	}

	// Client configs (pandaproxy_client, schema_registry_client, audit_log_client)
	// are only included alongside seed_servers because they reference internal
	// broker addresses that depend on the seed server list.
	if includeSeedServers {
		redpandaYaml["rpk"] = rpkNodeConfig(state, pool)

		pandaproxyClient := kafkaClientConfig(state, "pandaproxy")
		schemaRegistryClient := kafkaClientConfig(state, "schema_registry")

		// Merge user-provided client configuration overrides.
		if cfg := state.Spec().Config; cfg != nil {
			mergeRawExtension(pandaproxyClient, cfg.PandaProxyClient)
			mergeRawExtension(schemaRegistryClient, cfg.SchemaRegistryClient)
		}

		redpandaYaml["pandaproxy_client"] = pandaproxyClient
		redpandaYaml["schema_registry_client"] = schemaRegistryClient

		if state.Spec().IsAuditLoggingEnabled() {
			redpandaYaml["audit_log_client"] = kafkaClientConfig(state, "audit_log")
		}
	}

	return tplutil.ToYaml(redpandaYaml), nil
}

// configureListeners populates the listener entries in the redpanda config section.
func configureListeners(redpanda map[string]any, state *RenderState) {
	l := state.Spec().Listeners

	var admin, kafka *redpandav1alpha2.StretchAPIListener
	if l != nil {
		admin = l.Admin
		kafka = l.Kafka
	}

	// Admin listener.
	configureAPIListener(redpanda, state, admin, "admin", "admin_api_tls", state.Spec().AdminPort(), redpandav1alpha2.DefaultExternalAdminPort, "")

	// Kafka listener.
	authMethod := ""
	if state.Spec().Auth.IsSASLEnabled() {
		authMethod = "sasl"
	}
	configureAPIListener(redpanda, state, kafka, "kafka_api", "kafka_api_tls", state.Spec().KafkaPort(), redpandav1alpha2.DefaultExternalKafkaPort, authMethod)

	// RPC listener.
	redpanda["rpc_server"] = map[string]any{
		"address": "0.0.0.0",
		"port":    state.Spec().RPCPort(),
	}
	if l != nil && l.RPC.IsTLSEnabled(state.Spec().TLS) && l.RPC.TLS != nil && l.RPC.TLS.GetCert() != "" {
		certName := l.RPC.TLS.GetCert()
		redpanda["rpc_server_tls"] = map[string]any{
			"enabled":             true,
			"cert_file":           fmt.Sprintf("%s/tls.crt", certServerMountPoint(certName)),
			"key_file":            fmt.Sprintf("%s/tls.key", certServerMountPoint(certName)),
			"require_client_auth": l.RPC.TLS.RequiresClientAuth(),
			"truststore_file":     l.RPC.TLS.ServerCAPath(state.Spec().TLS),
		}
	}
}

// configureAPIListener handles the common pattern for Admin, Kafka, HTTP, and SchemaRegistry
// listeners: creates the internal listener entry, adds TLS if enabled, iterates external
// listeners, and sets the results on the redpanda config map.
func configureAPIListener(
	redpanda map[string]any,
	state *RenderState,
	listener *redpandav1alpha2.StretchAPIListener,
	listenerKey, tlsKey string,
	internalPort, defaultExtPort int32,
	authMethod string,
) {
	internal := map[string]any{
		"name": internalListenerName, "address": "0.0.0.0", "port": internalPort,
	}
	if authMethod != "" {
		internal["authentication_method"] = authMethod
	}

	listeners := []map[string]any{internal}
	var tlsEntries []map[string]any

	if listener != nil {
		if listener.IsTLSEnabled(state.Spec().TLS) && listener.TLS.GetCert() != "" {
			tlsEntries = append(tlsEntries, listenerTLSEntry(state, internalListenerName, listener.TLS))
		}
		forEachEnabledExternal(listener.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			entry := map[string]any{
				"name": name, "address": "0.0.0.0", "port": ext.GetPort(defaultExtPort),
			}
			if authMethod != "" {
				entry["authentication_method"] = authMethod
			}
			listeners = append(listeners, entry)
			if ext.TLS.GetCert() != "" {
				tlsEntries = append(tlsEntries, listenerTLSEntry(state, name, ext.TLS))
			}
		})
	}

	redpanda[listenerKey] = listeners
	if len(tlsEntries) > 0 {
		redpanda[tlsKey] = tlsEntries
	}
}

// listenerTLSEntry returns the TLS config map for a named listener.
func listenerTLSEntry(state *RenderState, name string, tls *redpandav1alpha2.StretchListenerTLS) map[string]any {
	certName := tls.GetCert()
	return map[string]any{
		"name":                name,
		"enabled":             true,
		"cert_file":           fmt.Sprintf("%s/tls.crt", certServerMountPoint(certName)),
		"key_file":            fmt.Sprintf("%s/tls.key", certServerMountPoint(certName)),
		"require_client_auth": state.Spec().Listeners.CertRequiresClientAuth(certName),
		"truststore_file":     tls.ServerCAPath(state.Spec().TLS),
	}
}
