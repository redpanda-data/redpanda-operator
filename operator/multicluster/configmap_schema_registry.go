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
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// schemaRegistryConfig generates the schema_registry section of the redpanda.yaml template.
func schemaRegistryConfig(state *RenderState) map[string]any {
	cfg := map[string]any{}

	var sr *redpandav1alpha2.StretchAPIListener
	if l := state.Spec().Listeners; l != nil {
		sr = l.SchemaRegistry
	}

	configureAPIListener(cfg, state, sr, "schema_registry_api", "schema_registry_api_tls",
		state.Spec().SchemaRegistryPort(), redpandav1alpha2.DefaultExternalSchemaRegistryPort, "")

	return cfg
}
