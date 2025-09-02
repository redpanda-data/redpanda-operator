// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_console.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3/chart"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// consoleChartIntegration plumbs redpanda connection information into the console subchart.
// It does this by calculating Kafka, Schema registry, Redpanda Admin API configuration
// from Redpanda chart state.Values.
func consoleChartIntegration(state *RenderState) []kube.Object {
	if !ptr.Deref(state.Values.Console.Enabled, true) {
		return nil
	}

	consoleState := consolechart.DotToState(state.Dot.Subcharts["console"])

	// Pass the same Redpanda License to Console
	if license := state.Values.Enterprise.License; license != "" && !ptr.Deref(state.Values.Console.Secret.Create, false) {
		consoleState.Values.Secret.Create = true
		consoleState.Values.Secret.License = license
	}

	// Create console configuration based on Redpanda helm chart state.Values.
	if !ptr.Deref(state.Values.Console.ConfigMap.Create, false) {
		consoleState.Values.ConfigMap.Create = true
		consoleState.Values.Config = ConsoleConfig(state)
	}

	if !ptr.Deref(state.Values.Console.Deployment.Create, false) {
		consoleState.Values.Deployment.Create = true

		// Adopt Console entry point to use SASL user in Kafka,
		// Schema Registry and Redpanda Admin API connection
		if state.Values.Auth.IsSASLEnabled() {
			command := []string{
				"sh",
				"-c",
				"set -e; IFS=':' read -r KAFKA_SASL_USERNAME KAFKA_SASL_PASSWORD KAFKA_SASL_MECHANISM < <(grep \"\" $(find /mnt/users/* -print));" +
					fmt.Sprintf(" KAFKA_SASL_MECHANISM=${KAFKA_SASL_MECHANISM:-%s};", GetSASLMechanism(state)) +
					" export KAFKA_SASL_USERNAME KAFKA_SASL_PASSWORD KAFKA_SASL_MECHANISM;" +
					" export KAFKA_SCHEMAREGISTRY_USERNAME=$KAFKA_SASL_USERNAME;" +
					" export KAFKA_SCHEMAREGISTRY_PASSWORD=$KAFKA_SASL_PASSWORD;" +
					" export REDPANDA_ADMINAPI_USERNAME=$KAFKA_SASL_USERNAME;" +
					" export REDPANDA_ADMINAPI_PASSWORD=$KAFKA_SASL_PASSWORD;" +
					" /app/console $@",
				" --",
			}
			consoleState.Values.Deployment.Command = command
		}

		// Create License reference for Console
		if secret := state.Values.Enterprise.LicenseSecretRef; secret != nil {
			consoleState.Values.LicenseSecretRef = secret
		}

		consoleState.Values.ExtraVolumes = consoleTLSVolumes(state)
		consoleState.Values.ExtraVolumeMounts = consoleTLSVolumesMounts(state)

		if consoleState.Values.PodAnnotations == nil {
			consoleState.Values.PodAnnotations = map[string]string{}
		}

		cfg := console.ConfigMap(consoleState)
		consoleState.Values.PodAnnotations["checksum-redpanda-chart/config"] = helmette.Sha256Sum(helmette.ToYaml(cfg))
	}

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return []kube.Object{
		console.Secret(consoleState),
		console.ConfigMap(consoleState),
		console.Deployment(consoleState),
	}
}

func consoleTLSVolumesMounts(state *RenderState) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{}

	if sasl := state.Values.Auth.SASL; sasl.Enabled && sasl.SecretRef != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("%s-users", Fullname(state)),
			MountPath: "/mnt/users",
			ReadOnly:  true,
		})
	}

	if len(state.Values.Listeners.TrustStores(&state.Values.TLS)) > 0 {
		mounts = append(
			mounts,
			corev1.VolumeMount{Name: "truststores", MountPath: TrustStoreMountPath, ReadOnly: true},
		)
	}

	visitedCert := map[string]bool{}
	for _, tlsCfg := range []InternalTLS{
		state.Values.Listeners.Kafka.TLS,
		state.Values.Listeners.SchemaRegistry.TLS,
		state.Values.Listeners.Admin.TLS,
	} {
		_, visited := visitedCert[tlsCfg.Cert]
		if !tlsCfg.IsEnabled(&state.Values.TLS) || visited {
			continue
		}
		visitedCert[tlsCfg.Cert] = true

		mounts = append(mounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("redpanda-%s-cert", tlsCfg.Cert),
			MountPath: fmt.Sprintf("%s/%s", certificateMountPoint, tlsCfg.Cert),
		})
	}

	return append(mounts, state.Values.Console.ExtraVolumeMounts...)
}

func consoleTLSVolumes(state *RenderState) []corev1.Volume {
	volumes := []corev1.Volume{}

	if sasl := state.Values.Auth.SASL; sasl.Enabled && sasl.SecretRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%s-users", Fullname(state)),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: state.Values.Auth.SASL.SecretRef,
				},
			},
		})
	}

	if vol := state.Values.Listeners.TrustStoreVolume(&state.Values.TLS); vol != nil {
		volumes = append(volumes, *vol)
	}

	visitedCert := map[string]bool{}
	for _, tlsCfg := range []InternalTLS{
		state.Values.Listeners.Kafka.TLS,
		state.Values.Listeners.SchemaRegistry.TLS,
		state.Values.Listeners.Admin.TLS,
	} {
		_, visited := visitedCert[tlsCfg.Cert]
		if !tlsCfg.IsEnabled(&state.Values.TLS) || visited {
			continue
		}
		visitedCert[tlsCfg.Cert] = true

		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("redpanda-%s-cert", tlsCfg.Cert),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					DefaultMode: ptr.To[int32](0o420),
					SecretName:  CertSecretName(state, tlsCfg.Cert, state.Values.TLS.Certs.MustGet(tlsCfg.Cert)),
				},
			},
		})
	}

	return append(volumes, state.Values.Console.ExtraVolumes...)
}

func ConsoleConfig(state *RenderState) map[string]any {
	var schemaURLs []string
	if state.Values.Listeners.SchemaRegistry.Enabled {
		schema := "http"
		if state.Values.Listeners.SchemaRegistry.TLS.IsEnabled(&state.Values.TLS) {
			schema = "https"
		}

		for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
			schemaURLs = append(schemaURLs, fmt.Sprintf("%s://%s-%d.%s:%d", schema, Fullname(state), i, InternalDomain(state), state.Values.Listeners.SchemaRegistry.Port))
		}
	}

	schema := "http"
	if state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) {
		schema = "https"
	}

	c := map[string]any{
		"kafka": map[string]any{
			"brokers": BrokerList(state, state.Values.Listeners.Kafka.Port),
			"sasl": map[string]any{
				"enabled": state.Values.Auth.IsSASLEnabled(),
			},
			"tls": state.Values.Listeners.Kafka.ConsoleTLS(&state.Values.TLS),
		},
		"redpanda": map[string]any{
			"adminApi": map[string]any{
				"enabled": true,
				"urls": []string{
					fmt.Sprintf("%s://%s:%d", schema, InternalDomain(state), state.Values.Listeners.Admin.Port),
				},
				"tls": state.Values.Listeners.Admin.ConsoleTLS(&state.Values.TLS),
			},
		},
		"schemaRegistry": map[string]any{
			"enabled": state.Values.Listeners.SchemaRegistry.Enabled,
			"urls":    schemaURLs,
			"tls":     state.Values.Listeners.SchemaRegistry.ConsoleTLS(&state.Values.TLS),
		},
	}

	if state.Values.Console.Config == nil {
		state.Values.Console.Config = map[string]any{}
	}

	return helmette.Merge(state.Values.Console.Config, c)
}
