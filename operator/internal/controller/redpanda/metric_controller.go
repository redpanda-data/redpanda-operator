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
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redpanda-data/common-go/otelutil/log"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// kind label values for the v2 metrics. They mirror the Kubernetes Kind name
// of each CRD so a Prometheus query maps cleanly onto `kubectl get <kind>`.
const (
	kindRedpanda       = "Redpanda"
	kindStretchCluster = "StretchCluster"
)

var (
	redpandasTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpandas_total",
			Help: "Number of Redpanda clusters managed by the operator, by CRD kind",
		}, []string{"kind"},
	)
	redpandaDesiredNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_desired_nodes",
			Help: "Desired number of broker pods per cluster, summed across all node pools",
		}, []string{"kind", "namespace", "name"},
	)
	redpandaReadyNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_ready_nodes",
			Help: "Number of broker pods reporting Ready per cluster, summed across all node pools",
		}, []string{"kind", "namespace", "name"},
	)
	redpandaMisconfiguredClusters = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_misconfigured_clusters",
			Help: "Number of clusters whose ConfigurationApplied condition is not True, by kind and reason",
		}, []string{"kind", "reason"},
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

// redpandaMetricKey identifies a cluster CR for the purpose of tracking
// previously-emitted Prometheus label values across reconciles. The kind is
// part of the key because Redpanda and StretchCluster CRs share the same
// gauge label set and could otherwise collide on (namespace, name).
type redpandaMetricKey struct {
	kind      string
	namespace string
	name      string
}

type misconfigKey struct {
	kind   string
	reason string
}

// RedpandaMetricsReconciler emits Prometheus metrics for the v2 Redpanda and
// StretchCluster CRDs, providing the parallel of the v1 ClusterMetricController.
// The reconciler ignores the incoming request and recomputes gauges from a
// fresh List so that values stay accurate even when individual events are
// coalesced — matching the v1 pattern.
//
// One reconciler instance backs two controller-runtime controllers (one per
// CRD), so a Reconcile triggered from a Redpanda event still also re-lists
// StretchClusters, and vice versa. The mutex serializes those two trigger
// paths so they do not race on gauge label cleanup.
type RedpandaMetricsReconciler struct {
	Manager multicluster.Manager

	// mu serializes Reconcile invocations so the two controllers backing this
	// reconciler do not race on label cleanup.
	mu sync.Mutex

	// Labels we previously emitted on per-cluster gauges. Retained so we can
	// DeleteLabelValues for clusters that have since been deleted — without
	// this, Prometheus would keep reporting their last value indefinitely.
	currentLabels collections.Set[redpandaMetricKey]

	// Misconfiguration reasons we previously emitted. Same idea as
	// currentLabels but for the aggregated misconfigured gauge.
	currentMisconfigReasons collections.Set[misconfigKey]
}

// NewRedpandaMetricsReconciler creates a RedpandaMetricsReconciler.
func NewRedpandaMetricsReconciler(mgr multicluster.Manager) *RedpandaMetricsReconciler {
	return &RedpandaMetricsReconciler{
		Manager:                 mgr,
		currentLabels:           collections.NewSet[redpandaMetricKey](),
		currentMisconfigReasons: collections.NewSet[misconfigKey](),
	}
}

// Reconcile lists Redpandas and StretchClusters across every cluster known to
// the multicluster Manager and updates the registered Prometheus gauges. The
// incoming request is ignored.
//
// Per-cluster failures (unreachable, list error) are logged and skipped rather
// than aborting the entire reconcile — partial metrics from healthy clusters
// are more useful than no metrics at all. StretchCluster CRs are deduped by
// (namespace, name) since the same StretchCluster is mirrored across every
// participating k8s cluster.
func (r *RedpandaMetricsReconciler) Reconcile(ctx context.Context, _ mcreconcile.Request) (ctrl.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	logger := log.FromContext(ctx).WithName("RedpandaMetricsReconciler.Reconcile")

	seenLabels := collections.NewSet[redpandaMetricKey]()
	seenStretchKeys := collections.NewSet[redpandaMetricKey]()
	misconfigCounts := map[misconfigKey]int{}

	var redpandaCount, stretchClusterCount int

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
		k8sClient := cl.GetClient()

		var rps redpandav1alpha2.RedpandaList
		if err := k8sClient.List(ctx, &rps); err != nil {
			logger.Info("listing Redpandas failed, skipping", "cluster", clusterName, "error", err)
		} else {
			for i := range rps.Items {
				rp := &rps.Items[i]
				key := redpandaMetricKey{kind: kindRedpanda, namespace: rp.Namespace, name: rp.Name}
				redpandaCount++
				r.recordCluster(key, rp.Status.NodePools, rp.Status.Conditions, statuses.ClusterConfigurationApplied, misconfigCounts)
				seenLabels.Add(key)
			}
		}

		var scs redpandav1alpha2.StretchClusterList
		if err := k8sClient.List(ctx, &scs); err != nil {
			logger.Info("listing StretchClusters failed, skipping", "cluster", clusterName, "error", err)
		} else {
			for i := range scs.Items {
				sc := &scs.Items[i]
				key := redpandaMetricKey{kind: kindStretchCluster, namespace: sc.Namespace, name: sc.Name}
				// The same StretchCluster CR is replicated to every participating
				// k8s cluster; dedup so it counts once and one cluster's view
				// (the first one we see) wins for the gauge value.
				if seenStretchKeys.HasAny(key) {
					continue
				}
				seenStretchKeys.Add(key)
				stretchClusterCount++
				r.recordCluster(key, sc.Status.NodePools, sc.Status.Conditions, statuses.StretchClusterConfigurationApplied, misconfigCounts)
				seenLabels.Add(key)
			}
		}
	}

	redpandasTotal.WithLabelValues(kindRedpanda).Set(float64(redpandaCount))
	redpandasTotal.WithLabelValues(kindStretchCluster).Set(float64(stretchClusterCount))

	// Cleanup per-cluster gauge labels that no longer correspond to a live CR.
	for _, key := range r.currentLabels.Values() {
		if !seenLabels.HasAny(key) {
			redpandaDesiredNodes.DeleteLabelValues(key.kind, key.namespace, key.name)
			redpandaReadyNodes.DeleteLabelValues(key.kind, key.namespace, key.name)
			r.currentLabels.Delete(key)
		}
	}

	// Update misconfigured gauge for each (kind, reason) seen this round and
	// drop labels for reasons that no longer apply.
	seenReasons := collections.NewSet[misconfigKey]()
	for k, count := range misconfigCounts {
		redpandaMisconfiguredClusters.WithLabelValues(k.kind, k.reason).Set(float64(count))
		seenReasons.Add(k)
		r.currentMisconfigReasons.Add(k)
	}
	for _, k := range r.currentMisconfigReasons.Values() {
		if !seenReasons.HasAny(k) {
			redpandaMisconfiguredClusters.DeleteLabelValues(k.kind, k.reason)
			r.currentMisconfigReasons.Delete(k)
		}
	}

	return ctrl.Result{}, nil
}

// recordCluster sets the per-cluster desired/ready gauges and accumulates the
// misconfiguration count for one CR. Factored out to keep the per-CRD list
// loops in Reconcile readable.
func (r *RedpandaMetricsReconciler) recordCluster(
	key redpandaMetricKey,
	pools []redpandav1alpha2.EmbeddedNodePoolStatus,
	conditions []metav1.Condition,
	configurationAppliedCondition string,
	misconfigCounts map[misconfigKey]int,
) {
	var desired, ready int32
	for _, pool := range pools {
		desired += pool.DesiredReplicas
		ready += pool.ReadyReplicas
	}
	redpandaDesiredNodes.WithLabelValues(key.kind, key.namespace, key.name).Set(float64(desired))
	redpandaReadyNodes.WithLabelValues(key.kind, key.namespace, key.name).Set(float64(ready))
	r.currentLabels.Add(key)

	if cond := apimeta.FindStatusCondition(conditions, configurationAppliedCondition); cond != nil && cond.Status != metav1.ConditionTrue {
		misconfigCounts[misconfigKey{kind: key.kind, reason: cond.Reason}]++
	}
}

// SetupWithManager registers two controllers — one per watched CRD — that
// share a single reconciler instance. The shared reconciler's mutex serializes
// concurrent invocations from the two trigger paths.
func (r *RedpandaMetricsReconciler) SetupWithManager(_ context.Context, mgr multicluster.Manager, namespace string) error {
	register := func(name string, obj client.Object) error {
		return mcbuilder.ControllerManagedBy(mgr).
			Named(name).
			WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
				// Matches the convention used by other v2 controllers in this
				// package; avoids a duplicate-name panic when the multicluster
				// runtime reuses controller names across registrations during
				// tests.
				SkipNameValidation: ptr.To(true),
			}).
			For(obj,
				mcbuilder.WithEngageWithLocalCluster(true),
				mcbuilder.WithEngageWithProviderClusters(true),
			).
			Complete(controller.FilterNamespaceReconciler(namespace, r))
	}

	if err := register("redpanda-metrics-redpanda", &redpandav1alpha2.Redpanda{}); err != nil {
		return err
	}
	return register("redpanda-metrics-stretchcluster", &redpandav1alpha2.StretchCluster{})
}
