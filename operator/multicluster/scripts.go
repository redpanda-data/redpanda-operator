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

	// RedpandaAtLeast22_3 indicates whether the Redpanda image is >= v22.3.0.
	RedpandaAtLeast22_3 bool
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
func scriptInternalAdvertiseAddress(state *RenderState, pool *redpandav1alpha2.NodePool) string {
	if state.Spec().Networking.IsMCS() {
		return fmt.Sprintf("${SERVICE_NAME}.%s.svc.clusterset.local", state.namespace)
	}
	// Use per-pod service name pattern: <cluster-pool-name>-<ordinal>.<namespace>
	// This matches the cross-cluster per-pod Service created by the operator
	// (see PerPodServiceName in service_per_pod.go). ${POD_ORDINAL} is
	// derived at script runtime from SERVICE_NAME.
	return fmt.Sprintf("%s-${POD_ORDINAL}.%s", state.poolFullname(pool), state.namespace)
}

// scriptParamsForLifecycle returns script params for lifecycle hooks which
// don't need pool-specific fields like InternalAdvertiseAddress.
func scriptParamsForLifecycle(state *RenderState) ScriptParams {
	return ScriptParams{
		AdminCurlFlags:    state.adminTLSCurlFlags(),
		CurlURL:           state.Spec().AdminInternalURL(state.fullname(), state.namespace),
		TotalReplicas:     state.totalReplicas(),
		AdminHTTPProtocol: state.Spec().AdminInternalHTTPProtocol(),
		AdminAPIURLs:      state.Spec().AdminAPIURLs(state.fullname(), state.namespace),
	}
}

func scriptParamsFromState(state *RenderState, pool *redpandav1alpha2.NodePool) ScriptParams {
	p := ScriptParams{
		AdminCurlFlags:              state.adminTLSCurlFlags(),
		CurlURL:                     state.Spec().AdminInternalURL(state.fullname(), state.namespace),
		TotalReplicas:               state.totalReplicas(),
		InternalAdvertiseAddress:    scriptInternalAdvertiseAddress(state, pool),
		KafkaPort:                   state.Spec().KafkaPort(),
		HTTPPort:                    state.Spec().HTTPPort(),
		RedpandaAtLeast22_3:         state.Spec().Image.AtLeast("22.3.0"),
		RackAwarenessEnabled:        state.Spec().RackAwareness.IsEnabled(),
		RackAwarenessNodeAnnotation: state.Spec().RackAwareness.GetNodeAnnotation(),
		AdminHTTPProtocol:           state.Spec().AdminInternalHTTPProtocol(),
		AdminAPIURLs:                state.Spec().AdminAPIURLs(state.fullname(), state.namespace),
		RPCPort:                     state.Spec().RPCPort(),
	}

	// Collect external Kafka listeners.
	if l := state.Spec().Listeners; l != nil && l.Kafka != nil {
		forEachEnabledExternal(l.Kafka.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			p.ExternalKafkaListeners = append(p.ExternalKafkaListeners, ExternalAdvertisedListener{
				Name: name,
				Port: ext.GetAdvertisedPort(redpandav1alpha2.DefaultExternalKafkaAdvertisedPort),
			})
		})
	}

	// Collect external HTTP/pandaproxy listeners.
	if l := state.Spec().Listeners; l != nil && l.HTTP != nil {
		forEachEnabledExternal(l.HTTP.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			p.ExternalHTTPListeners = append(p.ExternalHTTPListeners, ExternalAdvertisedListener{
				Name: name,
				Port: ext.GetAdvertisedPort(redpandav1alpha2.DefaultExternalHTTPAdvertisedPort),
			})
		})
	}

	return p
}

// lifecycleCommonSh returns the contents of common.sh sourced by lifecycle hooks.
func lifecycleCommonSh(p ScriptParams) string {
	return strings.Join([]string{
		`#!/usr/bin/env bash`,
		``,
		`# the SERVICE_NAME comes from the metadata.name of the pod, essentially the POD_NAME`,
		fmt.Sprintf(`CURL_URL="%s"`, p.CurlURL),
		``,
		`# commands used throughout`,
		fmt.Sprintf(`CURL_NODE_ID_CMD="curl --silent --fail %s ${CURL_URL}/v1/node_config"`, p.AdminCurlFlags),
		``,
		`CURL_MAINTENANCE_DELETE_CMD_PREFIX='curl -X DELETE --silent -o /dev/null -w "%{http_code}"'`,
		`CURL_MAINTENANCE_PUT_CMD_PREFIX='curl -X PUT --silent -o /dev/null -w "%{http_code}"'`,
		fmt.Sprintf(`CURL_MAINTENANCE_GET_CMD="curl -X GET --silent %s ${CURL_URL}/v1/maintenance"`, p.AdminCurlFlags),
	}, "\n")
}

// lifecyclePostStartSh returns the postStart.sh lifecycle hook script.
func lifecyclePostStartSh(p ScriptParams) string {
	return strings.Join([]string{
		`#!/usr/bin/env bash`,
		`# This code should be similar if not exactly the same as that found in the panda-operator, see`,
		`# https://github.com/redpanda-data/redpanda/blob/e51d5b7f2ef76d5160ca01b8c7a8cf07593d29b6/src/go/k8s/pkg/resources/secret.go`,
		``,
		`# path below should match the path defined on the statefulset`,
		`source /var/lifecycle/common.sh`,
		``,
		`postStartHook () {`,
		`  set -x`,
		``,
		`  touch /tmp/postStartHookStarted`,
		``,
		`  until NODE_ID=$(${CURL_NODE_ID_CMD} | grep -o '\"node_id\":[^,}]*' | grep -o '[^: ]*$'); do`,
		`      sleep 0.5`,
		`  done`,
		``,
		`  echo "Clearing maintenance mode on node ${NODE_ID}"`,
		fmt.Sprintf(`  CURL_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"`, p.AdminCurlFlags),
		`  # a 400 here would mean not in maintenance mode`,
		`  until [ "${status:-}" = '"200"' ] || [ "${status:-}" = '"400"' ]; do`,
		`      status=$(${CURL_MAINTENANCE_DELETE_CMD})`,
		`      sleep 0.5`,
		`  done`,
		``,
		`  touch /tmp/postStartHookFinished`,
		`}`,
		``,
		`postStartHook`,
		`true`,
	}, "\n")
}

// lifecyclePreStopSh returns the preStop.sh lifecycle hook script.
func lifecyclePreStopSh(p ScriptParams) string {
	lines := []string{
		`#!/usr/bin/env bash`,
		`# This code should be similar if not exactly the same as that found in the panda-operator, see`,
		`# https://github.com/redpanda-data/redpanda/blob/e51d5b7f2ef76d5160ca01b8c7a8cf07593d29b6/src/go/k8s/pkg/resources/secret.go`,
		``,
		`touch /tmp/preStopHookStarted`,
		``,
		`# path below should match the path defined on the statefulset`,
		`source /var/lifecycle/common.sh`,
		``,
		`set -x`,
		``,
		`preStopHook () {`,
		`  until NODE_ID=$(${CURL_NODE_ID_CMD} | grep -o '\"node_id\":[^,}]*' | grep -o '[^: ]*$'); do`,
		`      sleep 0.5`,
		`  done`,
		``,
		`  echo "Setting maintenance mode on node ${NODE_ID}"`,
		fmt.Sprintf(`  CURL_MAINTENANCE_PUT_CMD="${CURL_MAINTENANCE_PUT_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"`, p.AdminCurlFlags),
		`  until [ "${status:-}" = '"200"' ]; do`,
		`      status=$(${CURL_MAINTENANCE_PUT_CMD})`,
		`      sleep 0.5`,
		`  done`,
		``,
		`  until [ "${finished:-}" = "true" ] || [ "${draining:-}" = "false" ]; do`,
		`      res=$(${CURL_MAINTENANCE_GET_CMD})`,
		`      finished=$(echo $res | grep -o '\"finished\":[^,}]*' | grep -o '[^: ]*$')`,
		`      draining=$(echo $res | grep -o '\"draining\":[^,}]*' | grep -o '[^: ]*$')`,
		`      sleep 0.5`,
		`  done`,
		``,
		`  touch /tmp/preStopHookFinished`,
		`}`,
	}
	// With ≤2 replicas, entering maintenance mode would lose quorum (Raft
	// needs a majority). Skip the drain and let the broker shut down immediately.
	if p.TotalReplicas > 2 {
		lines = append(lines, `preStopHook`)
	} else {
		lines = append(lines,
			`touch /tmp/preStopHookFinished`,
			`echo "Not enough replicas or in recovery mode, cannot put a broker into maintenance mode."`,
		)
	}
	lines = append(lines, `true`)
	return strings.Join(lines, "\n")
}

// configuratorSh returns the configurator.sh init container script. It runs
// once per pod before Redpanda starts and performs per-pod customization of
// the base redpanda.yaml that can't be done at the ConfigMap level:
//   - Sets node_id from the pod ordinal (pre-22.3 only; newer versions auto-assign)
//   - Configures advertised_kafka_api / advertised_pandaproxy_api with the
//     pod's DNS name so other brokers and clients can reach it
//   - Reads the Kubernetes node annotation for rack awareness (if enabled)
func configuratorSh(p ScriptParams) string {
	lines := []string{
		`set -xe`,
		`SERVICE_NAME=$1`,
		`KUBERNETES_NODE_NAME=$2`,
		`POD_ORDINAL=${SERVICE_NAME##*-}`,
		``,
		`CONFIG=/etc/redpanda/redpanda.yaml`,
		``,
		`# Setup config files`,
		`cp /tmp/base-config/redpanda.yaml "${CONFIG}"`,
	}

	if !p.RedpandaAtLeast22_3 {
		lines = append(lines,
			``,
			`# Configure bootstrap`,
			`## Not used for Redpanda v22.3.0+`,
			`rpk --config "${CONFIG}" redpanda config set redpanda.node_id "${POD_ORDINAL}"`,
			`if [ "${POD_ORDINAL}" = "0" ]; then`,
			`	rpk --config "${CONFIG}" redpanda config set redpanda.seed_servers '[]' --format yaml`,
			`fi`,
		)
	}

	// Kafka advertised listeners
	lines = append(lines,
		``,
		fmt.Sprintf(`LISTENER=%s`, tplutil.Quote(tplutil.ToJSON(map[string]any{
			"name":    internalListenerName,
			"address": p.InternalAdvertiseAddress,
			"port":    p.KafkaPort,
		}))),
		`rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[0] "$LISTENER"`,
	)

	// External Kafka advertised listeners
	for i, ext := range p.ExternalKafkaListeners {
		lines = append(lines,
			``,
			fmt.Sprintf(`LISTENER=%s`, tplutil.Quote(tplutil.ToJSON(map[string]any{
				"address": "${SERVICE_NAME}",
				"name":    ext.Name,
				"port":    ext.Port,
			}))),
			fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[%d] "$LISTENER"`, i+1),
		)
	}

	// HTTP/pandaproxy advertised listeners
	lines = append(lines,
		``,
		fmt.Sprintf(`LISTENER=%s`, tplutil.Quote(tplutil.ToJSON(map[string]any{
			"name":    internalListenerName,
			"address": p.InternalAdvertiseAddress,
			"port":    p.HTTPPort,
		}))),
		`rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[0] "$LISTENER"`,
	)

	// External HTTP advertised listeners
	for i, ext := range p.ExternalHTTPListeners {
		lines = append(lines,
			``,
			fmt.Sprintf(`LISTENER=%s`, tplutil.Quote(tplutil.ToJSON(map[string]any{
				"address": "${SERVICE_NAME}",
				"name":    ext.Name,
				"port":    ext.Port,
			}))),
			fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[%d] "$LISTENER"`, i+1),
		)
	}

	// Rack awareness
	if p.RedpandaAtLeast22_3 && p.RackAwarenessEnabled {
		lines = append(lines,
			``,
			`# Configure Rack Awareness`,
			`set +x`,
			fmt.Sprintf(`RACK=$(curl --silent --cacert /run/secrets/kubernetes.io/serviceaccount/ca.crt --fail -H 'Authorization: Bearer '$(cat /run/secrets/kubernetes.io/serviceaccount/token) "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT_HTTPS}/api/v1/nodes/${KUBERNETES_NODE_NAME}?pretty=true" | grep %s | grep -v '\"key\":' | sed 's/.*": "\([^"]\+\).*/\1/')`,
				tplutil.SQuote(tplutil.Quote(p.RackAwarenessNodeAnnotation)),
			),
			`set -x`,
			`rpk --config "$CONFIG" redpanda config set redpanda.rack "${RACK}"`,
		)
	}

	return strings.Join(lines, "\n")
}

// fsValidatorSh is the static filesystem validation script.
const fsValidatorSh = `set -e
EXPECTED_FS_TYPE=$1

DATA_DIR="/var/lib/redpanda/data"
TEST_FILE="testfile"

echo "checking data directory exist..."
if [ ! -d "${DATA_DIR}" ]; then
  echo "data directory does not exists, exiting"
  exit 1
fi

echo "checking filesystem type..."
FS_TYPE=$(df -T $DATA_DIR  | tail -n +2 | awk '{print $2}')

if [ "${FS_TYPE}" != "${EXPECTED_FS_TYPE}" ]; then
  echo "file system found to be ${FS_TYPE} when expected ${EXPECTED_FS_TYPE}"
  exit 1
fi

echo "checking if able to create a test file..."

touch ${DATA_DIR}/${TEST_FILE}
result=$(touch ${DATA_DIR}/${TEST_FILE} 2> /dev/null; echo $?)
if [ "${result}" != "0" ]; then
  echo "could not write testfile, may not have write permission"
  exit 1
fi

echo "checking if able to delete a test file..."

result=$(rm ${DATA_DIR}/${TEST_FILE} 2> /dev/null; echo $?)
if [ "${result}" != "0" ]; then
  echo "could not delete testfile"
  exit 1
fi

echo "passed"`

// wrapLifecycleHook wraps a command in a timeout with timestamped output logging.
func wrapLifecycleHook(hook string, timeoutSeconds int64, cmd []string) []string {
	wrapped := strings.Join(cmd, " ")
	return []string{"bash", "-c", fmt.Sprintf(
		"timeout -v %d %s 2>&1 | sed \"s/^/lifecycle-hook %s $(date): /\" | tee /proc/1/fd/1; true",
		timeoutSeconds, wrapped, hook)}
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

func (r *RenderState) adminTLSCurlFlags() string {
	if !r.Spec().IsAdminTLSEnabled() {
		return ""
	}

	certName := r.Spec().Listeners.AdminCertName()
	if certName == "" {
		return ""
	}

	if r.Spec().Listeners.CertRequiresClientAuth(certName) {
		path := certClientMountPoint(certName)
		return fmt.Sprintf("--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key", path, path, path)
	}

	return fmt.Sprintf("--cacert %s", r.Spec().TLS.CertServerCAPath(certName))
}
