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
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

var (
	redpandas = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "redpandas",
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
		redpandas,
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
// The reconciler runs against the local cluster's manager (Redpanda CRs only
// live there); operator-mode StretchCluster metrics are out of scope and
// would be added in a separate reconciler wired into `cmd/multicluster`.
type RedpandaMetricsReconciler struct {
	Client client.Client

	// Labels we previously emitted on the per-cluster gauges. Retained so we
	// can DeleteLabelValues for Redpandas that have since been deleted —
	// without this Prometheus would keep reporting their last value forever.
	currentLabels collections.Set[redpandaMetricKey]

	// Misconfiguration reasons we previously emitted; same idea as
	// currentLabels but for the aggregated misconfigured gauge.
	currentConfigurationLabels collections.Set[string]
}

// NewRedpandaMetricsReconciler creates a RedpandaMetricsReconciler bound to
// the local manager's client. Pass `mcmgr.GetLocalManager()` from a multicluster
// manager.
func NewRedpandaMetricsReconciler(mgr ctrl.Manager) *RedpandaMetricsReconciler {
	return &RedpandaMetricsReconciler{
		Client:                     mgr.GetClient(),
		currentLabels:              collections.NewConcurrentSet[redpandaMetricKey](),
		currentConfigurationLabels: collections.NewConcurrentSet[string](),
	}
}

// Reconcile lists every Redpanda known to the local manager and updates the
// registered Prometheus gauges. The incoming request is ignored: we always
// recompute totals from scratch so coalesced events do not produce stale
// values.
func (r *RedpandaMetricsReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	var rps redpandav1alpha2.RedpandaList
	if err := r.Client.List(ctx, &rps); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing Redpandas: %w", err)
	}

	seenLabels := collections.NewSet[redpandaMetricKey]()
	misconfiguredCounts := map[string]int{}

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

	redpandas.Set(float64(len(rps.Items)))

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

// SetupWithManager registers the metrics reconciler with the local manager.
// The operator's `--namespace` flag is honored implicitly: when set, the
// manager's cache is restricted to that namespace (see `cmd/run`), which
// scopes both the watch and the List in Reconcile.
func (r *RedpandaMetricsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("redpanda-metrics").
		For(&redpandav1alpha2.Redpanda{}).
		WithOptions(ctrlcontroller.Options{
			// Matches the convention used by other v2 controllers in this
			// package; avoids a duplicate-name panic when controller-runtime
			// reuses controller names across registrations during tests.
			SkipNameValidation: ptr.To(true),
		}).
		Complete(r)
}
