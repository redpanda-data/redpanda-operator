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

// pandaProxyConfig generates the pandaproxy section of the redpanda.yaml template.
func pandaProxyConfig(state *RenderState) map[string]any {
	cfg := map[string]any{}

	var http *redpandav1alpha2.StretchAPIListener
	if l := state.Spec().Listeners; l != nil {
		http = l.HTTP
	}

	authMethod := ""
	if state.Spec().Auth.IsSASLEnabled() {
		authMethod = "http_basic"
	}

	configureAPIListener(cfg, state, http, "pandaproxy_api", "pandaproxy_api_tls",
		state.Spec().HTTPPort(), redpandav1alpha2.DefaultExternalHTTPPort, authMethod)

	return cfg
}
