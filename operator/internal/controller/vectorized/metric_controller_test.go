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

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestClusterMetricController(t *testing.T) {
	ctl := kubetest.NewEnv(t, kube.Options{Options: client.Options{Scheme: controller.UnifiedScheme}})
	testutils.InstallCRDs(t, ctl, crds.VectorizedCluster())

	c, err := client.New(ctl.RestConfig(), client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)
	r := NewClusterMetricsController(c)

	reconcile := func(t *testing.T) {
		t.Helper()
		_, err := r.Reconcile(t.Context(), ctrl.Request{})
		require.NoError(t, err)
	}

	one, two := minimalClusterDef(), minimalClusterDef()
	one.Name = "metrics-one"
	two.Name = "metrics-two"
	two.Spec.Replicas = ptr.To[int32](3)
	require.NoError(t, c.Create(t.Context(), one))
	require.NoError(t, c.Create(t.Context(), two))
	two.Status.SetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType, corev1.ConditionFalse, "TestError", "")
	require.NoError(t, c.Status().Update(t.Context(), two))

	reconcile(t)
	require.Equal(t, 2.0, promtestutil.ToFloat64(redpandaClusters))
	requireGauge(t, "desired_redpanda_nodes_total", map[string]string{"cluster": "metrics-one"}, 1)
	requireGauge(t, "desired_redpanda_nodes_total", map[string]string{"cluster": "metrics-two"}, 3)
	requireGauge(t, "redpanda_misconfigured_clusters_total", map[string]string{"reason": "TestError"}, 1)

	// A removed cluster must drop out of the retained label sets too, or they
	// grow with every cluster the operator has ever seen.
	require.NoError(t, c.Delete(t.Context(), two))
	reconcile(t)
	require.Equal(t, 1.0, promtestutil.ToFloat64(redpandaClusters))
	requireGauge(t, "desired_redpanda_nodes_total", map[string]string{"cluster": "metrics-one"}, 1)
	requireNoGauge(t, "desired_redpanda_nodes_total", map[string]string{"cluster": "metrics-two"})
	requireNoGauge(t, "redpanda_misconfigured_clusters_total", map[string]string{"reason": "TestError"})
	require.False(t, r.currentLabels.HasAny("metrics-two"))
	require.False(t, r.currentConfigurationLabels.HasAny("TestError"))
}

func requireGauge(t *testing.T, name string, labels map[string]string, want float64) {
	t.Helper()
	got, found := testutils.GaugeSeries(t, name, labels)
	require.True(t, found, "%s%v not exported", name, labels)
	require.Equal(t, want, got, "%s%v", name, labels)
}

func requireNoGauge(t *testing.T, name string, labels map[string]string) {
	t.Helper()
	_, found := testutils.GaugeSeries(t, name, labels)
	require.False(t, found, "%s%v still exported", name, labels)
}
