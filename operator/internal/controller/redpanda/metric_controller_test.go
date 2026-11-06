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
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

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
	r := NewRedpandaMetricsReconciler(c, ctl, false)

	reconcile := func(t *testing.T) {
		t.Helper()
		res, err := r.Reconcile(t.Context(), ctrl.Request{})
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)
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
	brokers := []*redpandav1alpha2.Broker{
		metricsBroker("metrics-rp", "metrics-rp-0", redpandav1alpha2.ClusterRef{Name: "metrics-rp"}),
		// A NodePool-referenced broker rolls up to its Redpanda.
		metricsBroker("metrics-rp", "metrics-rp-pool-0", redpandav1alpha2.ClusterRef{Kind: ptr.To(redpandav1alpha2.NodePoolKind), Name: "pool"}),
		// A same-named V1 Cluster's broker belongs to the other kind.
		metricsBroker("metrics-rp", "metrics-rp-1", redpandav1alpha2.ClusterRef{Group: ptr.To(vectorizedv1alpha1.GroupVersion.Group), Kind: ptr.To("Cluster"), Name: "metrics-rp"}),
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

// A Broker phase change alone, with no Redpanda event, has to move the gauge:
// that is what the Broker watch is for. Brokers are read through the
// manager's cache, as in production.
func TestRedpandaMetricsReconcilerWatchesBrokers(t *testing.T) {
	ctl := kubetest.NewEnv(t, kube.Options{Options: client.Options{Scheme: controller.UnifiedScheme}})
	testutils.InstallCRDs(t, ctl, crds.Redpanda(), crds.Broker())
	t.Cleanup(observability.BrokerPhase.Reset)

	mgr, err := ctrl.NewManager(ctl.RestConfig(), ctrl.Options{
		Scheme:     controller.UnifiedScheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	require.NoError(t, err)
	cached, err := kube.FromRESTConfig(ctl.RestConfig(), kube.Options{Options: client.Options{
		Scheme: controller.UnifiedScheme,
		Cache:  &client.CacheOptions{Reader: mgr.GetCache()},
	}})
	require.NoError(t, err)
	require.NoError(t, NewRedpandaMetricsReconciler(mgr.GetClient(), cached, true).SetupWithManager(mgr))
	testutils.StartManager(t, mgr)

	c, err := client.New(ctl.RestConfig(), client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)
	require.NoError(t, c.Create(t.Context(), &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "watch-rp"},
		Spec:       redpandav1alpha2.MinimalRedpandaSpec(),
	}))
	broker := metricsBroker("watch-rp", "watch-rp-0", redpandav1alpha2.ClusterRef{Name: "watch-rp"})
	require.NoError(t, c.Create(t.Context(), broker))

	phaseIs := func(phase string, want float64) func() bool {
		return func() bool {
			got, ok, err := testutils.GaugeValue("operator_broker_phase", map[string]string{"namespace": "default", "kind": observability.BrokerKindRedpanda, "name": "watch-rp", "phase": phase})
			return err == nil && ok && got == want
		}
	}
	require.Eventually(t, phaseIs(observability.BrokerPhaseUnknown, 1), 10*time.Second, 50*time.Millisecond)

	broker.Status.Phase = redpandav1alpha2.BrokerPhaseStuck
	require.NoError(t, c.Status().Update(t.Context(), broker))
	require.Eventually(t, phaseIs(string(redpandav1alpha2.BrokerPhaseStuck), 1), 10*time.Second, 50*time.Millisecond)
	require.True(t, phaseIs(observability.BrokerPhaseUnknown, 0)())
}

// metricsBroker is the smallest Broker the CRD admits, attributed to cluster.
func metricsBroker(cluster, name string, ref redpandav1alpha2.ClusterRef) *redpandav1alpha2.Broker {
	return &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, Labels: map[string]string{redpandav1alpha2.ClusterNameLabel: cluster}},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   ref,
			NetworkIndex: ptr.To(int32(0)),
			PodTemplate: redpandav1alpha2.BrokerPodTemplate{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "busybox"}}},
			},
		},
	}
}
