// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_connectors.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/connectors"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// connectorsChartIntegration plumbs redpanda connection information into the connectors subchart.
// It does this by calculating Kafka configuration from Redpanda chart values.
func connectorsChartIntegration(dot *helmette.Dot) []kube.Object {
	values := helmette.UnmarshalInto[Values](dot.Values)

	if !ptr.Deref(values.Connectors.Enabled, false) || ptr.Deref(values.Connectors.Deployment.Create, false) {
		return nil
	}

	connectorsDot := dot.Subcharts["connectors"]
	loadedValues := connectorsDot.Values

	connectorsValue := helmette.UnmarshalInto[connectors.Values](connectorsDot.Values)

	connectorsValue.Deployment = helmette.MergeTo[connectors.DeploymentConfig](connectorsValue.Deployment, connectors.DeploymentConfig{
		Create: true,
	})

	if connectorsValue.Connectors.BootstrapServers == "" {
		for _, b := range BrokerList(dot, values.Statefulset.Replicas, values.Listeners.Kafka.Port) {
			if len(connectorsValue.Connectors.BootstrapServers) == 0 {
				connectorsValue.Connectors.BootstrapServers = b
				continue
			}
			connectorsValue.Connectors.BootstrapServers = fmt.Sprintf("%s,%s", connectorsValue.Connectors.BootstrapServers, b)
		}
	}

	connectorsValue.Connectors.BrokerTLS = connectors.TLS{
		Enabled: false,
		CA: struct {
			SecretRef           string `json:"secretRef"`
			SecretNameOverwrite string `json:"secretNameOverwrite"`
		}{},
		Cert: struct {
			SecretRef           string `json:"secretRef"`
			SecretNameOverwrite string `json:"secretNameOverwrite"`
		}{},
		Key: struct {
			SecretRef           string `json:"secretRef"`
			SecretNameOverwrite string `json:"secretNameOverwrite"`
		}{},
	}

	connectorsValue.Connectors.BrokerTLS = values.Listeners.Kafka.ConnectorsTLS(&values.TLS, Fullname(dot))

	if values.Auth.IsSASLEnabled() {
		command := []string{
			"bash",
			"-c",
			"set -e; IFS=':' read -r CONNECT_SASL_USERNAME CONNECT_SASL_PASSWORD CONNECT_SASL_MECHANISM < <(grep \"\" $(find /mnt/users/* -print));" +
				fmt.Sprintf(" CONNECT_SASL_MECHANISM=${CONNECT_SASL_MECHANISM:-%s};", SASLMechanism(dot)) +
				" export CONNECT_SASL_USERNAME CONNECT_SASL_PASSWORD CONNECT_SASL_MECHANISM;" +
				" [[ $CONNECT_SASL_MECHANISM == \"SCRAM-SHA-256\" ]] && CONNECT_SASL_MECHANISM=scram-sha-256;" +
				" [[ $CONNECT_SASL_MECHANISM == \"SCRAM-SHA-512\" ]] && CONNECT_SASL_MECHANISM=scram-sha-512;" +
				" export CONNECT_SASL_MECHANISM;" +
				" echo $CONNECT_SASL_PASSWORD > /opt/kafka/connect-password/rc-credentials/password;" +
				" exec /opt/kafka/bin/kafka_connect_run.sh",
		}
		connectorsValue.Deployment.Command = command

		connectorsValue.Auth.SASL = helmette.MergeTo[struct {
			Enabled   bool   `json:"enabled"`
			Mechanism string `json:"mechanism"`
			SecretRef string `json:"secretRef"`
			UserName  string `json:"userName"`
		}](connectorsValue.Auth.SASL, struct {
			Enabled   bool   `json:"enabled"`
			Mechanism string `json:"mechanism"`
			SecretRef string `json:"secretRef"`
			UserName  string `json:"userName"`
		}{Enabled: true})

		connectorsValue.Storage.Volume = append(connectorsValue.Storage.Volume, []corev1.Volume{
			{
				Name: cleanForK8sWithSuffix(Fullname(dot), "users"),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: values.Auth.SASL.SecretRef,
					},
				},
			},
			{
				Name: cleanForK8sWithSuffix(Fullname(dot), "user-password"),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		}...)

		connectorsValue.Storage.VolumeMounts = append(connectorsValue.Storage.VolumeMounts, []corev1.VolumeMount{
			{
				Name:      cleanForK8sWithSuffix(Fullname(dot), "users"),
				MountPath: "/mnt/users",
				ReadOnly:  true,
			},
			{
				Name:      cleanForK8sWithSuffix(Fullname(dot), "user-password"),
				MountPath: "/opt/kafka/connect-password/rc-credentials",
			},
		}...)

		connectorsValue.Deployment.ExtraEnv = append(connectorsValue.Deployment.ExtraEnv, []corev1.EnvVar{
			{
				Name:  "CONNECT_SASL_PASSWORD_FILE",
				Value: "rc-credentials/password",
			},
		}...)
	}

	connectorsDot.Values = helmette.UnmarshalInto[helmette.Values](connectorsValue)

	manifests := []kube.Object{
		connectors.Deployment(connectorsDot),
	}

	connectorsDot.Values = loadedValues

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return manifests
}
