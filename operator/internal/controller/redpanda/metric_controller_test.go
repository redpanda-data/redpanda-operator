// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestRedpandaMetricsReconciler(t *testing.T) {
	ctl := kubetest.NewEnv(t, kube.Options{Options: client.Options{Scheme: controller.UnifiedScheme}})
	testutils.InstallCRDs(t, ctl, crds.Redpanda())

	c, err := client.New(ctl.RestConfig(), client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)
	t.Cleanup(observability.BrokerPhase.Reset)
	r := NewRedpandaMetricsReconciler(c, ctl)

	reconcile := func(t *testing.T) {
		t.Helper()
		res, err := r.Reconcile(t.Context(), ctrl.Request{})
		require.NoError(t, err)
		require.Equal(t, observability.ResourceStateRequeue, res.RequeueAfter)
	}

	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "metrics-rp"},
		Spec:       redpandav1alpha2.MinimalRedpandaSpec(),
	}
	require.NoError(t, c.Create(t.Context(), rp))

	// The Broker CRD is not installed yet: the Redpanda gauges must still be
	// exported, with nothing counted for brokers.
	reconcile(t)
	require.Equal(t, 1.0, promtestutil.ToFloat64(observability.Redpandas))
	testutils.RequireGauge(t, "redpanda_desired_nodes", map[string]string{"namespace": "default", "name": "metrics-rp"}, 0)
	testutils.RequireNoGauge(t, "operator_broker_phase", map[string]string{"name": "metrics-rp"})

	testutils.InstallCRDs(t, ctl, crds.Broker())
	broker := func(name string, ref redpandav1alpha2.ClusterRef) *redpandav1alpha2.Broker {
		return &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, Labels: map[string]string{redpandav1alpha2.ClusterNameLabel: "metrics-rp"}},
			// The smallest spec the CRD admits.
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef:   ref,
				NetworkIndex: ptr.To(int32(0)),
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "busybox"}}},
				},
			},
		}
	}
	brokers := []*redpandav1alpha2.Broker{
		broker("metrics-rp-0", redpandav1alpha2.ClusterRef{Name: "metrics-rp"}),
		// A NodePool-referenced broker rolls up to its Redpanda.
		broker("metrics-rp-pool-0", redpandav1alpha2.ClusterRef{Kind: ptr.To(redpandav1alpha2.NodePoolKind), Name: "pool"}),
		// A same-named V1 Cluster's broker belongs to the other kind.
		broker("metrics-rp-1", redpandav1alpha2.ClusterRef{Group: ptr.To(vectorizedv1alpha1.GroupVersion.Group), Kind: ptr.To("Cluster"), Name: "metrics-rp"}),
	}
	for i, phase := range []redpandav1alpha2.BrokerPhase{redpandav1alpha2.BrokerPhaseRunning, redpandav1alpha2.BrokerPhaseDiskLost, redpandav1alpha2.BrokerPhaseRunning} {
		require.NoError(t, c.Create(t.Context(), brokers[i]))
		brokers[i].Status.Phase = phase
		require.NoError(t, c.Status().Update(t.Context(), brokers[i]))
	}
	series := func(phase redpandav1alpha2.BrokerPhase) map[string]string {
		return map[string]string{"namespace": "default", "kind": observability.BrokerKindRedpanda, "name": "metrics-rp", "phase": string(phase)}
	}
	reconcile(t)
	testutils.RequireGauge(t, "operator_broker_phase", series(redpandav1alpha2.BrokerPhaseRunning), 1)
	testutils.RequireGauge(t, "operator_broker_phase", series(redpandav1alpha2.BrokerPhaseDiskLost), 1)
	testutils.RequireGauge(t, "operator_broker_phase", series(redpandav1alpha2.BrokerPhaseStuck), 0)
	testutils.RequireNoGauge(t, "operator_broker_phase", map[string]string{"kind": observability.BrokerKindCluster, "name": "metrics-rp"})

	for _, b := range brokers {
		require.NoError(t, c.Delete(t.Context(), b))
	}
	reconcile(t)
	testutils.RequireNoGauge(t, "operator_broker_phase", map[string]string{"name": "metrics-rp"})
}
