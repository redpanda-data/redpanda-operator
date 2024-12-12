// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"

	_ "github.com/redpanda-data/redpanda-operator/acceptance/steps"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

var (
	imageRepo = "localhost/redpanda-operator"
	imageTag  = "dev"
	suite     *framework.Suite
)

func TestMain(m *testing.M) {
	log.SetLogger(logr.Discard())

	var err error

	suite, err = framework.SuiteBuilderFromFlags().
		RegisterProvider("eks", framework.NoopProvider).
		RegisterProvider("gke", framework.NoopProvider).
		RegisterProvider("aks", framework.NoopProvider).
		RegisterProvider("k3d", framework.NoopProvider).
		WithDefaultProvider("k3d").
		WithSchemeFunctions(redpandav1alpha1.AddToScheme, redpandav1alpha2.AddToScheme).
		WithHelmChart("https://charts.jetstack.io", "jetstack", "cert-manager", helm.InstallOptions{
			Name:            "cert-manager",
			Namespace:       "cert-manager",
			Version:         "v1.14.2",
			CreateNamespace: true,
			Values: map[string]any{
				"installCRDs": true,
			},
		}).
		WithCRDDirectory("../operator/config/crd/bases").
		WithCRDDirectory("../operator/config/crd/bases/toolkit.fluxcd.io").
		OnFeature(func(ctx context.Context, t framework.TestingT) {
			t.Log("Installing Redpanda operator chart")
			t.InstallHelmChart(ctx, "https://charts.redpanda.com", "redpanda", "operator", helm.InstallOptions{
				Name:      "redpanda-operator",
				Namespace: t.IsolateNamespace(ctx),
				Values: map[string]any{
					"logLevel": "trace",
					"image": map[string]any{
						"tag":        imageTag,
						"repository": imageRepo,
					},
					"rbac": map[string]any{
						"createAdditionalControllerCRs": true,
						"createRPKBundleCRs":            true,
					},
					"additionalCmdFlags": []string{"--additional-controllers=all", "--enable-helm-controllers=false", "--force-defluxed-mode"},
				},
			})
			t.Log("Successfully installed Redpanda operator chart")
		}).
		RegisterTag("cluster", 1, ClusterTag).
		ExitOnCleanupFailures().
		Build()
	if err != nil {
		fmt.Printf("error running test suite: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestSuite(t *testing.T) {
	suite.RunT(t)
}

func ClusterTag(ctx context.Context, t framework.TestingT, args ...string) context.Context {
	require.Greater(t, len(args), 0, "clusters tags can only be used with additional arguments")
	name := args[0]

	t.Logf("Installing cluster %q", name)
	t.ApplyManifest(ctx, filepath.Join("clusters", name))
	t.Logf("Finished installing cluster %q", name)

	return ctx
}
