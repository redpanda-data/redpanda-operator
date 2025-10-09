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

	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3/chart"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
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

	staticCfg := toStaticConfig(state)
	overlay := console.StaticConfigurationSourceToPartialRenderValues(&staticCfg)

	consoleState.Values.ConfigMap.Create = true
	consoleState.Values.Deployment.Create = true
	consoleState.Values.ExtraEnv = append(overlay.ExtraEnv, consoleState.Values.ExtraEnv...)
	consoleState.Values.ExtraVolumes = append(overlay.ExtraVolumes, consoleState.Values.ExtraVolumes...)
	consoleState.Values.ExtraVolumeMounts = append(overlay.ExtraVolumeMounts, consoleState.Values.ExtraVolumeMounts...)
	consoleState.Values.Config = helmette.MergeTo[map[string]any](consoleState.Values.Config, overlay.Config)

	// Pass the same Redpanda License to Console
	if state.Values.Enterprise.LicenseSecretRef != nil {
		consoleState.Values.LicenseSecretRef = state.Values.Enterprise.LicenseSecretRef
	}

	if license := state.Values.Enterprise.License; license != "" && !ptr.Deref(state.Values.Console.Secret.Create, false) {
		consoleState.Values.Secret.Create = true
		consoleState.Values.Secret.License = license
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

func toStaticConfig(state *RenderState) ir.StaticConfigurationSource {
	username := state.Values.Auth.SASL.BootstrapUser.Username()
	passwordRef := state.Values.Auth.SASL.BootstrapUser.SecretKeySelector(Fullname(state))

	// Kafka API configuration
	kafkaSpec := &ir.KafkaAPISpec{
		Brokers: BrokerList(state, state.Values.Listeners.Kafka.Port),
	}

	// Add TLS configuration for Kafka if enabled
	if state.Values.Listeners.Kafka.TLS.IsEnabled(&state.Values.TLS) {
		kafkaSpec.TLS = state.Values.Listeners.Kafka.TLS.ToCommonTLS(state, &state.Values.TLS)
	}

	// TODO This check may need to be more complex.
	// There's two cluster configs and then listener level configuration.
	// Add SASL authentication using bootstrap user if enabled
	if state.Values.Auth.IsSASLEnabled() {
		kafkaSpec.SASL = &ir.KafkaSASL{
			Username: username,
			Password: ir.SecretKeyRef{
				Namespace: state.Release.Namespace,
				Name:      passwordRef.Name,
				Key:       passwordRef.Key,
			},
			Mechanism: ir.SASLMechanism(state.Values.Auth.SASL.BootstrapUser.GetMechanism()),
		}
	}

	// Admin API configuration
	var adminTLS *ir.CommonTLS
	adminSchema := "http"
	if state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) {
		adminSchema = "https"
		adminTLS = state.Values.Listeners.Admin.TLS.ToCommonTLS(state, &state.Values.TLS)
	}

	var adminAuth *ir.AdminAuth
	adminAuthEnabled, _ := state.Values.Config.Cluster["admin_api_require_auth"].(bool)
	if adminAuthEnabled {
		adminAuth = &ir.AdminAuth{
			Username: username,
			Password: ir.SecretKeyRef{
				Namespace: state.Release.Namespace,
				Name:      passwordRef.Name,
				Key:       passwordRef.Key,
			},
		}
	}

	adminSpec := &ir.AdminAPISpec{
		TLS:  adminTLS,
		Auth: adminAuth,
		URLs: []string{
			// NB: Console uses SRV based service discovery and doesn't require a full list of addresses.
			fmt.Sprintf("%s://%s:%d", adminSchema, InternalDomain(state), state.Values.Listeners.Admin.Port),
		},
	}

	// Schema Registry configuration (if enabled)
	var schemaRegistrySpec *ir.SchemaRegistrySpec
	if state.Values.Listeners.SchemaRegistry.Enabled {
		var schemaTLS *ir.CommonTLS
		schemaSchema := "http"
		if state.Values.Listeners.SchemaRegistry.TLS.IsEnabled(&state.Values.TLS) {
			schemaSchema = "https"
			schemaTLS = state.Values.Listeners.SchemaRegistry.TLS.ToCommonTLS(state, &state.Values.TLS)
		}

		var schemaURLs []string
		brokers := BrokerList(state, state.Values.Listeners.SchemaRegistry.Port)
		for _, broker := range brokers {
			schemaURLs = append(schemaURLs, fmt.Sprintf("%s://%s", schemaSchema, broker))
		}

		schemaRegistrySpec = &ir.SchemaRegistrySpec{
			URLs: schemaURLs,
			TLS:  schemaTLS,
		}

		// TODO: This check is likely incorrect but it matches the historical
		// behavior.
		if state.Values.Auth.IsSASLEnabled() {
			schemaRegistrySpec.SASL = &ir.SchemaRegistrySASL{
				Username: username,
				Password: ir.SecretKeyRef{
					Namespace: state.Release.Namespace,
					Name:      passwordRef.Name,
					Key:       passwordRef.Key,
				},
			}
		}
	}

	return ir.StaticConfigurationSource{
		Kafka:          kafkaSpec,
		Admin:          adminSpec,
		SchemaRegistry: schemaRegistrySpec,
	}
}
