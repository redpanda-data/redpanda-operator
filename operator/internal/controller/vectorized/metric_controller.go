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
	"context"

	"github.com/cockroachdb/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

var (
	redpandaClusters = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "redpanda_clusters_total",
			Help: "Number of Redpanda clusters running",
		},
	)
	desireRedpandaNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "desired_redpanda_nodes_total",
			Help: "Number of desire Redpanda nodes",
		}, []string{"cluster"},
	)
	actualRedpandaNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "actual_redpanda_nodes_total",
			Help: "Number of actual Redpanda nodes",
		}, []string{"cluster"},
	)
	misconfiguredClusters = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redpanda_misconfigured_clusters_total",
			Help: "Number of Redpanda clusters having configuration problems",
		}, []string{"reason"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(redpandaClusters, desireRedpandaNodes, actualRedpandaNodes, misconfiguredClusters)
}

// ClusterMetricController provides metrics for nodes and cluster
type ClusterMetricController struct {
	client.Client
	// Brokers lists Broker CRs for operator_broker_phase on every pass; a
	// missing Broker CRD is tolerated (see BrokerPhaseRecorder.Observe).
	Brokers *kube.Ctl

	currentLabels              collections.Set[string]
	currentConfigurationLabels collections.Set[string]
	brokerPhases               *observability.BrokerPhaseRecorder
}

// NewClusterMetricsController creates ClusterMetricController
func NewClusterMetricsController(c client.Client, brokers *kube.Ctl) *ClusterMetricController {
	return &ClusterMetricController{
		Client:                     c,
		Brokers:                    brokers,
		currentLabels:              collections.NewConcurrentSet[string](),
		currentConfigurationLabels: collections.NewConcurrentSet[string](),
		brokerPhases:               observability.NewBrokerPhaseRecorder(observability.BrokerKindCluster),
	}
}

// Reconcile gets all Redpanda cluster and registers metrics for them
func (r *ClusterMetricController) Reconcile(
	c context.Context, _ ctrl.Request,
) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	cl := vectorizedv1alpha1.ClusterList{}
	err := r.List(ctx, &cl, &client.ListOptions{})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "unable to list the clusters")
	}

	redpandaClusters.Set(float64(len(cl.Items)))

	curLabels := collections.NewSet[string]()

	for i := range cl.Items {
		g, err := desireRedpandaNodes.GetMetricWithLabelValues(cl.Items[i].Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		g.Set(float64(cl.Items[i].GetDesiredReplicas()))

		g, err = actualRedpandaNodes.GetMetricWithLabelValues(cl.Items[i].Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		g.Set(float64(cl.Items[i].Status.ReadyReplicas))
		curLabels.Add(cl.Items[i].Name)
		r.currentLabels.Add(cl.Items[i].Name)
	}

	for _, key := range r.currentLabels.Values() {
		if !curLabels.HasAny(key) {
			desireRedpandaNodes.DeleteLabelValues(key)
			actualRedpandaNodes.DeleteLabelValues(key)
			r.currentLabels.Delete(key)
		}
	}

	misconfiguredClustersCount := make(map[string]int)
	for i := range cl.Items {
		if cond := cl.Items[i].Status.GetCondition(vectorizedv1alpha1.ClusterConfiguredConditionType); cond != nil && cond.Status != corev1.ConditionTrue {
			cur := misconfiguredClustersCount[cond.Reason]
			cur++
			misconfiguredClustersCount[cond.Reason] = cur
		}
	}
	for k, c := range misconfiguredClustersCount {
		g, err := misconfiguredClusters.GetMetricWithLabelValues(k)
		if err != nil {
			return ctrl.Result{}, err
		}
		g.Set(float64(c))
		r.currentConfigurationLabels.Add(k)
	}
	for _, k := range r.currentConfigurationLabels.Values() {
		if _, exists := misconfiguredClustersCount[k]; !exists {
			misconfiguredClusters.DeleteLabelValues(k)
			r.currentConfigurationLabels.Delete(k)
		}
	}

	if err := r.brokerPhases.Observe(ctx, r.Brokers); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: observability.ResourceStateRequeue}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterMetricController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorizedv1alpha1.Cluster{}).
		Owns(&corev1.Pod{}).
		WithOptions(controller.Options{SkipNameValidation: ptr.To(true)}).
		Complete(r)
}
