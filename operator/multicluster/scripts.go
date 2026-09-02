// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// This file centralizes all shell script generation for auditability.
// Every bash/sh script embedded in Secrets or container commands is
// generated here so that security-sensitive string interpolation is
// visible in a single place.

package multicluster

import (
	"fmt"
	"strings"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// NOTE: This file contains a bunch of shell scripts that really should be
// first class citizens written in Gowithin the operator codebase.

// ScriptParams holds the values that get interpolated into shell scripts.
// Centralizing these makes it easy to audit what dynamic values flow into bash.
// ExternalAdvertisedListener holds the info needed to generate an external
// advertised listener in the configurator script.
type ExternalAdvertisedListener struct {
	Name string
	Port int32
}

type ScriptParams struct {
	// AdminCurlFlags are the TLS flags for curl (e.g. "--cacert /path/ca.crt").
	AdminCurlFlags string
	// CurlURL is the admin API base URL template (contains ${SERVICE_NAME}).
	CurlURL string
	// TotalReplicas is the number of replicas across all pools.
	TotalReplicas int32

	// InternalAdvertiseAddress is the pod DNS address template (contains ${SERVICE_NAME}).
	InternalAdvertiseAddress string
	// KafkaPort is the internal Kafka listener port.
	KafkaPort int32
	// HTTPPort is the internal HTTP proxy listener port.
	HTTPPort int32

	// ExternalKafkaListeners are the external Kafka listeners to advertise.
	ExternalKafkaListeners []ExternalAdvertisedListener
	// ExternalHTTPListeners are the external HTTP/pandaproxy listeners to advertise.
	ExternalHTTPListeners []ExternalAdvertisedListener

	// RackAwarenessEnabled indicates whether rack awareness should be configured.
	RackAwarenessEnabled bool
	// RackAwarenessNodeAnnotation is the K8s node annotation key for rack detection.
	RackAwarenessNodeAnnotation string

	// AdminHTTPProtocol is "http" or "https".
	AdminHTTPProtocol string
	// AdminAPIURLs is the admin API host:port for probes (contains ${SERVICE_NAME}).
	AdminAPIURLs string
	// RPCPort is the internal RPC port.
	RPCPort int32
}

// scriptParamsFromState extracts all values needed for script generation from a RenderState.
// scriptInternalAdvertiseAddress returns the advertised address template for
// internal listeners. In MCS mode, uses the clusterset.local domain.
// For mesh/flat modes, uses the per-pod service name (<pool>-<ordinal>)
// which is resolvable across clusters, rather than the StatefulSet pod FQDN
// which only resolves within the local cluster.
func scriptInternalAdvertiseAddress(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) string {
	if state.Spec().Networking.IsMCS() {
		return fmt.Sprintf("${SERVICE_NAME}.%s.svc.clusterset.local", state.namespace)
	}
	// Use per-pod service name pattern: <cluster-pool-name>-<ordinal>.<namespace>
	// This matches the cross-cluster per-pod Service created by the operator
	// (see PerPodServiceName in service_per_pod.go). ${POD_ORDINAL} is
	// derived at script runtime from SERVICE_NAME.
	return fmt.Sprintf("%s-${POD_ORDINAL}.%s", state.poolFullname(pool), state.namespace)
}

// scriptParamsForLifecycle returns script params for lifecycle hooks for the
// given pool. The lifecycle scripts are mounted per-pool so admin URL / TLS
// flags / protocol come from this pool's TLS/Listeners/ClusterDomain.
func scriptParamsForLifecycle(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ScriptParams {
	return ScriptParams{
		AdminCurlFlags:           poolAdminTLSCurlFlags(pool),
		CurlURL:                  pool.Spec.AdminInternalURL(state.fullname(), state.namespace),
		TotalReplicas:            state.totalReplicas(),
		AdminHTTPProtocol:        pool.Spec.AdminInternalHTTPProtocol(),
		AdminAPIURLs:             pool.Spec.AdminAPIURLs(state.fullname(), state.namespace),
		InternalAdvertiseAddress: scriptInternalAdvertiseAddress(state, pool),
	}
}

func scriptParamsFromState(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ScriptParams {
	p := ScriptParams{
		AdminCurlFlags:              poolAdminTLSCurlFlags(pool),
		CurlURL:                     pool.Spec.AdminInternalURL(state.fullname(), state.namespace),
		TotalReplicas:               state.totalReplicas(),
		InternalAdvertiseAddress:    scriptInternalAdvertiseAddress(state, pool),
		KafkaPort:                   pool.Spec.KafkaPort(),
		HTTPPort:                    pool.Spec.HTTPPort(),
		RackAwarenessEnabled:        pool.Spec.RackAwareness.IsEnabled(),
		RackAwarenessNodeAnnotation: pool.Spec.RackAwareness.GetNodeAnnotation(),
		AdminHTTPProtocol:           pool.Spec.AdminInternalHTTPProtocol(),
		AdminAPIURLs:                pool.Spec.AdminAPIURLs(state.fullname(), state.namespace),
		RPCPort:                     pool.Spec.RPCPort(),
	}

	// Collect external Kafka listeners.
	if l := pool.Spec.Listeners; l != nil && l.Kafka != nil {
		forEachEnabledExternal(l.Kafka.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			p.ExternalKafkaListeners = append(p.ExternalKafkaListeners, ExternalAdvertisedListener{
				Name: name,
				Port: ext.GetAdvertisedPort(redpandav1alpha2.DefaultExternalKafkaAdvertisedPort),
			})
		})
	}

	// Collect external HTTP/pandaproxy listeners.
	if l := pool.Spec.Listeners; l != nil && l.HTTP != nil {
		forEachEnabledExternal(l.HTTP.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			p.ExternalHTTPListeners = append(p.ExternalHTTPListeners, ExternalAdvertisedListener{
				Name: name,
				Port: ext.GetAdvertisedPort(redpandav1alpha2.DefaultExternalHTTPAdvertisedPort),
			})
		})
	}

	return p
}

// configuratorSh returns the configurator.sh init container script. It runs
// once per pod before Redpanda starts and performs per-pod customization of
// the base redpanda.yaml that can't be done at the ConfigMap level:
//   - Configures advertised_kafka_api / advertised_pandaproxy_api with the
//     pod's DNS name so other brokers and clients can reach it
//   - Reads the Kubernetes node annotation for rack awareness (if enabled)
func configuratorSh(p ScriptParams) string {
	lines := redpanda.ConfiguratorPrologueSh()

	// Kafka advertised listeners
	lines = append(lines,
		``,
		fmt.Sprintf(`LISTENER=%q`, tplutil.ToJSON(map[string]any{
			"name":    internalListenerName,
			"address": p.InternalAdvertiseAddress,
			"port":    p.KafkaPort,
		})),
		`rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[0] "$LISTENER"`,
	)

	// External Kafka advertised listeners
	for i, ext := range p.ExternalKafkaListeners {
		lines = append(lines,
			``,
			fmt.Sprintf(`LISTENER=%q`, tplutil.ToJSON(map[string]any{
				"address": "${SERVICE_NAME}",
				"name":    ext.Name,
				"port":    ext.Port,
			})),
			fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[%d] "$LISTENER"`, i+1),
		)
	}

	// HTTP/pandaproxy advertised listeners
	lines = append(lines,
		``,
		fmt.Sprintf(`LISTENER=%q`, tplutil.ToJSON(map[string]any{
			"name":    internalListenerName,
			"address": p.InternalAdvertiseAddress,
			"port":    p.HTTPPort,
		})),
		`rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[0] "$LISTENER"`,
	)

	// External HTTP advertised listeners
	for i, ext := range p.ExternalHTTPListeners {
		lines = append(lines,
			``,
			fmt.Sprintf(`LISTENER=%q`, tplutil.ToJSON(map[string]any{
				"address": "${SERVICE_NAME}",
				"name":    ext.Name,
				"port":    ext.Port,
			})),
			fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[%d] "$LISTENER"`, i+1),
		)
	}

	// Rack awareness
	if p.RackAwarenessEnabled {
		lines = append(lines, redpanda.ConfiguratorRackAwarenessSh(p.RackAwarenessNodeAnnotation)...)
	}

	return strings.Join(lines, "\n")
}

// startupProbeScript returns the shell command for the Redpanda startup probe.
func startupProbeScript(p ScriptParams) string {
	return strings.Join([]string{
		`set -e`,
		fmt.Sprintf(`RESULT=$(curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready")`,
			p.AdminCurlFlags,
			p.AdminHTTPProtocol,
			p.AdminAPIURLs,
		),
		`echo $RESULT`,
		`echo $RESULT | grep ready`,
		``,
	}, "\n")
}

// livenessProbeScript returns the shell command for the Redpanda liveness probe.
func livenessProbeScript(p ScriptParams) string {
	return fmt.Sprintf(`curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready"`,
		p.AdminCurlFlags,
		p.AdminHTTPProtocol,
		p.AdminAPIURLs,
	)
}

// poolAdminTLSCurlFlags returns curl flags for the pool's admin listener TLS.
// Reads TLS and Listeners from the pool's spec.
func poolAdminTLSCurlFlags(pool *redpandav1alpha2.RedpandaBrokerPool) string {
	if !pool.Spec.IsAdminTLSEnabled() {
		return ""
	}

	certName := pool.Spec.Listeners.AdminCertName()
	if certName == "" {
		return ""
	}

	if pool.Spec.Listeners.CertRequiresClientAuth(certName) {
		path := certClientMountPoint(certName)
		return fmt.Sprintf("--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key", path, path, path)
	}

	return fmt.Sprintf("--cacert %s", pool.Spec.TLS.CertServerCAPath(certName))
}
