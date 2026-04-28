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
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

var (
	redpandasTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "redpandas_total",
			Help: "Number of Redpanda clusters (cluster.redpanda.com/v1alpha2) managed by the operator",
		},
	)
	redpandaDesiredNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_desired_nodes",
			Help: "Desired number of broker pods per Redpanda cluster, summed across all node pools",
		}, []string{"namespace", "name"},
	)
	redpandaReadyNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_ready_nodes",
			Help: "Number of broker pods reporting Ready per Redpanda cluster, summed across all node pools",
		}, []string{"namespace", "name"},
	)
	redpandaMisconfiguredClusters = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_misconfigured_clusters",
			Help: "Number of Redpanda clusters whose ConfigurationApplied condition is not True, labeled by reason",
		}, []string{"reason"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		redpandasTotal,
		redpandaDesiredNodes,
		redpandaReadyNodes,
		redpandaMisconfiguredClusters,
	)
}

// redpandaMetricKey identifies a Redpanda CR for the purpose of tracking
// previously-observed Prometheus label values across reconciles. Cluster
// names are not globally unique (the same name can exist in multiple
// namespaces), so the namespace is part of the key.
type redpandaMetricKey struct {
	namespace string
	name      string
}

// RedpandaMetricsReconciler emits Prometheus metrics for the v2 Redpanda CRD,
// providing the parallel of the v1 ClusterMetricController for the
// cluster.redpanda.com/v1alpha2 Redpanda type. The reconciler ignores the
// incoming request and recomputes the gauges from a fresh List so that
// metrics stay accurate even when individual events are coalesced.
type RedpandaMetricsReconciler struct {
	Manager multicluster.Manager

	// labels previously emitted on the desired/ready GaugeVecs. We retain them
	// across reconciles so that we can call DeleteLabelValues for Redpandas
	// that have since been deleted — without this, prometheus would keep
	// reporting their last value forever.
	currentLabels collections.Set[redpandaMetricKey]

	// reasons previously emitted on the misconfigured GaugeVec. Same idea as
	// currentLabels.
	currentConfigurationLabels collections.Set[string]
}

// NewRedpandaMetricsReconciler creates a RedpandaMetricsReconciler.
func NewRedpandaMetricsReconciler(mgr multicluster.Manager) *RedpandaMetricsReconciler {
	return &RedpandaMetricsReconciler{
		Manager:                    mgr,
		currentLabels:              collections.NewConcurrentSet[redpandaMetricKey](),
		currentConfigurationLabels: collections.NewConcurrentSet[string](),
	}
}

// Reconcile lists all Redpandas across every cluster known to the multicluster
// Manager and updates the registered Prometheus gauges. The incoming request
// is ignored: we always recompute totals from scratch.
func (r *RedpandaMetricsReconciler) Reconcile(ctx context.Context, _ mcreconcile.Request) (ctrl.Result, error) {
	seenLabels := collections.NewSet[redpandaMetricKey]()
	misconfiguredCounts := map[string]int{}

	var total int

	for _, clusterName := range r.Manager.GetClusterNames() {
		cl, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting cluster %q: %w", clusterName, err)
		}

		var rps redpandav1alpha2.RedpandaList
		if err := cl.GetClient().List(ctx, &rps); err != nil {
			return ctrl.Result{}, fmt.Errorf("listing Redpandas in cluster %q: %w", clusterName, err)
		}

		total += len(rps.Items)
		for i := range rps.Items {
			rp := &rps.Items[i]
			key := redpandaMetricKey{namespace: rp.Namespace, name: rp.Name}

			var desired, ready int32
			for _, pool := range rp.Status.NodePools {
				desired += pool.DesiredReplicas
				ready += pool.ReadyReplicas
			}

			d, err := redpandaDesiredNodes.GetMetricWithLabelValues(key.namespace, key.name)
			if err != nil {
				return ctrl.Result{}, err
			}
			d.Set(float64(desired))

			a, err := redpandaReadyNodes.GetMetricWithLabelValues(key.namespace, key.name)
			if err != nil {
				return ctrl.Result{}, err
			}
			a.Set(float64(ready))

			seenLabels.Add(key)
			r.currentLabels.Add(key)

			if cond := apimeta.FindStatusCondition(rp.Status.Conditions, statuses.ClusterConfigurationApplied); cond != nil && cond.Status != metav1.ConditionTrue {
				misconfiguredCounts[cond.Reason]++
			}
		}
	}

	redpandasTotal.Set(float64(total))

	for _, key := range r.currentLabels.Values() {
		if !seenLabels.HasAny(key) {
			redpandaDesiredNodes.DeleteLabelValues(key.namespace, key.name)
			redpandaReadyNodes.DeleteLabelValues(key.namespace, key.name)
		}
	}

	for reason, count := range misconfiguredCounts {
		g, err := redpandaMisconfiguredClusters.GetMetricWithLabelValues(reason)
		if err != nil {
			return ctrl.Result{}, err
		}
		g.Set(float64(count))
		r.currentConfigurationLabels.Add(reason)
	}
	for _, reason := range r.currentConfigurationLabels.Values() {
		if _, exists := misconfiguredCounts[reason]; !exists {
			redpandaMisconfiguredClusters.DeleteLabelValues(reason)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the metrics reconciler with the multicluster
// manager. It engages with both local and provider clusters so that
// metrics reflect Redpandas across every cluster the operator manages.
func (r *RedpandaMetricsReconciler) SetupWithManager(mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).
		WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
			// SkipNameValidation matches the convention used by other v2
			// controllers in this package — the multicluster runtime
			// occasionally reuses controller names across registrations
			// during tests, and this avoids a duplicate-name panic.
			SkipNameValidation: ptr.To(true),
		}).
		For(
			&redpandav1alpha2.Redpanda{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Complete(r)
}
