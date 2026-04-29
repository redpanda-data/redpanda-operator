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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redpanda-data/common-go/otelutil/log"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
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
// previously-emitted Prometheus label values across reconciles. The namespace
// is part of the key because v2 Redpanda names can collide across namespaces.
type redpandaMetricKey struct {
	namespace string
	name      string
}

// RedpandaMetricsReconciler emits Prometheus metrics for the v2
// `cluster.redpanda.com/v1alpha2` Redpanda CRD, providing the parallel of the
// v1 ClusterMetricController. The reconciler ignores the incoming request and
// recomputes the gauges from a fresh List so that values stay accurate even
// when individual events are coalesced — matching the v1 pattern.
//
// Scope: only the Redpanda CRD, intentionally not StretchCluster. The
// StretchCluster CRD is only installed in multicluster operator mode
// (`cmd/multicluster`), which has its own setup path; watching that type
// from `cmd/run` would fail to start the cache because the API resource
// does not exist there.
type RedpandaMetricsReconciler struct {
	Manager multicluster.Manager

	// Labels we previously emitted on the per-cluster gauges. Retained so we
	// can DeleteLabelValues for Redpandas that have since been deleted —
	// without this Prometheus would keep reporting their last value forever.
	currentLabels collections.Set[redpandaMetricKey]

	// Misconfiguration reasons we previously emitted; same idea as
	// currentLabels but for the aggregated misconfigured gauge.
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
//
// Per-cluster failures (unreachable, list error) are logged and skipped rather
// than aborting the whole reconcile — partial metrics from healthy clusters
// are more useful than no metrics at all.
func (r *RedpandaMetricsReconciler) Reconcile(ctx context.Context, _ mcreconcile.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("RedpandaMetricsReconciler.Reconcile")

	seenLabels := collections.NewSet[redpandaMetricKey]()
	misconfiguredCounts := map[string]int{}

	var total int

	for _, clusterName := range r.Manager.GetClusterNames() {
		if !r.Manager.IsClusterReachable(clusterName) {
			logger.V(log.DebugLevel).Info("cluster unreachable, skipping for metrics", "cluster", clusterName)
			continue
		}
		cl, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			logger.Info("cannot get cluster for metrics, skipping", "cluster", clusterName, "error", err)
			continue
		}

		var rps redpandav1alpha2.RedpandaList
		if err := cl.GetClient().List(ctx, &rps); err != nil {
			logger.Info("listing Redpandas failed, skipping cluster", "cluster", clusterName, "error", err)
			continue
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

			redpandaDesiredNodes.WithLabelValues(key.namespace, key.name).Set(float64(desired))
			redpandaReadyNodes.WithLabelValues(key.namespace, key.name).Set(float64(ready))

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
			r.currentLabels.Delete(key)
		}
	}

	for reason, count := range misconfiguredCounts {
		redpandaMisconfiguredClusters.WithLabelValues(reason).Set(float64(count))
		r.currentConfigurationLabels.Add(reason)
	}
	for _, reason := range r.currentConfigurationLabels.Values() {
		if _, exists := misconfiguredCounts[reason]; !exists {
			redpandaMisconfiguredClusters.DeleteLabelValues(reason)
			r.currentConfigurationLabels.Delete(reason)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the metrics reconciler with the multicluster
// manager. It engages with both local and provider clusters so that metrics
// reflect Redpandas across every cluster the operator manages, and is wrapped
// with FilterNamespaceReconciler to honor the operator's --namespace flag,
// matching the convention used by the other v2 controllers in this package.
func (r *RedpandaMetricsReconciler) SetupWithManager(_ context.Context, mgr multicluster.Manager, namespace string) error {
	return mcbuilder.ControllerManagedBy(mgr).
		Named("redpanda-metrics").
		WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
			// Matches the convention used by other v2 controllers in this
			// package; avoids a duplicate-name panic when the multicluster
			// runtime reuses controller names across registrations during
			// tests.
			SkipNameValidation: ptr.To(true),
		}).
		For(&redpandav1alpha2.Redpanda{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Complete(controller.FilterNamespaceReconciler(namespace, r))
}
