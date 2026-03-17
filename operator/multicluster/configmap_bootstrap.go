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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// bootstrapResult holds the outputs from bootstrap content generation.
type bootstrapResult struct {
	// template is the JSON-encoded bootstrap template string.
	template string
	// fixups is the JSON-encoded fixups array string.
	fixups string
	// envVars are the environment variables needed by the bootstrap-yaml-envsubst
	// init container to resolve secret/configmap references.
	envVars []corev1.EnvVar
}

// bootstrapContents computes the bootstrap template, fixups, and env vars.
// Configuration is layered with later values overriding earlier ones:
//  1. configurationDefaults — hardcoded safe defaults
//  2. Dynamic values — computed from CRD fields (auth, rack awareness, storage)
//  3. Tiered storage config — from StretchStorage.Tiered
//  4. ClusterConfig — user-specified cluster-level config overrides
//  5. TunableConfig — user-specified tunable config overrides
//  6. ExtraClusterConfiguration — escape hatch for arbitrary key/value pairs
func bootstrapContents(state *RenderState) bootstrapResult {
	fixups := []clusterconfiguration.Fixup{}
	var envVars []corev1.EnvVar

	// Start with shared defaults, then layer on dynamic values.
	bootstrap := map[string]any{}
	for k, v := range configurationDefaults {
		bootstrap[k] = v
	}

	bootstrap["kafka_enable_authorization"] = state.Spec().Auth.IsSASLEnabled()
	bootstrap["enable_sasl"] = state.Spec().Auth.IsSASLEnabled()
	bootstrap["enable_rack_awareness"] = state.Spec().RackAwareness.IsEnabled()
	bootstrap["audit_enabled"] = state.Spec().IsAuditLoggingEnabled()

	// storage_min_free_bytes: min(5GiB, 5% of PV size).
	bootstrap["storage_min_free_bytes"] = state.Spec().GetStorageMinFreeBytes()

	// If total cluster replicas >= 3, set default_topic_replications to 3 for HA.
	if state.totalReplicas() >= 3 {
		bootstrap["default_topic_replications"] = 3
	}

	// Merge in tiered storage config from CRD (overrides defaults above).
	tieredAttrs, tieredFixups, tieredEnvVars := tieredStorageToConfiguration(state.Spec().Storage)
	for k, v := range tieredAttrs {
		bootstrap[k] = v
	}
	fixups = append(fixups, tieredFixups...)
	envVars = append(envVars, tieredEnvVars...)

	// Merge in cluster config from CRD (overrides defaults above).
	for k, v := range state.Spec().GetClusterConfig() {
		bootstrap[k] = v
	}

	// Merge in tunable config (overrides defaults above).
	for k, v := range state.Spec().GetTunableConfig() {
		bootstrap[k] = v
	}

	// Convert all values to string-encoded JSON (the format expected by the
	// bootstrap-yaml-envsubst init container).
	template := map[string]string{}
	for k, v := range bootstrap {
		encoded, err := json.Marshal(v)
		if err != nil {
			continue
		}
		template[k] = string(encoded)
	}

	// Fold in any ExtraClusterConfiguration values.
	if cfg := state.Spec().Config; cfg != nil {
		extra, extraFixups, extraEnvVars := clusterConfigurationToConfiguration(redpandav1alpha2.ClusterConfiguration(cfg.ExtraClusterConfiguration))
		for k, v := range extra {
			template[k] = v
		}
		fixups = append(fixups, extraFixups...)
		envVars = append(envVars, extraEnvVars...)
	}

	templateJSON, err := json.Marshal(template)
	if err != nil {
		templateJSON = []byte("{}")
	}

	fixupsJSON, err := json.Marshal(fixups)
	if err != nil || len(fixups) == 0 {
		fixupsJSON = []byte("[]")
	}

	return bootstrapResult{
		template: string(templateJSON),
		fixups:   string(fixupsJSON),
		envVars:  envVars,
	}
}

// configurationDefaults are the default bootstrap configuration values matching the
// Helm chart's values.yaml. They are applied before any CRD-level overrides.
var configurationDefaults = map[string]any{
	// Tunable defaults.
	"compacted_log_segment_size":     64 * redpandav1alpha2.MiB,
	"log_segment_size_min":           16 * redpandav1alpha2.MiB,
	"log_segment_size_max":           256 * redpandav1alpha2.MiB,
	"max_compacted_log_segment_size": 512 * redpandav1alpha2.MiB,
	"kafka_connection_rate_limit":    1000,

	// Tiered storage defaults.
	"cloud_storage_enabled":             false,
	"cloud_storage_enable_remote_read":  true,
	"cloud_storage_enable_remote_write": true,
	"cloud_storage_cache_size":          5 * redpandav1alpha2.GiB,
}
