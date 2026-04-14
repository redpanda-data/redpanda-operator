// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/redpanda-data/common-go/license"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func setupTestEnv(t *testing.T) *kube.Ctl {
	t.Helper()

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: controller.UnifiedScheme,
		},
	})

	require.NoError(t, kube.ApplyAllAndWait(t.Context(), ctl, func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established {
				return cond.Status == apiextensionsv1.ConditionTrue, nil
			}
		}
		return false, nil
	}, crds.All()...))

	return ctl
}

func TestReconcile_NoLicense(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-no-license"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

	c := &Controller{
		Ctl:             ctl,
		LicenseFilePath: "", // No license
	}

	result, err := c.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: kube.AsKey(pipeline),
	})
	require.NoError(t, err)
	assert.Equal(t, time.Minute, result.RequeueAfter, "should requeue for license retry")

	// Verify status shows license invalid.
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	assert.Equal(t, redpandav1alpha2.PipelinePhasePending, pipeline.Status.Phase)
	require.Len(t, pipeline.Status.Conditions, 1)
	assert.Equal(t, redpandav1alpha2.PipelineConditionReady, pipeline.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionFalse, pipeline.Status.Conditions[0].Status)
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, pipeline.Status.Conditions[0].Reason)

	// Verify no Deployment was created.
	var deployments appsv1.DeploymentList
	require.NoError(t, ctl.List(t.Context(), ns.Name, &deployments))
	assert.Empty(t, deployments.Items)
}

func TestReconcile_InvalidLicenseFile(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-bad-license"},
	})
	require.NoError(t, err)

	// Write a bad license file.
	dir := t.TempDir()
	path := filepath.Join(dir, "license")
	require.NoError(t, os.WriteFile(path, []byte("not-a-valid-license"), 0o644))

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

	c := &Controller{
		Ctl:             ctl,
		LicenseFilePath: path,
	}

	result, err := c.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: kube.AsKey(pipeline),
	})
	require.NoError(t, err)
	assert.Equal(t, time.Minute, result.RequeueAfter)

	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, pipeline.Status.Conditions[0].Reason)
	assert.Contains(t, pipeline.Status.Conditions[0].Message, "failed to read license")
}

func TestReconcile_Deletion(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deletion"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:       ns.Name,
			Namespace:  ns.Name,
			Finalizers: []string{finalizerKey},
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

	// Trigger deletion.
	require.NoError(t, ctl.Delete(t.Context(), pipeline))

	c := &Controller{
		Ctl:             ctl,
		LicenseFilePath: "", // License doesn't matter for deletion
	}

	// Reconcile the deletion.
	_, err = c.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: kube.AsKey(pipeline),
	})
	require.NoError(t, err)

	// Verify the object was GC'd (finalizer removal allows API server to delete it).
	err = ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline)
	assert.True(t, apierrors.IsNotFound(err), "expected object to be garbage collected after finalizer removal")
}

func TestRender_GoldenFiles(t *testing.T) {
	golden := testutil.NewTxTar(t, "testdata/controller-tests.golden.txtar")

	testCases := []struct {
		name     string
		pipeline *redpandav1alpha2.Pipeline
	}{
		{
			name: "basic-pipeline",
			pipeline: &redpandav1alpha2.Pipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "basic-pipeline",
					Namespace: "default",
				},
				Spec: redpandav1alpha2.PipelineSpec{
					ConfigYAML: "input:\n  generate:\n    mapping: 'root.message = \"hello\"'\n    interval: \"5s\"\noutput:\n  stdout: {}\n",
				},
			},
		},
		{
			name: "pipeline-with-annotations",
			pipeline: &redpandav1alpha2.Pipeline{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "annotated-pipeline",
					Namespace: "default",
				},
				Spec: redpandav1alpha2.PipelineSpec{
					ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
					Annotations: map[string]string{
						"ad.datadoghq.com/connect.checks": "openmetrics",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labels := Labels(tc.pipeline)
			r := &render{
				pipeline: tc.pipeline,
				labels:   labels,
			}

			objs, err := r.Render(t.Context())
			require.NoError(t, err)

			manifest, err := yaml.Marshal(objs)
			require.NoError(t, err)

			golden.AssertGolden(t, testutil.YAML, tc.name, manifest)
		})
	}
}

func TestReconcile_DeletionGC(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-deletion-gc"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gc-pipeline",
			Namespace:  ns.Name,
			Finalizers: []string{finalizerKey},
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

	// Create child resources that the syncer would manage.
	syncer := &kube.Syncer{
		Ctl:       ctl,
		Namespace: ns.Name,
		Renderer: &render{
			pipeline: pipeline,
			labels:   Labels(pipeline),
		},
		Owner:           *metav1.NewControllerRef(pipeline, redpandav1alpha2.SchemeGroupVersion.WithKind("Pipeline")),
		OwnershipLabels: Labels(pipeline),
	}
	_, err = syncer.Sync(t.Context())
	require.NoError(t, err)

	// Verify child objects exist.
	objects := scrapeControllerObjects(t, ctl, pipeline)
	require.NotEmpty(t, objects, "expected child resources to exist before deletion")

	// Trigger deletion.
	require.NoError(t, ctl.Delete(t.Context(), pipeline))

	c := &Controller{Ctl: ctl}

	// Reconcile the deletion a few times.
	doneCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		doneCh <- ctl.DeleteAndWait(ctx, pipeline)
		close(doneCh)
	}()

	for range 3 {
		_, err = c.Reconcile(t.Context(), ctrl.Request{
			NamespacedName: kube.AsKey(pipeline),
		})
		require.NoError(t, err)
	}

	require.NoError(t, <-doneCh)

	// Assert that all child resources have been GC'd.
	require.Empty(t, scrapeControllerObjects(t, ctl, pipeline))
}

// scrapeControllerObjects finds all objects created by the pipeline controller using ownership labels.
func scrapeControllerObjects(t *testing.T, ctl *kube.Ctl, pipeline *redpandav1alpha2.Pipeline) []kube.Object {
	ownershipLabels := Labels(pipeline)

	var objects []kube.Object
	for _, objType := range Types() {
		// Skip PodMonitor as it's optional (only created when monitoring.enabled is true).
		if _, ok := objType.(*monitoringv1.PodMonitor); ok {
			continue
		}
		list, err := kube.ListFor(ctl.Scheme(), objType)
		require.NoError(t, err)

		err = ctl.List(
			t.Context(),
			pipeline.Namespace,
			list,
			client.MatchingLabels(ownershipLabels),
		)
		require.NoError(t, err)

		objs, err := kube.Items[kube.Object](list)
		require.NoError(t, err)

		for _, obj := range objs {
			cleanObjectForGolden(ctl.Scheme(), obj)
			objects = append(objects, obj)
		}
	}

	slices.SortFunc(objects, func(i, j client.Object) int {
		iKey := fmt.Sprintf("%T%s%s", i, i.GetNamespace(), i.GetName())
		jKey := fmt.Sprintf("%T%s%s", j, j.GetNamespace(), j.GetName())
		return strings.Compare(iKey, jKey)
	})

	return objects
}

// cleanObjectForGolden removes dynamic fields that change between test runs.
func cleanObjectForGolden(scheme *runtime.Scheme, obj client.Object) {
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		panic(err)
	}
	obj.GetObjectKind().SetGroupVersionKind(gvks[0])

	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetFinalizers(nil)
	obj.SetGeneration(0)
	obj.SetManagedFields(nil)
	obj.SetOwnerReferences(nil)
	obj.SetResourceVersion("")
	obj.SetUID("")
}

func TestRender_CommonAnnotations(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "annotated-pipeline",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}

	labels := Labels(pipeline)
	r := &render{
		pipeline: pipeline,
		labels:   labels,
		commonAnnotations: map[string]string{
			"compliance/owner": "platform-team",
			"compliance/env":   "production",
		},
	}

	// Verify annotations propagate to all rendered objects.
	objs, err := r.Render(t.Context())
	require.NoError(t, err)
	require.Len(t, objs, 2, "expected ConfigMap and Deployment")

	for _, obj := range objs {
		annotations := obj.(metav1.ObjectMetaAccessor).GetObjectMeta().GetAnnotations()
		assert.Equal(t, "platform-team", annotations["compliance/owner"],
			"commonAnnotations should propagate to %T", obj)
		assert.Equal(t, "production", annotations["compliance/env"],
			"commonAnnotations should propagate to %T", obj)
	}

	// Verify pod template also has annotations.
	dp := objs[1].(*appsv1.Deployment)
	podAnnotations := dp.Spec.Template.ObjectMeta.Annotations
	assert.Equal(t, "platform-team", podAnnotations["compliance/owner"])
}

func TestRender_PodAnnotations(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dd-pipeline",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
			Annotations: map[string]string{
				"ad.datadoghq.com/connect.checks": `{"openmetrics":{"instances":[{"openmetrics_endpoint":"http://%%host%%:4195/metrics","namespace":"redpanda_connect","metrics":[".*"]}]}}`,
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{
		pipeline: pipeline,
		labels:   labels,
		commonAnnotations: map[string]string{
			"compliance/owner": "platform-team",
		},
	}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	// ConfigMap should only have commonAnnotations, not pod annotations.
	cm := objs[0].(*corev1.ConfigMap)
	assert.Equal(t, "platform-team", cm.Annotations["compliance/owner"])
	assert.Empty(t, cm.Annotations["ad.datadoghq.com/connect.checks"],
		"spec.annotations should not propagate to ConfigMap")

	// Pod template should have both commonAnnotations and spec.annotations.
	dp := objs[1].(*appsv1.Deployment)
	podAnn := dp.Spec.Template.ObjectMeta.Annotations
	assert.Equal(t, "platform-team", podAnn["compliance/owner"],
		"commonAnnotations should be on pod template")
	assert.Contains(t, podAnn["ad.datadoghq.com/connect.checks"], "openmetrics",
		"spec.annotations should be on pod template")

	// Deployment metadata should only have commonAnnotations.
	assert.Empty(t, dp.Annotations["ad.datadoghq.com/connect.checks"],
		"spec.annotations should not propagate to Deployment metadata")
}

func TestRender_PodAnnotations_Override(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "override-pipeline",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
			Annotations: map[string]string{
				"shared-key": "from-pipeline",
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{
		pipeline: pipeline,
		labels:   labels,
		commonAnnotations: map[string]string{
			"shared-key": "from-common",
		},
	}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	podAnn := dp.Spec.Template.ObjectMeta.Annotations
	assert.Equal(t, "from-pipeline", podAnn["shared-key"],
		"per-pipeline annotations should override commonAnnotations on pod template")
}

func TestRender_Deployment_HasLintInitContainer(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lint-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)

	require.Len(t, dp.Spec.Template.Spec.InitContainers, 1, "expected one init container")
	init := dp.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, "lint", init.Name)
	assert.Equal(t, []string{"/redpanda-connect", "lint", "/config/connect.yaml"}, init.Command)
	assert.Equal(t, redpandav1alpha2.PipelineDefaultImage, init.Image, "init container should use same image as main container")
	assert.Equal(t, corev1.TerminationMessageFallbackToLogsOnError, init.TerminationMessagePolicy)

	require.Len(t, init.VolumeMounts, 1)
	assert.Equal(t, "config", init.VolumeMounts[0].Name)
	assert.Equal(t, "/config", init.VolumeMounts[0].MountPath)
	assert.True(t, init.VolumeMounts[0].ReadOnly)
}

func TestRender_ConfigMap(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "render-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ConfigFiles: map[string]string{
				"extra.yaml": "some: config",
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	cm := objs[0].(*corev1.ConfigMap)
	assert.Equal(t, "render-test", cm.Name)
	assert.Equal(t, pipeline.Spec.ConfigYAML, cm.Data["connect.yaml"])
	assert.Equal(t, "some: config", cm.Data["extra.yaml"])
}

func TestRender_ConfigMap_ReservedKey(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reserved-key-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ConfigFiles: map[string]string{
				"connect.yaml": "should fail",
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	_, err := r.Render(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect.yaml")
}

func TestRender_Deployment_Defaults(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	assert.Equal(t, int32(1), *dp.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, dp.Spec.Strategy.Type)
	assert.Equal(t, redpandav1alpha2.PipelineDefaultImage, dp.Spec.Template.Spec.Containers[0].Image)
	assert.NotNil(t, dp.Spec.Template.Spec.Containers[0].ReadinessProbe)
}

func TestRender_Deployment_Paused(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "paused-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			Paused:     true,
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	assert.Equal(t, int32(0), *dp.Spec.Replicas)
}

func TestRender_Deployment_SecretRef(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			SecretRef: []corev1.LocalObjectReference{
				{Name: "my-creds"},
				{Name: "other-creds"},
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	envFrom := dp.Spec.Template.Spec.Containers[0].EnvFrom
	require.Len(t, envFrom, 2)
	assert.Equal(t, "my-creds", envFrom[0].SecretRef.Name)
	assert.Equal(t, "other-creds", envFrom[1].SecretRef.Name)
}

func TestRender_Deployment_Zones(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "zone-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			Zones:      []string{"us-east-1a", "us-east-1b"},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	// Verify node affinity.
	require.NotNil(t, dp.Spec.Template.Spec.Affinity)
	require.NotNil(t, dp.Spec.Template.Spec.Affinity.NodeAffinity)
	terms := dp.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	require.Len(t, terms, 1)
	assert.Equal(t, zoneTopologyKey, terms[0].MatchExpressions[0].Key)
	assert.Equal(t, []string{"us-east-1a", "us-east-1b"}, terms[0].MatchExpressions[0].Values)

	// Verify topology spread.
	require.Len(t, dp.Spec.Template.Spec.TopologySpreadConstraints, 1)
	assert.Equal(t, zoneTopologyKey, dp.Spec.Template.Spec.TopologySpreadConstraints[0].TopologyKey)
}

// PodDisruptionBudget tests.

func TestRender_PDB_NotConfigured(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "no-pdb", Namespace: "default"},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}

	r := &render{pipeline: pipeline, labels: Labels(pipeline)}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	// Should only have ConfigMap + Deployment, no PDB.
	for _, obj := range objs {
		assert.NotEqual(t, "PodDisruptionBudget", obj.GetObjectKind().GroupVersionKind().Kind)
	}
}

func TestRender_PDB_MaxUnavailable(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pdb-max", Namespace: "default"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			Budget: &redpandav1alpha2.PipelineBudget{
				MaxUnavailable: 1,
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	// Find the PDB.
	var pdb *policyv1.PodDisruptionBudget
	for _, obj := range objs {
		if p, ok := obj.(*policyv1.PodDisruptionBudget); ok {
			pdb = p
		}
	}
	require.NotNil(t, pdb, "expected a PodDisruptionBudget in rendered objects")
	assert.Equal(t, "pdb-max", pdb.Name)
	assert.Equal(t, "default", pdb.Namespace)
	assert.Equal(t, labels, pdb.Labels)
	assert.Equal(t, labels, pdb.Spec.Selector.MatchLabels)
	require.NotNil(t, pdb.Spec.MaxUnavailable)
	assert.Equal(t, int32(1), pdb.Spec.MaxUnavailable.IntVal)
	assert.Nil(t, pdb.Spec.MinAvailable)
}

func TestRender_PDB_ZeroMaxUnavailable(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pdb-zero", Namespace: "default"},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			Budget: &redpandav1alpha2.PipelineBudget{
				MaxUnavailable: 0,
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	var pdb *policyv1.PodDisruptionBudget
	for _, obj := range objs {
		if p, ok := obj.(*policyv1.PodDisruptionBudget); ok {
			pdb = p
		}
	}
	require.NotNil(t, pdb, "expected a PodDisruptionBudget in rendered objects")
	require.NotNil(t, pdb.Spec.MaxUnavailable)
	assert.Equal(t, int32(0), pdb.Spec.MaxUnavailable.IntVal)
}

// License validation unit tests.

func TestValidateLicenseNoPath(t *testing.T) {
	c := &Controller{LicenseFilePath: ""}
	err := c.validateLicense()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no license configured")
}

func TestValidateLicenseBadPath(t *testing.T) {
	c := &Controller{LicenseFilePath: "/nonexistent/path/to/license"}
	err := c.validateLicense()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read license")
}

func TestValidateLicenseInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "license")
	require.NoError(t, os.WriteFile(path, []byte("not-a-valid-license"), 0o644))

	c := &Controller{LicenseFilePath: path}
	err := c.validateLicense()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read license")
}

func TestValidateLicenseOpenSource(t *testing.T) {
	l := license.OpenSourceLicense
	assert.False(t, l.AllowsEnterpriseFeatures())
}

func TestValidateLicenseExpired(t *testing.T) {
	err := license.CheckExpiration(time.Now().Add(-24 * time.Hour))
	require.Error(t, err)
}

func TestValidateLicenseNotExpired(t *testing.T) {
	err := license.CheckExpiration(time.Now().Add(24 * time.Hour))
	require.NoError(t, err)
}

func TestV0LicenseIncludesAllProducts(t *testing.T) {
	l := &license.V0RedpandaLicense{
		Type:   license.V0LicenseTypeEnterprise,
		Expiry: time.Now().Add(24 * time.Hour).Unix(),
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1LicenseWithConnectProduct(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1LicenseWithoutConnectProduct(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{},
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.False(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1TrialLicenseWithConnect(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeFreeTrial,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.True(t, l.AllowsEnterpriseFeatures())
	assert.True(t, l.IncludesProduct(license.ProductConnect))
}

func TestV1ExpiredEnterpriseLicense(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeEnterprise,
		Expiry:   time.Now().Add(-24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.False(t, l.AllowsEnterpriseFeatures())
}

func TestV1OpenSourceLicenseType(t *testing.T) {
	l := &license.V1RedpandaLicense{
		Type:     license.LicenseTypeOpenSource,
		Expiry:   time.Now().Add(24 * time.Hour).Unix(),
		Products: []license.Product{license.ProductConnect},
	}
	assert.False(t, l.AllowsEnterpriseFeatures())
}

// PodMonitor tests.

func TestRender_PodMonitor_Disabled(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-disabled", Namespace: "default"},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}

	r := &render{
		pipeline:   pipeline,
		labels:     Labels(pipeline),
		monitoring: MonitoringConfig{Enabled: false},
	}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)
	assert.Len(t, objs, 2, "only ConfigMap + Deployment when monitoring disabled")
}

func TestRender_PodMonitor_Enabled(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-enabled", Namespace: "default"},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}

	r := &render{
		pipeline: pipeline,
		labels:   Labels(pipeline),
		monitoring: MonitoringConfig{
			Enabled:        true,
			ScrapeInterval: "30s",
			Labels:         map[string]string{"team": "platform"},
		},
	}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)
	require.Len(t, objs, 3, "ConfigMap + Deployment + PodMonitor")

	pm := objs[2].(*monitoringv1.PodMonitor)
	assert.Equal(t, "pm-enabled", pm.Name)
	assert.Equal(t, "default", pm.Namespace)
	assert.Equal(t, "platform", pm.Labels["team"])
	assert.Equal(t, "redpanda-connect", pm.Labels["app.kubernetes.io/name"])
	require.Len(t, pm.Spec.PodMetricsEndpoints, 1)
	assert.Equal(t, "/metrics", pm.Spec.PodMetricsEndpoints[0].Path)
	assert.Equal(t, "http", *pm.Spec.PodMetricsEndpoints[0].Port)
	assert.Equal(t, monitoringv1.Duration("30s"), pm.Spec.PodMetricsEndpoints[0].Interval)
	assert.Equal(t, Labels(pipeline), pm.Spec.Selector.MatchLabels)
}

func TestRender_PodMonitor_CommonAnnotations(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-annotated", Namespace: "default"},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}

	r := &render{
		pipeline: pipeline,
		labels:   Labels(pipeline),
		commonAnnotations: map[string]string{
			"compliance/owner": "platform-team",
		},
		monitoring: MonitoringConfig{Enabled: true},
	}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)
	require.Len(t, objs, 3)

	pm := objs[2].(*monitoringv1.PodMonitor)
	assert.Equal(t, "platform-team", pm.Annotations["compliance/owner"])
}

func TestRender_PodMonitor_NoScrapeInterval(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "pm-no-interval", Namespace: "default"},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}

	r := &render{
		pipeline:   pipeline,
		labels:     Labels(pipeline),
		monitoring: MonitoringConfig{Enabled: true},
	}
	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	pm := objs[2].(*monitoringv1.PodMonitor)
	assert.Empty(t, pm.Spec.PodMetricsEndpoints[0].Interval, "empty interval uses Prometheus default")
}
