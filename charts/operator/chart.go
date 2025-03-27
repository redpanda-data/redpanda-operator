// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_chart.go.tpl
package operator

import (
	"embed"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

var (
	// Scheme is a [runtime.Scheme] with the appropriate extensions to load all
	// objects produced by the redpanda chart.
	Scheme = runtime.NewScheme()

	//go:embed Chart.yaml
	//go:embed templates/*
	//go:embed values.schema.json
	//go:embed values.yaml
	ChartFiles embed.FS

	// Chart is the go version of the redpanda helm chart.
	Chart = gotohelm.MustLoad(ChartFiles, render)
)

// +gotohelm:ignore=true
func init() {
	must(scheme.AddToScheme(Scheme))
	must(certmanagerv1.AddToScheme(Scheme))
	must(monitoringv1.AddToScheme(Scheme))
}

func render(dot *helmette.Dot) []kube.Object {
	manifests := []kube.Object{
		Issuer(dot),
		Certificate(dot),
		ConfigMap(dot),
		MetricsService(dot),
		WebhookService(dot),
		MutatingWebhookConfiguration(dot),
		ValidatingWebhookConfiguration(dot),
		ServiceAccount(dot),
		ServiceMonitor(dot),
		Deployment(dot),
	}

	for _, role := range Roles(dot) {
		manifests = append(manifests, &role)
	}

	for _, cr := range ClusterRoles(dot) {
		manifests = append(manifests, &cr)
	}

	for _, rb := range RoleBindings(dot) {
		manifests = append(manifests, &rb)
	}

	for _, crb := range ClusterRoleBindings(dot) {
		manifests = append(manifests, &crb)
	}

	// NB: This slice may contain nil interfaces!
	// Filtering happens elsewhere, don't call this function directly if you
	// can avoid it.
	return manifests
}

// +gotohelm:ignore=true
func must(err error) {
	if err != nil {
		panic(err)
	}
}
