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
func redpandaConfigFile(state *RenderState, includeSeedServers bool, pool *redpandav1alpha2.RedpandaBrokerPool) (string, error) {
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

	listeners := poolListeners(state, pool)

	sections := listeners.ConfigSections()

	for _, key := range sortedMapKeys(sections["redpanda"]) {
		redpanda[key] = sections["redpanda"][key]
	}

	redpandaYaml := map[string]any{
		"redpanda":        redpanda,
		"schema_registry": sections["schema_registry"],
		"pandaproxy":      sections["pandaproxy"],
		"config_file":     redpandaConfigMountPath + "/redpanda.yaml",
	}

	// Client configs (pandaproxy_client, schema_registry_client, audit_log_client)
	// are only included alongside seed_servers because they reference internal
	// broker addresses that depend on the seed server list.
	if includeSeedServers {
		redpandaYaml["rpk"] = rpkNodeConfig(state, pool)

		pandaproxyClient := kafkaClientConfig(state, pool, "pandaproxy")
		schemaRegistryClient := kafkaClientConfig(state, pool, "schema_registry")

		// Merge user-provided client configuration overrides.
		if cfg := state.Spec().Config; cfg != nil {
			mergeRawExtension(pandaproxyClient, cfg.PandaProxyClient)
			mergeRawExtension(schemaRegistryClient, cfg.SchemaRegistryClient)
		}

		redpandaYaml["pandaproxy_client"] = pandaproxyClient
		redpandaYaml["schema_registry_client"] = schemaRegistryClient

		if state.Spec().IsAuditLoggingEnabled() {
			redpandaYaml["audit_log_client"] = kafkaClientConfig(state, pool, "audit_log")
		}
	}

	return tplutil.ToYaml(redpandaYaml), nil
}
