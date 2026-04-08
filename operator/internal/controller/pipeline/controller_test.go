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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/redpanda-data/common-go/license"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
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
			Finalizers: []string{FinalizerKey},
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

	// Verify the finalizer was removed (which means the object can be GC'd).
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	assert.NotContains(t, pipeline.Finalizers, FinalizerKey)
}

func TestRender_CommonAnnotations(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "annotated-pipeline",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
			CommonAnnotations: map[string]string{
				"compliance/owner": "platform-team",
				"compliance/env":   "production",
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

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
