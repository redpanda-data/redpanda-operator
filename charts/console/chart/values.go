// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:ignore=true
package chart

import (
	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
)

type Values struct {
	console.RenderValues `json:",inline"`

	Globals      map[string]any    `json:"global,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	CommonLabels map[string]string `json:"commonLabels"`
	Tests        Enableable        `json:"tests"`
}

type Enableable struct {
	Enabled bool `json:"enabled"`
}
