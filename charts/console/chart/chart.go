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
	"fmt"
	"strings"

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

	// Chart is the go version of the console helm chart.
	Chart = gotohelm.MustLoad(chartFiles, Render)
)

// Render is the entrypoint to both the go and helm versions of the console
// helm chart.
// In helm, _shims.render-manifest is used to call and filter the output of
// this function.
// In go, this function should be call by executing [ChartLabel.Render], which will
// handle construction of [helmette.Dot], subcharting, and output filtering.
func Render(dot *helmette.Dot) []kube.Object {
	state := DotToState(dot)

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return console.Render(state)
}

func DotToState(dot *helmette.Dot) *console.RenderState {
	values := helmette.Unwrap[Values](dot.Values)
	templater := &templater{Dot: dot, FauxDot: newFauxDot(dot)}

	if values.RenderValues.Secret.Authentication.JWTSigningKey == "" {
		values.RenderValues.Secret.Authentication.JWTSigningKey = helmette.RandAlphaNum(32)
	}

	return &console.RenderState{
		ReleaseName: dot.Release.Name,
		Namespace:   dot.Release.Namespace,
		Values:      values.RenderValues,
		Template:    templater.Template,
		CommonLabels: map[string]string{
			"helm.sh/chart":                ChartLabel(dot),
			"app.kubernetes.io/managed-by": dot.Release.Service,
			"app.kubernetes.io/version":    dot.Chart.AppVersion,
		},
	}
}

// templater is a fairly hacky yet effective way to abstract out the global
// `tpl` function that's plagued our rendering packages.
//
// It works around a very niche issue in gotohelm/helm:
// 1. The inability to return a [helmette.Chart]. Helm's internal version
// of `Chart` has JSON tags. Returning it in gotohelm results in all
// fields being downcased and therefore inaccessible. To work around this, we
// construct a [FauxChart] which explicitly keeps the Chart fields upper cased.
// 2. The inability to return a `helmette.Values`. Values is a wrapper type
// around a map[string]any. It comes with an AsMap function that's used by
// gotohelm. We hack in interoperability by assigning a shallow clone of the
// values to itself in the AsMap key.
type templater struct {
	// Dot is present purely to satisfy the go requirements of helmette.Tpl. DO
	// NOT REFERENCE IT IN HELM IT WILL NOT WORK.
	Dot     *helmette.Dot
	FauxDot *FauxDot
}

func (t *templater) Template(tpl string) string {
	return helmette.Tpl(t.Dot, tpl, t.FauxDot)
}

type FauxChart struct {
	Name       string
	Version    string
	AppVersion string
}

type FauxDot struct {
	Values   map[string]any
	Chart    FauxChart
	Release  helmette.Release
	Template helmette.Template
}

func newFauxDot(dot *helmette.Dot) *FauxDot {
	clone := map[string]any{}
	for key, value := range dot.Values.AsMap() {
		clone[key] = value
	}
	// NB: A shallow clone MUST be used here. If there's any recursion, the
	// JSON serialization in sprig "gives up" and returns an empty string.
	clone["AsMap"] = dot.Values.AsMap()
	return &FauxDot{
		Values:   clone,
		Release:  dot.Release,
		Template: dot.Template,
		Chart: FauxChart{
			Name:       dot.Chart.Name,
			AppVersion: dot.Chart.AppVersion,
			Version:    dot.Chart.Version,
		},
	}
}

// Functions below this line are included for backwards compatibility purposes.
// They're re-namespaced in compat.tpl

func Name(dot *helmette.Dot) string {
	return DotToState(dot).ChartName()
}

// Create a default fully qualified app name.
// We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
// If release name contains chart name it will be used as a full name.
func Fullname(dot *helmette.Dot) string {
	return DotToState(dot).FullName()
}

// Create chart name and version as used by the chart label.
func ChartLabel(dot *helmette.Dot) string {
	chart := fmt.Sprintf("%s-%s", dot.Chart.Name, dot.Chart.Version)
	return cleanForK8s(strings.ReplaceAll(chart, "+", "_"))
}

func cleanForK8s(s string) string {
	return helmette.TrimSuffix("-", helmette.Trunc(63, s))
}
