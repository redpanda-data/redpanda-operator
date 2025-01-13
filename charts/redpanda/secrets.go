// Copyright 2025 Redpanda Data, Inc.
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

	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

const DefaultSASLMechanism = "SCRAM-SHA-512"

func Secrets(dot *helmette.Dot) []*corev1.Secret {
	var secrets []*corev1.Secret
	secrets = append(secrets, SecretSTSLifecycle(dot))
	if saslUsers := SecretSASLUsers(dot); saslUsers != nil {
		secrets = append(secrets, saslUsers)
	}
	secrets = append(secrets, SecretConfigurator(dot))
	if fsValidator := SecretFSValidator(dot); fsValidator != nil {
		secrets = append(secrets, fsValidator)
	}
	if bootstrapUser := SecretBootstrapUser(dot); bootstrapUser != nil {
		secrets = append(secrets, bootstrapUser)
	}
	return secrets
}

func SecretSTSLifecycle(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sts-lifecycle", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels:    FullLabels(dot),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{},
	}
	adminCurlFlags := adminTLSCurlFlags(dot)
	secret.StringData["common.sh"] = helmette.Join("\n", []string{
		`#!/usr/bin/env bash`,
		``,
		`# the SERVICE_NAME comes from the metadata.name of the pod, essentially the POD_NAME`,
		fmt.Sprintf(`CURL_URL="%s"`, adminInternalURL(dot)),
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
	if values.Statefulset.Replicas > 2 && !helmette.Dig(values.Config.Node, false, "recovery_mode_enabled").(bool) {
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

func SecretSASLUsers(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	if values.Auth.SASL.SecretRef != "" && values.Auth.SASL.Enabled && len(values.Auth.SASL.Users) > 0 {
		secret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      values.Auth.SASL.SecretRef,
				Namespace: dot.Release.Namespace,
				Labels:    FullLabels(dot),
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{},
		}
		usersTxt := []string{}
		// Working around lack of support for += or strings.Join at the moment
		for _, user := range values.Auth.SASL.Users {
			if helmette.Empty(user.Mechanism) {
				usersTxt = append(usersTxt, fmt.Sprintf("%s:%s", user.Name, user.Password))
			} else {
				usersTxt = append(usersTxt, fmt.Sprintf("%s:%s:%s", user.Name, user.Password, user.Mechanism))
			}
		}
		secret.StringData["users.txt"] = helmette.Join("\n", usersTxt)
		return secret
	} else if values.Auth.SASL.Enabled && values.Auth.SASL.SecretRef == "" {
		panic("auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true")
	} else {
		// XXX no secret generated when enabled, we have a secret ref, but we have no users
		return nil
	}
}

func SecretBootstrapUser(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)
	if !values.Auth.SASL.Enabled || values.Auth.SASL.BootstrapUser.SecretKeyRef != nil {
		return nil
	}

	secretName := fmt.Sprintf("%s-bootstrap-user", Fullname(dot))

	if dot.Release.IsUpgrade {
		if existing, ok := helmette.Lookup[corev1.Secret](dot, dot.Release.Namespace, secretName); ok {
			return existing
		}
	}

	password := helmette.RandAlphaNum(32)

	userPassword := values.Auth.SASL.BootstrapUser.Password
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
			Namespace: dot.Release.Namespace,
			Labels:    FullLabels(dot),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": password,
		},
	}
}

func SecretFSValidator(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Statefulset.InitContainers.FSValidator.Enabled {
		return nil
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-fs-validator", Fullname(dot)[:49]),
			Namespace: dot.Release.Namespace,
			Labels:    FullLabels(dot),
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

func SecretConfigurator(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%.51s-configurator", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels:    FullLabels(dot),
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
	if !RedpandaAtLeast_22_3_0(dot) {
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

	kafkaSnippet := secretConfiguratorKafkaConfig(dot)
	configuratorSh = append(configuratorSh, kafkaSnippet...)

	httpSnippet := secretConfiguratorHTTPConfig(dot)
	configuratorSh = append(configuratorSh, httpSnippet...)

	if RedpandaAtLeast_22_3_0(dot) && values.RackAwareness.Enabled {
		configuratorSh = append(configuratorSh,
			``,
			`# Configure Rack Awareness`,
			`set +x`,
			fmt.Sprintf(`RACK=$(curl --silent --cacert /run/secrets/kubernetes.io/serviceaccount/ca.crt --fail -H 'Authorization: Bearer '$(cat /run/secrets/kubernetes.io/serviceaccount/token) "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT_HTTPS}/api/v1/nodes/${KUBERNETES_NODE_NAME}?pretty=true" | grep %s | grep -v '\"key\":' | sed 's/.*": "\([^"]\+\).*/\1/')`,
				helmette.SQuote(helmette.Quote(values.RackAwareness.NodeAnnotation)),
			),
			`set -x`,
			`rpk --config "$CONFIG" redpanda config set redpanda.rack "${RACK}"`,
		)
	}
	secret.StringData["configurator.sh"] = helmette.Join("\n", configuratorSh)
	return secret
}

func secretConfiguratorKafkaConfig(dot *helmette.Dot) []string {
	values := helmette.Unwrap[Values](dot.Values)

	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "${SERVICE_NAME}", InternalDomain(dot))

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
			"port":    values.Listeners.Kafka.Port,
		}))),
		fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"`,
			redpandaConfigPart,
			listenerAdvertisedName,
		),
	)
	if len(values.Listeners.Kafka.External) > 0 {
		externalCounter := 0
		for externalName, externalVals := range values.Listeners.Kafka.External {
			externalCounter = externalCounter + 1
			snippet = append(snippet,
				``,
				fmt.Sprintf(`ADVERTISED_%s_ADDRESSES=()`, helmette.Upper(listenerName)),
			)
			for _, replicaIndex := range helmette.Until(int(values.Statefulset.Replicas)) {
				// advertised-port for kafka
				port := externalVals.Port // This is always defined for kafka
				if len(externalVals.AdvertisedPorts) > 0 {
					if len(externalVals.AdvertisedPorts) == 1 {
						port = externalVals.AdvertisedPorts[0]
					} else {
						port = externalVals.AdvertisedPorts[replicaIndex]
					}
				}

				host := advertisedHostJSON(dot, externalName, port, replicaIndex)
				// XXX: the original code used the stringified `host` value as a template
				// for re-expansion; however it was impossible to make this work usefully,
				/// even with the original yaml template.
				address := helmette.ToJSON(host)
				prefixTemplate := ptr.Deref(externalVals.PrefixTemplate, "")
				if prefixTemplate == "" {
					// Required because the values might not specify this, it'll ensur we see "" if it's missing.
					prefixTemplate = helmette.Default("", values.External.PrefixTemplate)
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

func secretConfiguratorHTTPConfig(dot *helmette.Dot) []string {
	values := helmette.Unwrap[Values](dot.Values)

	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "${SERVICE_NAME}", InternalDomain(dot))

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
			"port":    values.Listeners.HTTP.Port,
		}))),
		fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"`,
			redpandaConfigPart,
			listenerAdvertisedName,
		),
	)
	if len(values.Listeners.HTTP.External) > 0 {
		externalCounter := 0
		for externalName, externalVals := range values.Listeners.HTTP.External {
			externalCounter = externalCounter + 1
			snippet = append(snippet,
				``,
				fmt.Sprintf(`ADVERTISED_%s_ADDRESSES=()`, helmette.Upper(listenerName)),
			)
			for _, replicaIndex := range helmette.Until(int(values.Statefulset.Replicas)) {
				// advertised-port for kafka
				port := externalVals.Port // This is always defined for kafka
				if len(externalVals.AdvertisedPorts) > 0 {
					if len(externalVals.AdvertisedPorts) == 1 {
						port = externalVals.AdvertisedPorts[0]
					} else {
						port = externalVals.AdvertisedPorts[replicaIndex]
					}
				}

				host := advertisedHostJSON(dot, externalName, port, replicaIndex)
				// XXX: the original code used the stringified `host` value as a template
				// for re-expansion; however it was impossible to make this work usefully,
				/// even with the original yaml template.
				address := helmette.ToJSON(host)

				prefixTemplate := ptr.Deref(externalVals.PrefixTemplate, "")
				if prefixTemplate == "" {
					// Required because the values might not specify this, it'll ensur we see "" if it's missing.
					prefixTemplate = helmette.Default("", values.External.PrefixTemplate)
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

func adminTLSCurlFlags(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Listeners.Admin.TLS.IsEnabled(&values.TLS) {
		return ""
	}

	if values.Listeners.Admin.TLS.RequireClientAuth {
		path := fmt.Sprintf("%s/%s-client", certificateMountPoint, Fullname(dot))
		return fmt.Sprintf("--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key", path, path, path)
	}

	path := values.Listeners.Admin.TLS.ServerCAPath(&values.TLS)
	return fmt.Sprintf("--cacert %s", path)
}

func externalAdvertiseAddress(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	eaa := "${SERVICE_NAME}"
	externalDomainTemplate := ptr.Deref(values.External.Domain, "")
	expanded := helmette.Tpl(externalDomainTemplate, dot)
	if !helmette.Empty(expanded) {
		eaa = fmt.Sprintf("%s.%s", "${SERVICE_NAME}", expanded)
	}
	return eaa
}

// was advertised-host
func advertisedHostJSON(dot *helmette.Dot, externalName string, port int32, replicaIndex int) map[string]any {
	values := helmette.Unwrap[Values](dot.Values)

	host := map[string]any{
		"name":    externalName,
		"address": externalAdvertiseAddress(dot),
		"port":    port,
	}
	if len(values.External.Addresses) > 0 {
		address := ""
		if len(values.External.Addresses) > 1 {
			address = values.External.Addresses[replicaIndex]
		} else {
			address = values.External.Addresses[0]
		}
		if domain := ptr.Deref(values.External.Domain, ""); domain != "" {
			host = map[string]any{
				"name":    externalName,
				"address": fmt.Sprintf("%s.%s", address, helmette.Tpl(domain, dot)),
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
func adminInternalHTTPProtocol(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	if values.Listeners.Admin.TLS.IsEnabled(&values.TLS) {
		return "https"
	}
	return "http"
}

// Additional helpers

func adminInternalURL(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	return fmt.Sprintf("%s://%s.%s.%s.svc.%s:%d",
		adminInternalHTTPProtocol(dot),
		`${SERVICE_NAME}`,
		ServiceName(dot),
		dot.Release.Namespace,
		strings.TrimSuffix(values.ClusterDomain, "."),
		values.Listeners.Admin.Port,
	)
}
