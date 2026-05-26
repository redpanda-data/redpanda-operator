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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// rpkNodeConfig generates the rpk section of the redpanda.yaml template for
// the given pool. Listener ports/TLS come from the pool; tuning/config/auth
// remain cluster-wide.
func rpkNodeConfig(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) map[string]any {
	flags := redpandaAdditionalStartFlags(state, pool)

	l := pool.Spec.Listeners

	result := map[string]any{
		"additional_start_flags": flags,
		"overprovisioned":        pool.Spec.GetOverProvisionValue(),
		"enable_memory_locking":  pool.Spec.GetEnableMemoryLocking(),
		"kafka_api": map[string]any{
			"brokers": state.BrokerList(pool.Spec.KafkaPort()),
			"tls":     rpkListenerTLS(pool, l.Kafka),
		},
		"admin_api": map[string]any{
			"addresses": state.BrokerList(pool.Spec.AdminPort()),
			"tls":       rpkListenerTLS(pool, l.Admin),
		},
		"schema_registry": map[string]any{
			"addresses": state.BrokerList(pool.Spec.SchemaRegistryPort()),
			"tls":       rpkListenerTLS(pool, l.SchemaRegistry),
		},
	}

	// Merge tuning configuration (tune_aio_events, tune_clocksource, etc.).
	for k, v := range tuningToConfiguration(state.Spec().Tuning) {
		result[k] = v
	}

	// Merge user-provided RPK configuration overrides.
	if cfg := state.Spec().Config; cfg != nil {
		mergeRawExtension(result, cfg.RPK)
	}

	return result
}

// rpkListenerTLS returns the rpk client TLS config for a listener, or nil if
// TLS is not enabled or no cert is configured.
func rpkListenerTLS(pool *redpandav1alpha2.RedpandaBrokerPool, listener *redpandav1alpha2.StretchAPIListener) map[string]any {
	if listener == nil || !listener.IsTLSEnabled(pool.Spec.TLS) || listener.TLS.GetCert() == "" {
		return nil
	}
	return rpkClientTLSConfig(pool, listener.TLS)
}

// rpkClientTLSConfig returns the TLS config map for rpk client connections.
func rpkClientTLSConfig(pool *redpandav1alpha2.RedpandaBrokerPool, tls *redpandav1alpha2.StretchListenerTLS) map[string]any {
	certName := tls.GetCert()
	result := map[string]any{
		"ca_file": tls.ServerCAPath(pool.Spec.TLS),
	}
	if pool.Spec.Listeners.CertRequiresClientAuth(certName) {
		clientPath := certClientMountPoint(certName)
		result["cert_file"] = fmt.Sprintf("%s/tls.crt", clientPath)
		result["key_file"] = fmt.Sprintf("%s/tls.key", clientPath)
	}
	return result
}

// kafkaClientConfig generates the pandaproxy_client / schema_registry_client / audit_log_client
// section of the redpanda.yaml template. clientType is "pandaproxy", "schema_registry", or "audit_log".
func kafkaClientConfig(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool, clientType string) map[string]any {
	var brokerList []map[string]any

	// Check if use_localhost is set in node config.
	useLocalhost := state.Spec().NodeConfigBoolValue(fmt.Sprintf("%s_client.use_localhost", clientType))

	if useLocalhost {
		brokerList = append(brokerList, map[string]any{
			"address": "localhost",
			"port":    pool.Spec.KafkaPort(),
		})
	} else {
		for _, addr := range state.BrokerList(pool.Spec.KafkaPort()) {
			// BrokerList returns "host:port" strings; split for the map format.
			brokerList = append(brokerList, map[string]any{
				"address": addr[:strings.LastIndex(addr, ":")],
				"port":    pool.Spec.KafkaPort(),
			})
		}
	}

	cfg := map[string]any{
		"brokers": brokerList,
	}

	// Kafka broker TLS for internal client connections (pandaproxy_client, etc.).
	l := pool.Spec.Listeners
	if kafka := l.Kafka; kafka != nil && kafka.IsTLSEnabled(pool.Spec.TLS) && kafka.TLS.GetCert() != "" {
		tls := kafka.TLS
		certName := tls.GetCert()
		brokerTLS := map[string]any{
			"enabled":             true,
			"require_client_auth": pool.Spec.Listeners.CertRequiresClientAuth(certName),
			"truststore_file":     tls.ServerCAPath(pool.Spec.TLS),
		}
		if pool.Spec.Listeners.CertRequiresClientAuth(certName) {
			clientPath := certClientMountPoint(certName)
			brokerTLS["cert_file"] = fmt.Sprintf("%s/tls.crt", clientPath)
			brokerTLS["key_file"] = fmt.Sprintf("%s/tls.key", clientPath)
		}
		cfg["broker_tls"] = brokerTLS
	}

	return cfg
}

// redpandaAdditionalStartFlags returns the additional_start_flags for rpk.
func redpandaAdditionalStartFlags(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []string {
	var flags []string

	// Add logging level.
	if log := state.Spec().Logging; log != nil && log.LogLevel != nil {
		flags = append(flags, fmt.Sprintf("--default-log-level=%s", *log.LogLevel))
	}

	// Add resource-derived flags in deterministic order. Pool wins over
	// cluster (merged in lifecycle.defaultedPoolCopy via MergeFromCluster).
	redpandaFlags := pool.Spec.GetRedpandaStartFlags()
	for _, key := range []string{"--memory", "--reserve-memory", "--smp"} {
		if v, ok := redpandaFlags[key]; ok {
			flags = append(flags, fmt.Sprintf("%s=%s", key, v))
		}
	}

	// Merge in pool-specific flags.
	for _, flag := range pool.Spec.AdditionalRedpandaCmdFlags {
		if !strings.HasPrefix(flag, "--lock-memory") && !strings.HasPrefix(flag, "--overprovisioned") {
			flags = append(flags, flag)
		}
	}

	return flags
}

// rpkProfileConfigMapForPool returns a ConfigMap containing an RPK profile for
// external client connections for a single local pool. Returns nil if external
// access is not enabled for this pool. One profile CM per pool, named
// <cluster>-<pool>-rpk.
func rpkProfileConfigMapForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.ConfigMap {
	if !pool.Spec.External.IsEnabled() {
		return nil
	}

	profile := rpkProfile(pool)

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-rpk", state.poolFullname(pool)),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Data: map[string]string{
			"profile": tplutil.ToYaml(profile),
		},
	}
}

// rpkProfileConfigMap wrapper iterates local pools so the umbrella
// `configMaps()` / RenderResources stays backward compatible.
func rpkProfileConfigMap(state *RenderState) []*corev1.ConfigMap {
	var out []*corev1.ConfigMap
	for _, pool := range state.inClusterPools {
		if cm := rpkProfileConfigMapForPool(state, pool); cm != nil {
			out = append(out, cm)
		}
	}
	return out
}

// rpkProfile generates the RPK profile data for external client connections
// for the given pool.
func rpkProfile(pool *redpandav1alpha2.RedpandaBrokerPool) map[string]any {
	// For stretch clusters, the advertised addresses are runtime-dependent
	// (per-node), so we use empty lists here. The profile is primarily useful
	// for its TLS configuration and name.
	brokerList := []string{}
	adminList := []string{}
	schemaList := []string{}

	// Use the first external kafka listener name (sorted) for the profile name.
	profileName := "default"
	if l := pool.Spec.Listeners; l != nil && l.Kafka != nil {
		if names := sortedMapKeys(l.Kafka.External); len(names) > 0 {
			profileName = names[0]
		}
	}

	l := pool.Spec.Listeners

	return map[string]any{
		"name":            profileName,
		"kafka_api":       rpkProfileEntry("brokers", brokerList, pool.Spec.TLS, l.Kafka),
		"admin_api":       rpkProfileEntry("addresses", adminList, pool.Spec.TLS, l.Admin),
		"schema_registry": rpkProfileEntry("addresses", schemaList, pool.Spec.TLS, l.SchemaRegistry),
	}
}

// rpkProfileEntry builds a profile section (kafka_api, admin_api, schema_registry)
// with optional TLS. The addressKey is "brokers" for kafka or "addresses" for others.
func rpkProfileEntry(addressKey string, addresses []string, globalTLS *redpandav1alpha2.TLS, listener *redpandav1alpha2.StretchAPIListener) map[string]any {
	var tls any
	if listener != nil && listener.IsTLSEnabled(globalTLS) && listener.TLS.GetCert() != "" {
		tls = map[string]any{"ca_file": "ca.crt"}
	}
	return map[string]any{
		addressKey: addresses,
		"tls":      tls,
	}
}
