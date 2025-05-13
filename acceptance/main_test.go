// Copyright 2025 Redpanda Data, Inc.
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
	"slices"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"

	_ "github.com/redpanda-data/redpanda-operator/acceptance/steps"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/harpoon/providers"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

var (
	imageRepo = "localhost/redpanda-operator"
	imageTag  = "dev"
)

func getSuite(t *testing.T) *framework.Suite {
	suit, err := setupSuite()
	require.NoError(t, err)
	return suit
}

var setupSuite = sync.OnceValues(func() (*framework.Suite, error) {
	return framework.SuiteBuilderFromFlags().
		RegisterProvider("eks", framework.NoopProvider).
		RegisterProvider("gke", framework.NoopProvider).
		RegisterProvider("aks", framework.NoopProvider).
		RegisterProvider("k3d", providers.NewK3D(5).RetainCluster()).
		WithDefaultProvider("k3d").
		WithImportedImages([]string{
			"localhost/redpanda-operator:dev",
			"docker.redpanda.com/redpandadata/redpanda-operator:v2.3.9-24.3.11",
			"docker.redpanda.com/redpandadata/redpanda:v24.3.11",
			"docker.redpanda.com/redpandadata/redpanda:v25.1.1",
			"quay.io/jetstack/cert-manager-controller:v1.14.2",
			"quay.io/jetstack/cert-manager-cainjector:v1.14.2",
			"quay.io/jetstack/cert-manager-startupapicheck:v1.14.2",
			"quay.io/jetstack/cert-manager-webhook:v1.14.2",
		}...).
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
		OnFeature(func(ctx context.Context, t framework.TestingT, tags ...framework.ParsedTag) {
			// this actually switches namespaces, run it first
			namespace := t.IsolateNamespace(ctx)

			if slices.ContainsFunc(tags, shouldSkipOperatorInstall) {
				return
			}
			t.Log("Installing default Redpanda operator chart")
			t.InstallLocalHelmChart(ctx, "../operator/chart", helm.InstallOptions{
				Name:      "redpanda-operator",
				Namespace: namespace,
				Values: map[string]any{
					"logLevel": "trace",
					"image": map[string]any{
						"tag":        imageTag,
						"repository": imageRepo,
					},
					"additionalCmdFlags": []string{
						// These are needed for running decommissioning tests.
						"--additional-controllers=all",
						"--unbind-pvcs-after=5s",
						// This is set to a lower timeout due to the way that our internal
						// admin client handles retries to brokers that are gone but still
						// remain in its internal broker list in-memory. Eventually the client
						// figures out which brokers are still active, but not until a large
						// chunk of time has past and a connection a no longer existing broker
						// times out. This makes the timeout substantially faster so that in
						// tests where brokers might intentionally go away we aren't sitting
						// for and additional 30+ seconds every reconciliation before the client's
						// broker list is pruned.
						"--cluster-connection-timeout=500ms",
					},
				},
			})
			t.Log("Successfully installed Redpanda operator chart")
		}).
		RegisterTag("operator", 1, OperatorTag).
		RegisterTag("cluster", 2, ClusterTag).
		ExitOnCleanupFailures().
		Build()
})

func TestMain(m *testing.M) {
	log.SetLogger(logr.Discard())
	os.Exit(m.Run())
}

func TestAcceptanceSuite(t *testing.T) {
	testutil.SkipIfNotAcceptance(t)

	getSuite(t).RunT(t)
}

func ClusterTag(ctx context.Context, t framework.TestingT, args ...string) context.Context {
	require.Greater(t, len(args), 0, "clusters tags can only be used with additional arguments")
	name := args[0]

	t.Logf("Installing cluster %q", name)
	t.ApplyManifest(ctx, filepath.Join("clusters", name))
	t.Logf("Finished installing cluster %q", name)

	return ctx
}

func OperatorTag(ctx context.Context, t framework.TestingT, args ...string) context.Context {
	require.Greater(t, len(args), 0, "operator tags can only be used with additional arguments")
	name := args[0]
	if name == "none" {
		t.Log("Skipping Redpanda operator installation")
		return ctx
	}

	t.Logf("Installing Redpanda operator chart: %q", name)
	t.InstallLocalHelmChart(ctx, "../operator/chart", helm.InstallOptions{
		Name:       "redpanda-operator",
		Namespace:  t.Namespace(),
		ValuesFile: filepath.Join("operator", fmt.Sprintf("%s.yaml", name)),
	})

	return ctx
}

func shouldSkipOperatorInstall(tag framework.ParsedTag) bool {
	return tag.Name == "operator"
}
