// Copyright 2026 Redpanda Data, Inc.
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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

const sharedOperatorNamespace = "redpanda-operator"

var (
	imageRepo = "localhost/redpanda-operator"
	imageTag  = "dev"
)

func getSuite(t *testing.T) *framework.Suite {
	suite, err := setupSuite()
	require.NoError(t, err)
	return suite
}

var setupSuite = sync.OnceValues(func() (*framework.Suite, error) {
	steps.DefaultRedpandaRepo = os.Getenv("TEST_REDPANDA_REPO")
	steps.DefaultRedpandaTag = os.Getenv("TEST_REDPANDA_VERSION")
	steps.OperatorNamespace = sharedOperatorNamespace

	builder := framework.SuiteBuilderFromFlags().
		Strict().
		RegisterProvider("eks", framework.NoopProvider).
		RegisterProvider("gke", framework.NoopProvider).
		RegisterProvider("aks", framework.NoopProvider).
		RegisterProvider("k3d", providers.NewK3D(5).RetainCluster()).
		WithDefaultProvider("k3d").
		WithImportedImages([]string{
			imageRepo + ":" + imageTag,
			steps.DefaultRedpandaRepo + ":" + steps.DefaultRedpandaTag,
			"redpandadata/redpanda-operator:v2.4.5",
			// Operator images used by upgrade features (overridden from docker.redpanda.com).
			"redpandadata/redpanda-operator:v25.1.3",
			"redpandadata/redpanda-operator:v25.2.2",
			"redpandadata/redpanda-operator:v25.3.1",
			"redpandadata/redpanda:v25.1.1",
			"redpandadata/redpanda:v25.2.1",
			// Images used by upgrade and upgrade-regressions features.
			"redpandadata/redpanda:v25.2.11",
			"redpandadata/redpanda-unstable:v25.3.1-rc4",
			"quay.io/jetstack/cert-manager-controller:v1.14.2",
			"quay.io/jetstack/cert-manager-cainjector:v1.14.2",
			"quay.io/jetstack/cert-manager-startupapicheck:v1.14.2",
			"quay.io/jetstack/cert-manager-webhook:v1.14.2",
		}...).
		WithSchemeFunctions(vectorizedv1alpha1.Install, redpandav1alpha1.Install, redpandav1alpha2.Install)

	// In multicluster setup mode, skip cert-manager and operator installation
	// as each vCluster instance manages its own.
	if !testutil.MultiClusterSetupOnly() {
		builder = builder.
			WithHelmChart("https://charts.jetstack.io", "jetstack", "cert-manager", helm.InstallOptions{
				Name:            "cert-manager",
				Namespace:       "cert-manager",
				Version:         "v1.14.2",
				CreateNamespace: true,
				Values: map[string]any{
					"installCRDs": true,
					"global": map[string]any{
						"leaderElection": map[string]any{
							"renewDeadline": "10s",
							"retryPeriod":   "5s",
						},
					},
				},
			}).
			AfterSetup(waitForCertManagerWebhook).
			AfterSetup(installSharedOperator)
	}

	builder = builder.
		OnFeature(func(ctx context.Context, t framework.TestingT, tags ...framework.ParsedTag) {
			// this actually switches namespaces, run it first
			t.IsolateNamespace(ctx)
		}).
		OnDiagnostics(func(ctx context.Context, t framework.TestingT) {
			// Only dump shared operator logs for features that use the
			// shared operator. Features with @vcluster or @multicluster
			// run their own operators inside vclusters.
			for _, tag := range t.FeatureTags() {
				if tag == "vcluster" || tag == "multicluster" {
					return
				}
			}
			dumpSharedOperatorLogs(ctx, t)
		}).
		RegisterTag("cluster", 2, ClusterTag).
		ExitOnCleanupFailures()

	if testutil.MultiClusterSetupOnly() {
		builder = builder.SkipCleanup()
	}

	return builder.Build()
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

// installSharedOperator installs a single Redpanda operator instance that is
// shared by all features that don't use @operator:none. This avoids creating
// one operator per feature and allows parallel features to share a single
// controller.
func installSharedOperator(ctx context.Context, restConfig *rest.Config) error {
	helmClient, err := helm.New(helm.Options{KubeConfig: restConfig})
	if err != nil {
		return err
	}

	_, err = helmClient.Install(ctx, "../operator/chart", helm.InstallOptions{
		Name:            "redpanda-operator",
		Namespace:       sharedOperatorNamespace,
		CreateNamespace: true,
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
				"--configurator-image-pull-policy=IfNotPresent",
				"--additional-controllers=nodeWatcher,decommission",
				"--unbind-pvcs-after=5s",
				"--cluster-connection-timeout=500ms",
				"--enable-shadowlinks",
			},
		},
	})
	// Tolerate "already installed" errors from rerun-fails retries where
	// the operator was installed in the first run.
	if err != nil && strings.Contains(err.Error(), "cannot re-use") {
		return nil
	}
	return err
}

// dumpSharedOperatorLogs collects logs from the shared operator pod to help
// debug cases where the operator isn't reconciling resources.
func dumpSharedOperatorLogs(ctx context.Context, t framework.TestingT) {
	var pods corev1.PodList
	if err := t.List(ctx, &pods, client.InNamespace(sharedOperatorNamespace)); err != nil {
		t.Logf("[diagnostics/shared-operator] error listing pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		t.Logf("[diagnostics/shared-operator] pod %s: phase=%s", pod.Name, pod.Status.Phase)
		for _, cs := range pod.Status.ContainerStatuses {
			t.Logf("[diagnostics/shared-operator]   container %s: ready=%v restarts=%d", cs.Name, cs.Ready, cs.RestartCount)
			if cs.State.Waiting != nil {
				t.Logf("[diagnostics/shared-operator]   waiting: %s", cs.State.Waiting.Reason)
			}
			if cs.State.Terminated != nil {
				t.Logf("[diagnostics/shared-operator]   terminated: exitCode=%d reason=%s", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
			}
		}
	}

	// Dump the last 500 lines of operator logs.
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			logReq := kubernetes.NewForConfigOrDie(t.RestConfig()).CoreV1().Pods(sharedOperatorNamespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: ptr.To(int64(500)),
			})
			stream, err := logReq.Stream(ctx)
			if err != nil {
				t.Logf("[diagnostics/shared-operator] error streaming logs for %s/%s: %v", pod.Name, container.Name, err)
				continue
			}
			logs, err := io.ReadAll(stream)
			_ = stream.Close()
			if err != nil {
				continue
			}
			t.Logf("[diagnostics/shared-operator] logs for %s/%s (last 500 lines):\n%s", pod.Name, container.Name, string(logs))
		}
	}
}

func waitForCertManagerWebhook(ctx context.Context, restConfig *rest.Config) error {
	c, err := client.New(restConfig, client.Options{})
	if err != nil {
		return err
	}
	return testutil.WaitForCertManagerWebhook(ctx, c, 2*time.Minute)
}
