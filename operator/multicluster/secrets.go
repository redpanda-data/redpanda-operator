// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_secrets.go.tpl
package redpanda

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const DefaultSASLMechanism = SASLMechanism("SCRAM-SHA-512")

func Secrets(state *RenderState) []*corev1.Secret {
	var secrets []*corev1.Secret
	secrets = append(secrets, SecretSTSLifecycle(state))
	if saslUsers := SecretSASLUsers(state); saslUsers != nil {
		secrets = append(secrets, saslUsers)
	}
	secrets = append(secrets, SecretConfigurator(state, Pool{Statefulset: state.Values.Statefulset}))
	if fsValidator := SecretFSValidator(state, Pool{Statefulset: state.Values.Statefulset}); fsValidator != nil {
		secrets = append(secrets, fsValidator)
	}
	for _, set := range state.Pools {
		secrets = append(secrets, SecretConfigurator(state, set))
		if fsValidator := SecretFSValidator(state, set); fsValidator != nil {
			secrets = append(secrets, fsValidator)
		}
	}
	if bootstrapUser := SecretBootstrapUser(state); bootstrapUser != nil {
		secrets = append(secrets, bootstrapUser)
	}
	return secrets
}

func SecretSTSLifecycle(state *RenderState) *corev1.Secret {
	replicas := state.Values.Statefulset.Replicas
	for _, set := range state.Pools {
		replicas = replicas + set.Statefulset.Replicas
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sts-lifecycle", Fullname(state)),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{},
	}
	adminCurlFlags := adminTLSCurlFlags(state)
	secret.StringData["common.sh"] = helmette.Join("\n", []string{
		`#!/usr/bin/env bash`,
		``,
		`# the SERVICE_NAME comes from the metadata.name of the pod, essentially the POD_NAME`,
		fmt.Sprintf(`CURL_URL="%s"`, adminInternalURL(state)),
		``,
		`# commands used throughout`,
		fmt.Sprintf(`CURL_NODE_ID_CMD="curl --silent --fail %s ${CURL_URL}/v1/node_config"`, adminCurlFlags),
		``,
		`CURL_MAINTENANCE_DELETE_CMD_PREFIX='curl -X DELETE --silent -o /dev/null -w "%{http_code}"'`,
		`CURL_MAINTENANCE_PUT_CMD_PREFIX='curl -X PUT --silent -o /dev/null -w "%{http_code}"'`,
		fmt.Sprintf(`CURL_MAINTENANCE_GET_CMD="curl -X GET --silent %s ${CURL_URL}/v1/maintenance"`, adminCurlFlags),
	})

	postStartSh := []string{
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
		fmt.Sprintf(`  CURL_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"`, adminCurlFlags),
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
	}
	secret.StringData["postStart.sh"] = helmette.Join("\n", postStartSh)

	preStopSh := []string{
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
		fmt.Sprintf(`  CURL_MAINTENANCE_PUT_CMD="${CURL_MAINTENANCE_PUT_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"`, adminCurlFlags),
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
	if replicas > 2 && !helmette.Dig(state.Values.Config.Node, false, "recovery_mode_enabled").(bool) {
		preStopSh = append(preStopSh,
			`preStopHook`,
		)
	} else {
		preStopSh = append(preStopSh,
			`touch /tmp/preStopHookFinished`,
			`echo "Not enough replicas or in recovery mode, cannot put a broker into maintenance mode."`,
		)
	}
	preStopSh = append(preStopSh,
		`true`,
	)
	secret.StringData["preStop.sh"] = helmette.Join("\n", preStopSh)
	return secret
}

func SecretSASLUsers(state *RenderState) *corev1.Secret {
	if state.Values.Auth.SASL.SecretRef != "" && state.Values.Auth.SASL.Enabled && len(state.Values.Auth.SASL.Users) > 0 {
		secret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      state.Values.Auth.SASL.SecretRef,
				Namespace: state.Release.Namespace,
				Labels:    FullLabels(state),
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{},
		}
		usersTxt := []string{}

		defaultMechanism := DefaultSASLMechanism
		if state.Values.Auth.SASL.Mechanism != "" {
			defaultMechanism = state.Values.Auth.SASL.Mechanism
		}

		// Working around lack of support for += or strings.Join at the moment
		for _, user := range state.Values.Auth.SASL.Users {
			mechanism := ptr.Deref(user.Mechanism, defaultMechanism)
			usersTxt = append(usersTxt, fmt.Sprintf("%s:%s:%s", user.Name, user.Password, mechanism))
		}
		secret.StringData["users.txt"] = helmette.Join("\n", usersTxt)
		return secret
	} else if state.Values.Auth.SASL.Enabled && state.Values.Auth.SASL.SecretRef == "" {
		panic("auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true")
	} else {
		// XXX no secret generated when enabled, we have a secret ref, but we have no users
		return nil
	}
}

func SecretBootstrapUser(state *RenderState) *corev1.Secret {
	if !state.Values.Auth.SASL.Enabled || state.Values.Auth.SASL.BootstrapUser.SecretKeyRef != nil {
		return nil
	}

	secretName := fmt.Sprintf("%s-bootstrap-user", Fullname(state))

	if state.BootstrapUserSecret != nil {
		return state.BootstrapUserSecret
	}

	password := helmette.RandAlphaNum(32)

	userPassword := state.Values.Auth.SASL.BootstrapUser.Password
	if userPassword != nil {
		password = *userPassword
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Immutable: ptr.To(true),
		Type:      corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": password,
		},
	}
}

func SecretFSValidator(state *RenderState, pool Pool) *corev1.Secret {
	if !pool.Statefulset.InitContainers.FSValidator.Enabled {
		return nil
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%.49s-fs-validator", fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{},
	}

	secret.StringData["fsValidator.sh"] = `set -e
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
	return secret
}

func SecretConfigurator(state *RenderState, pool Pool) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%.51s-configurator", fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{},
	}
	configuratorSh := []string{}
	configuratorSh = append(configuratorSh,
		`set -xe`,
		`SERVICE_NAME=$1`,
		`KUBERNETES_NODE_NAME=$2`,
		`POD_ORDINAL=${SERVICE_NAME##*-}`,
		"BROKER_INDEX=`expr $POD_ORDINAL + 1`", // POSIX sh-safe
		``,
		`CONFIG=/etc/redpanda/redpanda.yaml`,
		``,
		`# Setup config files`,
		`cp /tmp/base-config/redpanda.yaml "${CONFIG}"`,
	)
	if !RedpandaAtLeast_22_3_0(state) {
		configuratorSh = append(configuratorSh,
			``,
			`# Configure bootstrap`,
			`## Not used for Redpanda v22.3.0+`,
			`rpk --config "${CONFIG}" redpanda config set redpanda.node_id "${POD_ORDINAL}"`,
			`if [ "${POD_ORDINAL}" = "0" ]; then`,
			`	rpk --config "${CONFIG}" redpanda config set redpanda.seed_servers '[]' --format yaml`,
			`fi`,
		)
	}

	kafkaSnippet := secretConfiguratorKafkaConfig(state, pool.Statefulset)
	configuratorSh = append(configuratorSh, kafkaSnippet...)

	httpSnippet := secretConfiguratorHTTPConfig(state, pool.Statefulset)
	configuratorSh = append(configuratorSh, httpSnippet...)

	if RedpandaAtLeast_22_3_0(state) && state.Values.RackAwareness.Enabled {
		configuratorSh = append(configuratorSh,
			``,
			`# Configure Rack Awareness`,
			`set +x`,
			fmt.Sprintf(`RACK=$(curl --silent --cacert /run/secrets/kubernetes.io/serviceaccount/ca.crt --fail -H 'Authorization: Bearer '$(cat /run/secrets/kubernetes.io/serviceaccount/token) "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT_HTTPS}/api/v1/nodes/${KUBERNETES_NODE_NAME}?pretty=true" | grep %s | grep -v '\"key\":' | sed 's/.*": "\([^"]\+\).*/\1/')`,
				helmette.SQuote(helmette.Quote(state.Values.RackAwareness.NodeAnnotation)),
			),
			`set -x`,
			`rpk --config "$CONFIG" redpanda config set redpanda.rack "${RACK}"`,
		)
	}
	secret.StringData["configurator.sh"] = helmette.Join("\n", configuratorSh)
	return secret
}

func secretConfiguratorKafkaConfig(state *RenderState, sts Statefulset) []string {
	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "${SERVICE_NAME}", InternalDomain(state))

	var snippet []string

	// Handle kafka listener
	listenerName := "kafka"
	listenerAdvertisedName := listenerName
	redpandaConfigPart := "redpanda"
	snippet = append(snippet,
		``,
		fmt.Sprintf(`LISTENER=%s`, helmette.Quote(helmette.ToJSON(map[string]any{
			"name":    "internal",
			"address": internalAdvertiseAddress,
			"port":    state.Values.Listeners.Kafka.Port,
		}))),
		fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"`,
			redpandaConfigPart,
			listenerAdvertisedName,
		),
	)
	if len(state.Values.Listeners.Kafka.External) > 0 {
		externalCounter := 0
		for externalName, externalVals := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
			externalCounter = externalCounter + 1
			snippet = append(snippet,
				``,
				fmt.Sprintf(`ADVERTISED_%s_ADDRESSES=()`, helmette.Upper(listenerName)),
			)
			// TODO: this looks quite broken just based on the fact that if replicas > addresses
			for _, replicaIndex := range helmette.Until(int(sts.Replicas)) {
				// advertised-port for kafka
				port := externalVals.Port // This is always defined for kafka
				if len(externalVals.AdvertisedPorts) > 0 {
					if len(externalVals.AdvertisedPorts) == 1 {
						port = externalVals.AdvertisedPorts[0]
					} else {
						port = externalVals.AdvertisedPorts[replicaIndex]
					}
				}

				host := advertisedHostJSON(state, externalName, port, replicaIndex)
				// XXX: the original code used the stringified `host` value as a template
				// for re-expansion; however it was impossible to make this work usefully,
				/// even with the original yaml template.
				address := helmette.ToJSON(host)
				prefixTemplate := ptr.Deref(externalVals.PrefixTemplate, "")
				if prefixTemplate == "" {
					// Required because the values might not specify this, it'll ensur we see "" if it's missing.
					prefixTemplate = helmette.Default("", state.Values.External.PrefixTemplate)
				}
				snippet = append(snippet,
					``,
					fmt.Sprintf(`PREFIX_TEMPLATE=%s`, helmette.Quote(prefixTemplate)),
					fmt.Sprintf(`ADVERTISED_%s_ADDRESSES+=(%s)`,
						helmette.Upper(listenerName),
						helmette.Quote(address),
					),
				)
			}

			snippet = append(snippet,
				``,
				fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"`,
					redpandaConfigPart,
					listenerAdvertisedName,
					externalCounter,
					helmette.Upper(listenerName),
				),
			)
		}
	}

	return snippet
}

func secretConfiguratorHTTPConfig(state *RenderState, sts Statefulset) []string {
	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "${SERVICE_NAME}", InternalDomain(state))

	var snippet []string

	// Handle kafka listener
	listenerName := "http"
	listenerAdvertisedName := "pandaproxy"
	redpandaConfigPart := "pandaproxy"
	snippet = append(snippet,
		``,
		fmt.Sprintf(`LISTENER=%s`, helmette.Quote(helmette.ToJSON(map[string]any{
			"name":    "internal",
			"address": internalAdvertiseAddress,
			"port":    state.Values.Listeners.HTTP.Port,
		}))),
		fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"`,
			redpandaConfigPart,
			listenerAdvertisedName,
		),
	)
	if len(state.Values.Listeners.HTTP.External) > 0 {
		externalCounter := 0
		for externalName, externalVals := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
			externalCounter = externalCounter + 1
			snippet = append(snippet,
				``,
				fmt.Sprintf(`ADVERTISED_%s_ADDRESSES=()`, helmette.Upper(listenerName)),
			)
			// TODO: this looks quite broken just based on the fact that if replicas > addresses
			for _, replicaIndex := range helmette.Until(int(sts.Replicas)) {
				// advertised-port for kafka
				port := externalVals.Port // This is always defined for kafka
				if len(externalVals.AdvertisedPorts) > 0 {
					if len(externalVals.AdvertisedPorts) == 1 {
						port = externalVals.AdvertisedPorts[0]
					} else {
						port = externalVals.AdvertisedPorts[replicaIndex]
					}
				}

				host := advertisedHostJSON(state, externalName, port, replicaIndex)
				// XXX: the original code used the stringified `host` value as a template
				// for re-expansion; however it was impossible to make this work usefully,
				/// even with the original yaml template.
				address := helmette.ToJSON(host)

				prefixTemplate := ptr.Deref(externalVals.PrefixTemplate, "")
				if prefixTemplate == "" {
					// Required because the values might not specify this, it'll ensur we see "" if it's missing.
					prefixTemplate = helmette.Default("", state.Values.External.PrefixTemplate)
				}
				snippet = append(snippet,
					``,
					fmt.Sprintf(`PREFIX_TEMPLATE=%s`, helmette.Quote(prefixTemplate)),
					fmt.Sprintf(`ADVERTISED_%s_ADDRESSES+=(%s)`,
						helmette.Upper(listenerName),
						helmette.Quote(address),
					),
				)
			}

			snippet = append(snippet,
				``,
				fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"`,
					redpandaConfigPart,
					listenerAdvertisedName,
					externalCounter,
					helmette.Upper(listenerName),
				),
			)
		}
	}

	return snippet
}

// The following from _helpers.tpm

func adminTLSCurlFlags(state *RenderState) string {
	if !state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) {
		return ""
	}

	if state.Values.Listeners.Admin.TLS.RequireClientAuth {
		path := state.Values.Listeners.Admin.TLS.ClientMountPoint(&state.Values.TLS)
		return fmt.Sprintf("--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key", path, path, path)
	}

	path := state.Values.Listeners.Admin.TLS.ServerCAPath(&state.Values.TLS)
	return fmt.Sprintf("--cacert %s", path)
}

func externalAdvertiseAddress(state *RenderState) string {
	eaa := "${SERVICE_NAME}"
	externalDomainTemplate := ptr.Deref(state.Values.External.Domain, "")
	expanded := helmette.Tpl(state.Dot, externalDomainTemplate, state.Dot)
	if !helmette.Empty(expanded) {
		eaa = fmt.Sprintf("%s.%s", "${SERVICE_NAME}", expanded)
	}
	return eaa
}

// was advertised-host
func advertisedHostJSON(state *RenderState, externalName string, port int32, replicaIndex int) map[string]any {
	host := map[string]any{
		"name":    externalName,
		"address": externalAdvertiseAddress(state),
		"port":    port,
	}
	if len(state.Values.External.Addresses) > 0 {
		address := ""
		if len(state.Values.External.Addresses) > 1 {
			address = state.Values.External.Addresses[replicaIndex]
		} else {
			address = state.Values.External.Addresses[0]
		}
		if domain := ptr.Deref(state.Values.External.Domain, ""); domain != "" {
			host = map[string]any{
				"name":    externalName,
				"address": fmt.Sprintf("%s.%s", address, helmette.Tpl(state.Dot, domain, state.Dot)),
				"port":    port,
			}
		} else {
			host = map[string]any{
				"name":    externalName,
				"address": address,
				"port":    port,
			}
		}
	}
	return host
}

// adminInternalHTTPProtocol was admin-http-protocol
func adminInternalHTTPProtocol(state *RenderState) string {
	if state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) {
		return "https"
	}
	return "http"
}

// Additional helpers

func adminInternalURL(state *RenderState) string {
	// NB: SERVICE_NAME here actually refers to the podname via the downward
	// API.
	return fmt.Sprintf("%s://%s.%s:%d",
		adminInternalHTTPProtocol(state),
		`${SERVICE_NAME}`,
		strings.TrimSuffix(InternalDomain(state), "."),
		state.Values.Listeners.Admin.Port,
	)
}
