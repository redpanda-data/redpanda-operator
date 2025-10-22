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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/acceptance/steps"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/harpoon/providers"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	operatorchart "github.com/redpanda-data/redpanda-operator/operator/chart"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil"
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
	// For now we need to use nightly images so that we can use shadow links
	steps.DefaultRedpandaRepo = "redpandadata/redpanda-nightly"
	steps.DefaultRedpandaTag = "v0.0.0-20251022gitd94b19f"

	return framework.SuiteBuilderFromFlags().
		Strict().
		RegisterProvider("eks", framework.NoopProvider).
		RegisterProvider("gke", framework.NoopProvider).
		RegisterProvider("aks", framework.NoopProvider).
		RegisterProvider("k3d", providers.NewK3D(5).RetainCluster()).
		WithDefaultProvider("k3d").
		WithImportedImages([]string{
			imageRepo + ":" + imageTag,
			"docker.redpanda.com/redpandadata/redpanda-operator:v2.4.5",
			"docker.redpanda.com/redpandadata/redpanda:v25.1.1",
			"docker.redpanda.com/redpandadata/redpanda:v25.2.1",
			"quay.io/jetstack/cert-manager-controller:v1.14.2",
			"quay.io/jetstack/cert-manager-cainjector:v1.14.2",
			"quay.io/jetstack/cert-manager-startupapicheck:v1.14.2",
			"quay.io/jetstack/cert-manager-webhook:v1.14.2",
		}...).
		WithSchemeFunctions(vectorizedv1alpha1.Install, redpandav1alpha1.Install, redpandav1alpha2.Install).
		WithHelmChart("https://charts.jetstack.io", "jetstack", "cert-manager", helm.InstallOptions{
			Name:            "cert-manager",
			Namespace:       "cert-manager",
			Version:         "v1.14.2",
			CreateNamespace: true,
			Values: map[string]any{
				"installCRDs": true,
				"global": map[string]any{
					// Make leader election more aggressive as cert-manager appears to
					// not release it when uninstalled.
					"leaderElection": map[string]any{
						"renewDeadline": "10s",
						"retryPeriod":   "5s",
					},
				},
			},
		}).
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
				Values: operatorchart.PartialValues{
					LogLevel: ptr.To("trace"),
					Image: &operatorchart.PartialImage{
						Tag:        ptr.To(imageTag),
						Repository: ptr.To(imageRepo),
					},
					CRDs: &operatorchart.PartialCRDs{
						Enabled:      ptr.To(true),
						Experimental: ptr.To(true),
					},
					VectorizedControllers: &operatorchart.PartialVectorizedControllers{
						Enabled: ptr.To(true),
					},
					AdditionalCmdFlags: []string{
						// For the v1 controllers since otherwise we'll attempt to always
						// pull the locally built operator which will result in errors
						"--configurator-image-pull-policy=IfNotPresent",
						// These are needed for running decommissioning tests.
						"--additional-controllers=nodeWatcher,decommission",
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
						"--enable-shadowlinks",
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
	otelutil.TestMain(m, "acceptance", testutil.TestTypeAcceptance)
}

func TestAcceptanceSuite(t *testing.T) {
	testutil.SkipIfNotAcceptance(t)

	getSuite(t).RunT(t)
}

func ClusterTag(ctx context.Context, t framework.TestingT, args ...string) context.Context {
	require.Greater(t, len(args), 0, "clusters tags can only be used with additional arguments")

	for _, name := range args {
		if variant := t.Variant(); variant != "" {
			name = filepath.Join(variant, name)
		}

		t.Logf("Installing cluster %q", name)
		t.ApplyManifest(ctx, duplicateManifests(t, filepath.Join("clusters", name)))
		t.Logf("Finished installing cluster %q", name)
	}

	return ctx
}

// we need to dup the manifests some place where we can write their munged
// content using the patched up values
func duplicateManifests(t framework.TestingT, directory string) string {
	tmp, err := os.MkdirTemp("", "manifests-*")
	require.NoError(t, err)
	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, os.RemoveAll(tmp))
	})

	require.NoError(t, filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		patched := steps.PatchManifest(t, string(data))
		newPath := filepath.Join(tmp, path)
		if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(newPath, []byte(patched), 0o644)
	}))

	return filepath.Join(tmp, directory)
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
