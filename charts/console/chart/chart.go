// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"embed"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/gotohelm"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

var (
	//go:embed Chart.yaml
	//go:embed templates/*
	//go:embed values.schema.json
	//go:embed values.yaml
	chartFiles embed.FS

	//go:embed values.yaml
	DefaultValuesYAML []byte

	//go:embed values.schema.json
	ValuesSchemaJSON []byte

	// ChartLabel is the go version of the console helm chart.
	Chart = gotohelm.MustLoad(chartFiles, render)
)

type Values struct {
	console.Values `json:",inline"`

	Enabled *bool      `json:"enabled,omitempty"`
	Tests   Enableable `json:"tests"`
}

type Enableable struct {
	Enabled bool `json:"enabled"`
}

// render is the entrypoint to both the go and helm versions of the console
// helm chart.
// In helm, _shims.render-manifest is used to call and filter the output of
// this function.
// In go, this function should be call by executing [ChartLabel.Render], which will
// handle construction of [helmette.Dot], subcharting, and output filtering.
func render(dot *helmette.Dot) []kube.Object {
	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return console.Render(dot)
}
