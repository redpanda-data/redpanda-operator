// Copyright 2026 Redpanda Data, Inc.
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

	staticCfg := state.AsStaticConfigSource()
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
