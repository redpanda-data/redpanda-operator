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
	"os"
	"path/filepath"
	"sync"
	"testing"

	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	_ "github.com/redpanda-data/redpanda-operator/acceptance/steps"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
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
		RegisterProvider("k3d", framework.NoopProvider).
		WithDefaultProvider("k3d").
		WithSchemeFunctions(
			redpandav1alpha1.AddToScheme,
			redpandav1alpha2.AddToScheme,
			sourcev1beta2.AddToScheme,
		).
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
			namespace := t.IsolateNamespace(ctx)

			t.Log("Installing Redpanda operator chart")
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

			// As CRDs are installed and cleaned up in a different lifecycle,
			// we need to clear our the HelmRepository created by the operator
			// before removing the operator itself. If we don't, the CRD
			// removal will get stuck waiting on a finalizer that the
			// (uninstalled) operator would have removed.
			t.Cleanup(func(ctx context.Context) {
				t.Log("removing redpanda-repository HelmRepository")
				err := t.Delete(ctx, &sourcev1beta2.HelmRepository{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "redpanda-repository",
						Namespace: namespace,
					},
				})
				if err != nil && !apierrors.IsNotFound(err) {
					require.NoError(t, err)
				}
			})
		}).
		RegisterTag("cluster", 1, ClusterTag).
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
