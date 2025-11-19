// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:namespace=multicluster
// +gotohelm:filename=_chart.go.tpl
package multicluster

import (
	"embed"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/redpanda-data/redpanda-operator/gotohelm"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

var (
	// Scheme is a [runtime.Scheme] with the appropriate extensions to load all
	// objects produced by the redpanda chart.
	Scheme = runtime.NewScheme()

	//go:embed Chart.yaml
	//go:embed files/*
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
}

func render(dot *helmette.Dot) []kube.Object {
	Validate(dot)

	manifests := []kube.Object{
		Service(dot),
		ServiceAccount(dot),
		Deployment(dot),
		PreInstallCRDJob(dot),
		CRDJobServiceAccount(dot),
	}

	for _, cr := range ClusterRoles(dot) {
		manifests = append(manifests, &cr)
	}

	for _, crb := range ClusterRoleBindings(dot) {
		manifests = append(manifests, &crb)
	}

	return manifests
}

// +gotohelm:ignore=true
func must(err error) {
	if err != nil {
		panic(err)
	}
}
