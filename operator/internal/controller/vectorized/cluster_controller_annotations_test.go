// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vectorized

import (
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// annotationKey is the pod annotation the propagation subtests add to
// Cluster.Spec.Annotations and then look for downstream.
const annotationKey = "test.redpanda.com/propagation"

// TestClusterSpecAnnotationsReachStatefulSet covers, deterministically and
// without a cluster, the half of the sts-annotation-propagation kuttl test
// that lives in the operator: Cluster.Spec.Annotations must reach the
// StatefulSet's pod template.
//
// envtest has no kubelet, so the kuttl test is still what proves the
// annotation lands on live *pods* — that needs the health-gated, one-at-a-time
// roll, since these StatefulSets run OnDelete and a template update alone
// never touches a running pod. What's covered here is everything up to that
// point, which is where a template-propagation regression would actually show:
// when the kuttl test last failed, the pod template itself had never been
// updated.
func TestClusterSpecAnnotationsReachStatefulSet(t *testing.T) {
	testScheme := controller.UnifiedScheme
	ctx := log.IntoContext(t.Context(), testr.New(t))

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{Scheme: testScheme},
	})

	require.NoError(t, kube.ApplyAllAndWait(ctx, ctl, func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
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

	mgr, err := ctrl.NewManager(ctl.RestConfig(), manager.Options{
		Logger:  testr.New(t),
		Scheme:  testScheme,
		Metrics: server.Options{BindAddress: "0"},
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
	})
	require.NoError(t, err)

	reconciler, adminAPIs := createTestReconcilerWithAdminAPIs(t, mgr)
	require.NoError(t, reconciler.SetupWithManager(mgr))

	go func() {
		require.NoError(t, mgr.Start(ctx))
	}()

	// createConvergedCluster applies a Cluster and waits for its StatefulSet to
	// exist. Waiting matters: annotating before the StatefulSet is created
	// would be picked up by the create path, which proves nothing about
	// updates.
	//
	// externalKafkaPort must be unique per cluster. minimalClusterDef hardcodes
	// the external Kafka listener's node port, and its NodePort Service failing
	// to allocate blocks the StatefulSet entirely: Ensure resolves the node port
	// Service before it renders anything.
	createConvergedCluster := func(t *testing.T, name string, externalKafkaPort int) (*vectorizedv1alpha1.Cluster, *appsv1.StatefulSet) {
		t.Helper()

		cluster := minimalClusterDef()
		cluster.Name = name
		for i := range cluster.Spec.Configuration.KafkaAPI {
			if cluster.Spec.Configuration.KafkaAPI[i].External.Enabled {
				cluster.Spec.Configuration.KafkaAPI[i].Port = externalKafkaPort
			}
		}
		require.NoError(t, ctl.Apply(ctx, cluster))

		sts := &appsv1.StatefulSet{ObjectMeta: cluster.ObjectMeta}
		sts.Name = cluster.Name
		require.NoError(t, ctl.WaitFor(ctx, sts, func(_ kube.Object, err error) (bool, error) {
			if err != nil {
				return false, client.IgnoreNotFound(err)
			}
			return true, nil
		}), "the StatefulSet was never created")
		require.NotContains(t, sts.Spec.Template.Annotations, annotationKey)

		return cluster, sts
	}

	annotate := func(t *testing.T, cluster *vectorizedv1alpha1.Cluster) {
		t.Helper()

		live := &vectorizedv1alpha1.Cluster{}
		require.NoError(t, ctl.Get(ctx, kube.AsKey(cluster), live))
		live.Spec.Annotations = map[string]string{annotationKey: "works"}
		require.NoError(t, ctl.Update(ctx, live))
	}

	awaitPropagation := func(t *testing.T, sts *appsv1.StatefulSet) error {
		t.Helper()

		return ctl.WaitFor(ctx, sts, func(obj kube.Object, err error) (bool, error) {
			if err != nil {
				return false, client.IgnoreNotFound(err)
			}
			return obj.(*appsv1.StatefulSet).Spec.Template.Annotations[annotationKey] == "works", nil
		})
	}

	t.Run("annotating a live Cluster updates the pod template", func(t *testing.T) {
		cluster, sts := createConvergedCluster(t, "annotation-propagation", 30093)
		annotate(t, cluster)

		require.NoError(t, awaitPropagation(t, sts),
			"the annotation never reached the StatefulSet pod template")
		// The rendered configmap hash must survive alongside it — the render
		// merges spec.annotations with its own bookkeeping rather than
		// replacing either.
		require.Contains(t, sts.Spec.Template.Annotations, resources.ConfigMapHashAnnotationKey)
	})

	t.Run("an unreachable cluster doesn't hold up the pod template", func(t *testing.T) {
		cluster, sts := createConvergedCluster(t, "annotation-unreachable", 30094)

		// Writing the StatefulSet spec is deliberately upstream of every
		// health gate: updateStatefulSet runs before isClusterHealthy, and the
		// admin API is only consulted after the resources are ensured. Only
		// the subsequent pod roll waits on the cluster. If that ordering ever
		// inverts, a spec change would become undeliverable exactly when the
		// cluster most needs one.
		api := adminAPIs.Get(cluster.Name)
		require.NotNil(t, api, "the reconciler never built an admin API client for this cluster")
		api.SetUnavailable(true)
		api.SetClusterHealth(false)

		annotate(t, cluster)

		require.NoError(t, awaitPropagation(t, sts),
			"an unreachable cluster blocked a change that needs no cluster interaction")
	})
}
