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
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
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

	// Verify status shows license invalid: the dedicated License condition
	// carries the failure, and — with no Deployment ever created — Ready
	// mirrors it so a never-provisioned pipeline points straight at the cause.
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	assert.Equal(t, redpandav1alpha2.PipelinePhasePending, pipeline.Status.Phase)

	licenseCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionLicense)
	require.NotNil(t, licenseCond)
	assert.Equal(t, metav1.ConditionFalse, licenseCond.Status)
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, licenseCond.Reason)

	readyCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionReady)
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionFalse, readyCond.Status)
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, readyCond.Reason)

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
	licenseCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionLicense)
	require.NotNil(t, licenseCond)
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, licenseCond.Reason)
	assert.Contains(t, licenseCond.Message, "failed to read license")
}

func TestReconcile_InvalidLicenseKeepsManagedResources(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-license-cleanup"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cleanup-pipeline",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

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
	require.NotEmpty(t, scrapeControllerObjects(t, ctl, pipeline))

	c := &Controller{
		Ctl:             ctl,
		LicenseFilePath: "",
	}

	result, err := c.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: kube.AsKey(pipeline),
	})
	require.NoError(t, err)
	assert.Equal(t, time.Minute, result.RequeueAfter)
	// Reconcile failures never tear down running workloads: the
	// last-known-good children stay in place so data processing continues;
	// only deleting the Pipeline CR removes them.
	require.NotEmpty(t, scrapeControllerObjects(t, ctl, pipeline))

	// The license failure lands on the License condition; phase and Ready
	// come from the live Deployment (Provisioning in envtest, where no
	// deployment controller ever readies pods) — a license blip must not
	// report a live workload as Pending.
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	assert.Equal(t, redpandav1alpha2.PipelinePhaseProvisioning, pipeline.Status.Phase)
	licenseCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionLicense)
	require.NotNil(t, licenseCond)
	assert.Equal(t, metav1.ConditionFalse, licenseCond.Status)
	assert.Equal(t, redpandav1alpha2.PipelineReasonLicenseInvalid, licenseCond.Reason)
	readyCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionReady)
	require.NotNil(t, readyCond)
	assert.Equal(t, redpandav1alpha2.PipelineReasonProvisioning, readyCond.Reason,
		"Ready must reflect the live workload, not the license failure")
}

func TestReconcile_InvalidClusterRefKeepsManagedResources(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-clusterref-cleanup"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterref-cleanup-pipeline",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{Name: "missing-cluster"},
			},
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

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
	require.NotEmpty(t, scrapeControllerObjects(t, ctl, pipeline))

	c := &Controller{
		Ctl: ctl,
	}

	result, err := c.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: kube.AsKey(pipeline),
	})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter)
	// Resolution failures are surfaced on status but never tear down
	// running workloads; the last-known-good children keep processing data.
	require.NotEmpty(t, scrapeControllerObjects(t, ctl, pipeline))
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))

	// The failure lands on the ClusterRef condition; phase and Ready come
	// from the live Deployment (Provisioning in envtest).
	clusterRefCond := apimeta.FindStatusCondition(pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionClusterRef)
	require.NotNil(t, clusterRefCond)
	assert.Equal(t, metav1.ConditionFalse, clusterRefCond.Status)
	assert.Equal(t, redpandav1alpha2.PipelineReasonClusterRefInvalid, clusterRefCond.Reason)
	assert.Equal(t, redpandav1alpha2.PipelinePhaseProvisioning, pipeline.Status.Phase)
}

// TestScaleSubresource exercises the Pipeline's /scale subresource — the
// contract HorizontalPodAutoscaler, KEDA, and `kubectl scale` build on. An
// autoscaler-written scale must land on .spec.replicas (and from there on the
// Deployment), and the advertised status.selector must parse and match the
// pipeline's pod labels so per-pod metrics can be resolved.
func TestScaleSubresource(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-scale-subresource"},
	})
	require.NoError(t, err)

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scaled-pipeline",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))

	// Every status write flows through applyStatus, which stamps
	// status.selector — so even the license-less reconcile path (envtest
	// cannot mint a valid enterprise license) populates the scale selector.
	c := &Controller{Ctl: ctl, LicenseFilePath: ""}
	_, err = c.Reconcile(t.Context(), ctrl.Request{NamespacedName: kube.AsKey(pipeline)})
	require.NoError(t, err)

	// controller-runtime's subresource client speaks the same /scale endpoint
	// HPA, KEDA, and kubectl scale use.
	cl, err := client.New(ctl.RestConfig(), client.Options{Scheme: ctl.Scheme()})
	require.NoError(t, err)

	scale := &autoscalingv1.Scale{}
	require.NoError(t, cl.SubResource("scale").Get(t.Context(), pipeline, scale))
	assert.Equal(t, int32(1), scale.Spec.Replicas, "CRD defaulting should surface replicas=1 through /scale")

	// HPA calls labels.Parse on the advertised selector and matches it
	// against pods; it must parse and select the pipeline's pod labels.
	sel, err := labels.Parse(scale.Status.Selector)
	require.NoError(t, err)
	assert.True(t, sel.Matches(labels.Set(Labels(pipeline))),
		"scale selector %q must match the pipeline pod labels", scale.Status.Selector)

	// An autoscaler scaling out writes spec.replicas through /scale...
	scale.Spec.Replicas = 3
	require.NoError(t, cl.SubResource("scale").Update(t.Context(), pipeline, client.WithSubResourceBody(scale)))

	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	require.NotNil(t, pipeline.Spec.Replicas)
	assert.Equal(t, int32(3), *pipeline.Spec.Replicas)

	// ...and the sync path propagates it onto the Deployment (a licensed
	// Reconcile drives this same syncer).
	syncer, err := c.syncerFor(pipeline, c.rendererFor(pipeline, nil, nil, nil, ""))
	require.NoError(t, err)
	_, err = syncer.Sync(t.Context())
	require.NoError(t, err)

	var dp appsv1.Deployment
	require.NoError(t, ctl.Get(t.Context(), kube.ObjectKey{Name: pipeline.Name, Namespace: ns.Name}, &dp))
	require.NotNil(t, dp.Spec.Replicas)
	assert.Equal(t, int32(3), *dp.Spec.Replicas)

	// KEDA's scale-to-zero writes 0 the same way; the CRD's replicas default
	// must not resurrect it to 1.
	scale.Spec.Replicas = 0
	require.NoError(t, cl.SubResource("scale").Update(t.Context(), pipeline, client.WithSubResourceBody(scale)))
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))
	require.NotNil(t, pipeline.Spec.Replicas)
	assert.Equal(t, int32(0), *pipeline.Spec.Replicas)
	assert.Equal(t, int32(0), pipeline.GetReplicas())

	// paused wins over autoscaler-written replicas: the effective count parks
	// at zero while .spec.replicas stays owned by the autoscaler.
	pipeline.Spec.Replicas = ptr.To(int32(3))
	pipeline.Spec.Paused = true
	assert.Equal(t, int32(0), pipeline.GetReplicas())
}

// TestSetupWithManager exercises the real registration path — scheme types,
// field index, watches, and the PodMonitor CRD probe — which the reconcile
// tests never touch (they drive Reconcile directly). The probe must give the
// right answer BEFORE mgr.Start(): the previous implementation did a cached
// List there, which always fails with ErrCacheNotStarted and silently skipped
// the PodMonitor watch on every real deployment.
func TestSetupWithManager(t *testing.T) {
	ctl := setupTestEnv(t)

	newManager := func() ctrl.Manager {
		mgr, err := ctrl.NewManager(ctl.RestConfig(), ctrl.Options{
			Scheme:  controller.UnifiedScheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
			// Both managers register a controller named "pipeline";
			// controller-runtime's global name-uniqueness check is only about
			// metric labels, which don't matter here.
			Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		})
		require.NoError(t, err)
		return mgr
	}

	// Without the PodMonitor CRD installed: setup succeeds (the watch is
	// skipped) and the probe reports "not installed" — not a cache error.
	mgr := newManager()
	c := &Controller{Ctl: ctl}
	require.NoError(t, c.SetupWithManager(t.Context(), mgr, ""))
	assert.False(t, c.podMonitorCRDInstalled(t.Context(), mgr))

	// Install a minimal PodMonitor CRD. A fresh manager (fresh RESTMapper —
	// the lazy mapper caches negative lookups) must now detect it pre-start.
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
	}, &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "podmonitors.monitoring.coreos.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "monitoring.coreos.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "podmonitors",
				Singular: "podmonitor",
				Kind:     "PodMonitor",
				ListKind: "PodMonitorList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: ptr.To(true),
					},
				},
			}},
		},
	}))

	mgr2 := newManager()
	c2 := &Controller{Ctl: ctl}
	require.NoError(t, c2.SetupWithManager(t.Context(), mgr2, ""))
	assert.True(t, c2.podMonitorCRDInstalled(t.Context(), mgr2),
		"with the CRD installed, the pre-start probe must detect it (a cached List would ErrCacheNotStarted here)")
}

// TestResolveClusterSource_Success exercises the successful clusterRef path
// end-to-end against a real Redpanda CR: the chart render resolves internal
// brokers and TLS material (the chart's default listener is TLS with a
// Secret-backed self-signed CA), the result is cached, and a delete +
// recreate of the cluster under the same name does NOT serve the stale entry
// (UID-keyed cache).
func TestResolveClusterSource_Success(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-clusterref-success"},
	})
	require.NoError(t, err)

	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "basic", Namespace: ns.Name},
	}
	require.NoError(t, ctl.Apply(t.Context(), rp))

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "resolver", Namespace: ns.Name},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{Name: "basic"},
			},
		},
	}

	c := &Controller{Ctl: ctl, clusterConns: newClusterConnCache()}

	conn, err := c.resolveClusterSource(t.Context(), pipeline)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotEmpty(t, conn.Brokers, "expected internal broker addresses from the chart render")
	for _, b := range conn.Brokers {
		assert.Contains(t, b, "basic", "brokers should reference the cluster's services")
	}
	require.NotNil(t, conn.TLS, "the chart's default Kafka listener is TLS-enabled")
	require.NotNil(t, conn.TLS.CACertSecretRef, "default self-signed CA is Secret-backed")

	// Second resolution is a cache hit (same pointer).
	cached, err := c.resolveClusterSource(t.Context(), pipeline)
	require.NoError(t, err)
	assert.Same(t, conn, cached)

	// Delete + recreate the cluster under the same name. The recreated CR
	// restarts at generation 1 — exactly the shape that used to serve a
	// stale entry when the cache was keyed by generation alone.
	require.NoError(t, ctl.Delete(t.Context(), rp))
	rp2 := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "basic", Namespace: ns.Name},
	}
	require.NoError(t, ctl.Apply(t.Context(), rp2))

	fresh, err := c.resolveClusterSource(t.Context(), pipeline)
	require.NoError(t, err)
	assert.NotSame(t, conn, fresh, "a recreated cluster (new UID) must be re-resolved, not served from the stale cache entry")
}

// TestResolveClusterSource_RejectsCrossNamespaceAndForeignKinds covers the
// controller-side guard behind the CEL rules: clusterRef.namespace/group/kind
// were previously accepted by the schema and silently ignored, binding the
// pipeline to a same-named cluster in its own namespace instead.
func TestResolveClusterSource_RejectsCrossNamespaceAndForeignKinds(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-clusterref-guard"},
	})
	require.NoError(t, err)

	c := &Controller{Ctl: ctl, clusterConns: newClusterConnCache()}
	mk := func(ref *redpandav1alpha2.ClusterRef) *redpandav1alpha2.Pipeline {
		return &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "guard", Namespace: ns.Name},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML:    "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
				ClusterSource: &redpandav1alpha2.ClusterSource{ClusterRef: ref},
			},
		}
	}

	_, err = c.resolveClusterSource(t.Context(), mk(&redpandav1alpha2.ClusterRef{
		Name:      "prod",
		Namespace: ptr.To("prod-namespace"),
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clusterRef.namespace")

	_, err = c.resolveClusterSource(t.Context(), mk(&redpandav1alpha2.ClusterRef{
		Name: "prod",
		Kind: ptr.To("NodePool"),
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only cluster.redpanda.com/Redpanda references are supported")
}

// TestResolveUserRef_Validation covers userRef resolution incl. the password
// Secret: previously a missing Secret still marked UserRef=True and the pod
// later wedged in CreateContainerConfigError.
func TestResolveUserRef_Validation(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-userref"},
	})
	require.NoError(t, err)

	mechanism := redpandav1alpha2.SASLMechanism("scram-sha-256")
	user := &redpandav1alpha2.User{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-user", Namespace: ns.Name},
		Spec: redpandav1alpha2.UserSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{Name: "basic"},
			},
			Authentication: &redpandav1alpha2.UserAuthenticationSpec{
				Type: &mechanism,
				Password: redpandav1alpha2.Password{
					ValueFrom: &redpandav1alpha2.PasswordSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "svc-user-password"},
							Key:                  "password",
						},
					},
				},
			},
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), user))

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "userref", Namespace: ns.Name},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			UserRef:    &redpandav1alpha2.PipelineUserRef{Name: "svc-user"},
		},
	}

	// Password Secret doesn't exist yet: resolution must fail.
	_, err = resolveUserRef(t.Context(), ctl, pipeline)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password Secret")

	// Secret exists but lacks the referenced key: still a failure.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "svc-user-password", Namespace: ns.Name},
		Data:       map[string][]byte{"wrong-key": []byte("hunter2")},
	}
	require.NoError(t, ctl.Apply(t.Context(), secret))
	_, err = resolveUserRef(t.Context(), ctl, pipeline)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no key")

	// Correct key present: resolves with the User's identity.
	secret.Data["password"] = []byte("hunter2")
	require.NoError(t, ctl.Apply(t.Context(), secret))
	creds, err := resolveUserRef(t.Context(), ctl, pipeline)
	require.NoError(t, err)
	assert.Equal(t, "svc-user", creds.Username)
	assert.Equal(t, "SCRAM-SHA-256", creds.Mechanism)
	assert.Equal(t, "svc-user-password", creds.PasswordRef.Name)
}

// TestFindOwnershipConflict covers the SSA adoption guard: a pre-existing
// object with the Pipeline's name that the Pipeline does not own must refuse
// to sync (previously ForceOwnership hijacked it, and Pipeline deletion then
// deleted it).
func TestFindOwnershipConflict(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-name-conflict"},
	})
	require.NoError(t, err)

	// A ConfigMap owned by "another team", name-colliding with the Pipeline.
	foreign := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: ns.Name},
		Data:       map[string]string{"precious": "data"},
	}
	require.NoError(t, ctl.Apply(t.Context(), foreign))

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: ns.Name},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
		},
	}
	require.NoError(t, ctl.Apply(t.Context(), pipeline))
	require.NoError(t, ctl.Get(t.Context(), kube.AsKey(pipeline), pipeline))

	c := &Controller{Ctl: ctl}
	renderer := c.rendererFor(pipeline, nil, nil, nil, "")

	conflict, err := c.findOwnershipConflict(t.Context(), pipeline, renderer)
	require.NoError(t, err)
	require.NotEmpty(t, conflict, "a foreign same-named ConfigMap must be reported as a conflict")
	assert.Contains(t, conflict, "ConfigMap")
	assert.Contains(t, conflict, "not owned by this Pipeline")

	// Once the object is owned by the Pipeline (the normal steady state),
	// there is no conflict.
	foreign.OwnerReferences = []metav1.OwnerReference{
		*metav1.NewControllerRef(pipeline, redpandav1alpha2.SchemeGroupVersion.WithKind("Pipeline")),
	}
	require.NoError(t, ctl.Apply(t.Context(), foreign))
	conflict, err = c.findOwnershipConflict(t.Context(), pipeline, renderer)
	require.NoError(t, err)
	assert.Empty(t, conflict)
}

// TestValidateValueSources covers reconcile-time resolution of valueSources
// backing objects — previously a typo'd Secret name synced fine and only
// surfaced as a wedged pod.
func TestValidateValueSources(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-valuesources"},
	})
	require.NoError(t, err)

	mk := func(sources ...redpandav1alpha2.NamedValueSource) *redpandav1alpha2.Pipeline {
		return &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "vs", Namespace: ns.Name},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML:   "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
				ValueSources: sources,
			},
		}
	}

	secretSource := redpandav1alpha2.NamedValueSource{
		Name: "S3_SECRET_KEY",
		Source: redpandav1alpha2.ValueSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "s3-creds"},
				Key:                  "secret_access_key",
			},
		},
	}

	// Missing Secret: rejected.
	err = validateValueSources(t.Context(), ctl, mk(secretSource))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3_SECRET_KEY")

	// Secret present but key missing: rejected.
	require.NoError(t, ctl.Apply(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: ns.Name},
		Data:       map[string][]byte{"other": []byte("x")},
	}))
	err = validateValueSources(t.Context(), ctl, mk(secretSource))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no key")

	// Key present: accepted.
	require.NoError(t, ctl.Apply(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: ns.Name},
		Data:       map[string][]byte{"secret_access_key": []byte("x")},
	}))
	require.NoError(t, validateValueSources(t.Context(), ctl, mk(secretSource)))

	// Inline values need no backing object.
	require.NoError(t, validateValueSources(t.Context(), ctl, mk(redpandav1alpha2.NamedValueSource{
		Name:   "REGION",
		Source: redpandav1alpha2.ValueSource{Inline: ptr.To("us-east-2")},
	})))

	// Optional missing Secret is tolerated (kubelet env semantics).
	require.NoError(t, validateValueSources(t.Context(), ctl, mk(redpandav1alpha2.NamedValueSource{
		Name: "OPTIONAL_KEY",
		Source: redpandav1alpha2.ValueSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
				Key:                  "k",
				Optional:             ptr.To(true),
			},
		},
	})))
}

// TestCredentialsChecksum covers the rotation-roll digest: it must be stable
// across reconciles, change when a referenced Secret's content changes, and
// be empty when the pipeline references nothing.
func TestCredentialsChecksum(t *testing.T) {
	ctl := setupTestEnv(t)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-creds-checksum"},
	})
	require.NoError(t, err)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rotating", Namespace: ns.Name},
		Data:       map[string][]byte{"password": []byte("v1")},
	}
	require.NoError(t, ctl.Apply(t.Context(), secret))

	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "checksum", Namespace: ns.Name},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ValueSources: []redpandav1alpha2.NamedValueSource{{
				Name: "PASSWORD",
				Source: redpandav1alpha2.ValueSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "rotating"},
						Key:                  "password",
					},
				},
			}},
		},
	}

	c := &Controller{Ctl: ctl}

	first, err := c.credentialsChecksum(t.Context(), pipeline, nil, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	// Stable while nothing changes.
	again, err := c.credentialsChecksum(t.Context(), pipeline, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, first, again, "checksum must be deterministic across reconciles")

	// Rotating the Secret changes the digest — this is what rolls the pods.
	secret.Data["password"] = []byte("v2")
	require.NoError(t, ctl.Apply(t.Context(), secret))
	rotated, err := c.credentialsChecksum(t.Context(), pipeline, nil, nil, nil)
	require.NoError(t, err)
	assert.NotEqual(t, first, rotated, "a Secret rotation must change the checksum")

	// No references, no license: nothing to digest.
	bare := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "bare", Namespace: ns.Name},
		Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
	}
	empty, err := c.credentialsChecksum(t.Context(), bare, nil, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
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

func TestRender_LicenseSecretAndEnvVar(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "license-test",
			Namespace: "redpanda",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}

	licenseBytes := []byte("eyJvcmciOiJ0ZXN0In0=.signature")
	r := &render{pipeline: pipeline, labels: Labels(pipeline), licenseContent: licenseBytes}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	var sec *corev1.Secret
	var dp *appsv1.Deployment
	for _, o := range objs {
		switch v := o.(type) {
		case *corev1.Secret:
			sec = v
		case *appsv1.Deployment:
			dp = v
		}
	}

	require.NotNil(t, sec, "expected a license Secret to be rendered")
	assert.Equal(t, "license-test-license", sec.Name)
	assert.Equal(t, "redpanda", sec.Namespace)
	assert.Equal(t, corev1.SecretTypeOpaque, sec.Type)
	assert.Equal(t, licenseBytes, sec.Data["license"])

	require.NotNil(t, dp, "expected a Deployment to be rendered")
	main := dp.Spec.Template.Spec.Containers[0]
	var found *corev1.EnvVar
	for i := range main.Env {
		if main.Env[i].Name == "REDPANDA_LICENSE" {
			found = &main.Env[i]
			break
		}
	}
	require.NotNil(t, found, "expected REDPANDA_LICENSE env var on connect container")
	require.NotNil(t, found.ValueFrom)
	require.NotNil(t, found.ValueFrom.SecretKeyRef)
	assert.Equal(t, "license-test-license", found.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "license", found.ValueFrom.SecretKeyRef.Key)

	// The lint init container should also see the env var since it shares the slice.
	require.Len(t, dp.Spec.Template.Spec.InitContainers, 1)
	lint := dp.Spec.Template.Spec.InitContainers[0]
	hasLicense := false
	for _, e := range lint.Env {
		if e.Name == "REDPANDA_LICENSE" {
			hasLicense = true
			break
		}
	}
	assert.True(t, hasLicense, "lint init container should also receive REDPANDA_LICENSE so the license loads during lint")
}

func TestRender_NoLicenseContent_OmitsSecretAndEnvVar(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-license-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  generate:\n    mapping: 'root = \"hello\"'\noutput:\n  stdout: {}\n",
		},
	}
	r := &render{pipeline: pipeline, labels: Labels(pipeline)}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	for _, o := range objs {
		_, isSecret := o.(*corev1.Secret)
		assert.False(t, isSecret, "no Secret should be rendered when licenseContent is empty")
	}

	for _, o := range objs {
		dp, ok := o.(*appsv1.Deployment)
		if !ok {
			continue
		}
		for _, e := range dp.Spec.Template.Spec.Containers[0].Env {
			assert.NotEqual(t, "REDPANDA_LICENSE", e.Name, "no REDPANDA_LICENSE env var should be set when no license")
		}
	}
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

func TestRender_Deployment_ImagePrecedence(t *testing.T) {
	// Exercises the three-tier image precedence:
	//   1. Pipeline.spec.image (per-pipeline override) wins.
	//   2. render.defaultImage (chart-level default via the operator's
	//      --connect-default-image flag) wins when .spec.image is empty.
	//   3. PipelineDefaultImage (binary-baked constant) wins when both
	//      are empty.
	t.Run("spec_image_wins_over_chart_default", func(t *testing.T) {
		pl := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "pl", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
				Image:      ptr.To("docker.example.com/connect:5.0.0"),
			},
		}
		r := &render{pipeline: pl, labels: Labels(pl), defaultImage: "docker.example.com/connect:4.92.0"}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "docker.example.com/connect:5.0.0", objs[1].(*appsv1.Deployment).Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("chart_default_wins_when_spec_image_empty", func(t *testing.T) {
		pl := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "pl", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			},
		}
		r := &render{pipeline: pl, labels: Labels(pl), defaultImage: "docker.example.com/connect:4.92.0"}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "docker.example.com/connect:4.92.0", objs[1].(*appsv1.Deployment).Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("binary_default_when_both_empty", func(t *testing.T) {
		pl := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "pl", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			},
		}
		r := &render{pipeline: pl, labels: Labels(pl)}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		assert.Equal(t, redpandav1alpha2.PipelineDefaultImage, objs[1].(*appsv1.Deployment).Spec.Template.Spec.Containers[0].Image)
	})
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

func TestRender_Deployment_ValueSources(t *testing.T) {
	pipeline := &redpandav1alpha2.Pipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "values-test",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.PipelineSpec{
			ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
			ValueSources: []redpandav1alpha2.NamedValueSource{
				{
					Name: "S3_SECRET_KEY",
					Source: redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "s3-creds"},
							Key:                  "secret_access_key",
						},
					},
				},
				{
					Name: "DB_HOST",
					Source: redpandav1alpha2.ValueSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "warehouse-env"},
							Key:                  "host",
						},
					},
				},
				{
					Name: "BUCKET",
					Source: redpandav1alpha2.ValueSource{
						Inline: ptr.To("orders-warehouse"),
					},
				},
			},
		},
	}

	labels := Labels(pipeline)
	r := &render{pipeline: pipeline, labels: labels}

	objs, err := r.Render(t.Context())
	require.NoError(t, err)

	dp := objs[1].(*appsv1.Deployment)
	// EnvFrom should be empty — the bag-of-Secrets pattern is gone.
	assert.Empty(t, dp.Spec.Template.Spec.Containers[0].EnvFrom)
	assert.Empty(t, dp.Spec.Template.Spec.InitContainers[0].EnvFrom)

	// Each ValueSource entry should appear as its own typed EnvVar.
	envByName := map[string]corev1.EnvVar{}
	for _, e := range dp.Spec.Template.Spec.Containers[0].Env {
		envByName[e.Name] = e
	}

	require.Contains(t, envByName, "S3_SECRET_KEY")
	require.NotNil(t, envByName["S3_SECRET_KEY"].ValueFrom)
	require.NotNil(t, envByName["S3_SECRET_KEY"].ValueFrom.SecretKeyRef)
	assert.Equal(t, "s3-creds", envByName["S3_SECRET_KEY"].ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "secret_access_key", envByName["S3_SECRET_KEY"].ValueFrom.SecretKeyRef.Key)

	require.Contains(t, envByName, "DB_HOST")
	require.NotNil(t, envByName["DB_HOST"].ValueFrom)
	require.NotNil(t, envByName["DB_HOST"].ValueFrom.ConfigMapKeyRef)
	assert.Equal(t, "warehouse-env", envByName["DB_HOST"].ValueFrom.ConfigMapKeyRef.Name)

	require.Contains(t, envByName, "BUCKET")
	assert.Equal(t, "orders-warehouse", envByName["BUCKET"].Value)
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

func TestRender_Deployment_ServiceAccountName(t *testing.T) {
	t.Run("propagates_to_pod_spec", func(t *testing.T) {
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "sa-test", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML:         "input:\n  stdin: {}\noutput:\n  stdout: {}\n",
				ServiceAccountName: "mysql-cdc-pipeline-sa",
			},
		}
		r := &render{pipeline: pipeline, labels: Labels(pipeline)}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		dp := objs[1].(*appsv1.Deployment)
		assert.Equal(t, "mysql-cdc-pipeline-sa", dp.Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("empty_when_unset", func(t *testing.T) {
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "sa-default", Namespace: "default"},
			Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: "input:\n  stdin: {}\noutput:\n  stdout: {}\n"},
		}
		r := &render{pipeline: pipeline, labels: Labels(pipeline)}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		dp := objs[1].(*appsv1.Deployment)
		assert.Empty(t, dp.Spec.Template.Spec.ServiceAccountName,
			"unset means the namespace's default SA is used at admission time")
	})
}

// TestRender_InlineMergesRedpandaPlugins covers the v2 cluster-binding
// render path: when a Pipeline is bound to a Redpanda cluster (via clusterRef
// or staticConfiguration), the operator merges seed_brokers, tls, and sasl
// into any output.redpanda and input.redpanda blocks in the user's configYaml.
// The resolved connection is additionally rendered as the top-level
// `redpanda:` shared-client block, which `redpanda_common` and other
// shared-client plugins bind to, alongside the supported
// `redpanda` plugin.
func TestRender_InlineMergesRedpandaPlugins(t *testing.T) {
	clusterConn := &clusterConnection{
		Brokers: []string{"broker-0.rp.svc:9093", "broker-1.rp.svc:9093"},
	}
	creds := &userCredentials{
		Mechanism: "SCRAM-SHA-512",
		Username:  "mysql-cdc-orders-svc",
	}

	t.Run("merges_into_output_redpanda", func(t *testing.T) {
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  stdin: {}\noutput:\n  redpanda:\n    topic: orders\n",
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
			},
		}
		r := &render{
			pipeline:        pipeline,
			labels:          Labels(pipeline),
			clusterConn:     clusterConn,
			userCredentials: creds,
		}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)

		var rendered map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(cm.Data["connect.yaml"]), &rendered))
		out, ok := rendered["output"].(map[string]any)
		require.True(t, ok)
		rp, ok := out["redpanda"].(map[string]any)
		require.True(t, ok, "output.redpanda must remain a map after merge")

		// User-side field preserved.
		assert.Equal(t, "orders", rp["topic"])
		// Operator-injected fields present.
		assert.Equal(t,
			[]any{"broker-0.rp.svc:9093", "broker-1.rp.svc:9093"},
			rp["seed_brokers"])
		sasl, ok := rp["sasl"].([]any)
		require.True(t, ok)
		require.Len(t, sasl, 1)
		assert.Equal(t, "SCRAM-SHA-512", sasl[0].(map[string]any)["mechanism"])

		// The resolved connection is also exposed as the top-level
		// `redpanda:` shared client so `redpanda_common` (and any
		// shared-client plugin) works without inline credentials.
		topLevel, hasTopLevel := rendered["redpanda"].(map[string]any)
		require.True(t, hasTopLevel, "top-level redpanda shared client rendered")
		assert.NotNil(t, topLevel["seed_brokers"])
	})

	t.Run("merges_into_input_redpanda", func(t *testing.T) {
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  redpanda:\n    topics: [orders]\n    consumer_group: cg\noutput:\n  stdout: {}\n",
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
			},
		}
		r := &render{
			pipeline:        pipeline,
			labels:          Labels(pipeline),
			clusterConn:     clusterConn,
			userCredentials: creds,
		}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)

		var rendered map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(cm.Data["connect.yaml"]), &rendered))
		in := rendered["input"].(map[string]any)
		rp := in["redpanda"].(map[string]any)
		assert.Equal(t, "cg", rp["consumer_group"])
		assert.NotNil(t, rp["seed_brokers"])
		assert.NotNil(t, rp["sasl"])
	})

	t.Run("user_keys_win_on_conflict", func(t *testing.T) {
		// User points the redpanda output at a different cluster — the
		// operator must not clobber that override.
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "" +
					"input:\n  stdin: {}\n" +
					"output:\n" +
					"  redpanda:\n" +
					"    topic: orders\n" +
					"    seed_brokers: [external.example.com:9093]\n",
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
			},
		}
		r := &render{
			pipeline:        pipeline,
			labels:          Labels(pipeline),
			clusterConn:     clusterConn,
			userCredentials: creds,
		}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)

		var rendered map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(cm.Data["connect.yaml"]), &rendered))
		rp := rendered["output"].(map[string]any)["redpanda"].(map[string]any)
		assert.Equal(t,
			[]any{"external.example.com:9093"},
			rp["seed_brokers"],
			"user-supplied seed_brokers wins")
		// sasl wasn't user-supplied, so it should be filled in.
		assert.NotNil(t, rp["sasl"])
	})

	t.Run("no_redpanda_plugin_no_merge", func(t *testing.T) {
		// Pipeline writes to S3 only — no output.redpanda block to merge
		// into. The configYaml should pass through unchanged.
		original := "input:\n  stdin: {}\noutput:\n  aws_s3:\n    bucket: my-bucket\n"
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: original,
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
			},
		}
		r := &render{
			pipeline:        pipeline,
			labels:          Labels(pipeline),
			clusterConn:     clusterConn,
			userCredentials: creds,
		}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)

		// The rendered config still parses to the same structure. The
		// top-level `redpanda:` shared client is always rendered from the
		// resolved connection (redpanda_common support).
		var rendered map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(cm.Data["connect.yaml"]), &rendered))
		_, hasTopLevel := rendered["redpanda"]
		assert.True(t, hasTopLevel)
		// And output.aws_s3 is untouched.
		out := rendered["output"].(map[string]any)
		_, hasRedpanda := out["redpanda"]
		assert.False(t, hasRedpanda, "operator must not synthesize an output.redpanda block")
	})

	t.Run("redpanda_common_binds_to_the_shared_client", func(t *testing.T) {
		// The deprecated redpanda_common plugin used to consume a
		// top-level `redpanda:` block. The v2 design intentionally
		// drops that injection — users staying on redpanda_common need
		// to hand-write its config. This test guards against an
		// accidental regression that re-introduces the top-level block.
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML: "input:\n  stdin: {}\noutput:\n  redpanda_common:\n    topic: orders\n",
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
			},
		}
		r := &render{
			pipeline:        pipeline,
			labels:          Labels(pipeline),
			clusterConn:     clusterConn,
			userCredentials: creds,
		}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)

		var rendered map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(cm.Data["connect.yaml"]), &rendered))
		topLevel, hasTopLevel := rendered["redpanda"].(map[string]any)
		require.True(t, hasTopLevel, "top-level redpanda shared client rendered for redpanda_common")
		assert.NotNil(t, topLevel["seed_brokers"])
	})

	t.Run("inline_only_pipeline_passes_through", func(t *testing.T) {
		// No cluster binding at all — fully inline configYaml. Render
		// must not modify it.
		original := "input:\n  stdin: {}\noutput:\n  stdout: {}\n"
		pipeline := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec:       redpandav1alpha2.PipelineSpec{ConfigYAML: original},
		}
		r := &render{pipeline: pipeline, labels: Labels(pipeline)}
		objs, err := r.Render(t.Context())
		require.NoError(t, err)
		cm := objs[0].(*corev1.ConfigMap)
		assert.Equal(t, original, cm.Data["connect.yaml"])
	})
}
