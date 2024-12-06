// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:ignore=true
package operator

import (
	_ "embed"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	// Scheme is a [runtime.Scheme] with the appropriate extensions to load all
	// objects produced by the redpanda chart.
	Scheme = runtime.NewScheme()

	//go:embed Chart.yaml
	chartYAML []byte

	//go:embed values.yaml
	defaultValuesYAML []byte

	// Chart is the go version of the operator helm chart.
	Chart = gotohelm.MustLoad(chartYAML, defaultValuesYAML, render)
)

func init() {
	must(scheme.AddToScheme(Scheme))
	must(certmanagerv1.AddToScheme(Scheme))
	must(monitoringv1.AddToScheme(Scheme))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// render is the entrypoint to both the go and helm versions of the redpanda
// helm chart.
// In helm, _shims.render-manifest is used to call and filter the output of
// this function.
// In go, this function should be call by executing [Chart.Render], which will
// handle construction of [helmette.Dot], subcharting, and output filtering.
func render(dot *helmette.Dot) []kube.Object {
	manifests := []kube.Object{
		Certificate(dot),
		ConfigMap(dot),
		Deployment(dot),
		WebhookService(dot),
		MetricsService(dot),
		ServiceAccount(dot),
		ServiceMonitor(dot),
	}

	// NB: gotohelm doesn't currently have a way to handle casting from
	// []Instance -> []Interface as doing so generally requires some go
	// helpers.
	// Instead, it's easiest (though painful to read and write) to iterate over
	// all functions that return slices and append them one at a time.
	for _, obj := range ClusterRole(dot) {
		manifests = append(manifests, obj)
	}

	for _, obj := range ClusterRoleBindings(dot) {
		manifests = append(manifests, obj)
	}

	for _, obj := range Roles(dot) {
		manifests = append(manifests, obj)
	}

	for _, obj := range RoleBindings(dot) {
		manifests = append(manifests, obj)
	}

	return manifests
}
