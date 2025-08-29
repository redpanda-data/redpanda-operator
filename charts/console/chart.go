// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:namespace=console
package console

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// Scheme is a [runtime.Scheme] with the appropriate extensions to load all
// objects produced by the console chart.
var Scheme = runtime.NewScheme()

// +gotohelm:ignore=true
func init() {
	must(scheme.AddToScheme(Scheme))
}

// +gotohelm:ignore=true
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// render is the entrypoint to both the go and helm versions of the console
// helm chart.
// In helm, _shims.render-manifest is used to call and filter the output of
// this function.
// In go, this function should be call by executing [ChartLabel.Render], which will
// handle construction of [helmette.Dot], subcharting, and output filtering.
func Render(dot *helmette.Dot) []kube.Object {
	manifests := []kube.Object{
		ServiceAccount(dot),
		Secret(dot),
		ConfigMap(dot),
		Service(dot),
		Ingress(dot),
		Deployment(dot),
		HorizontalPodAutoscaler(dot),
	}

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return manifests
}
