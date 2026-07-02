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

	"github.com/redpanda-data/common-go/otelutil/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/finalizers,verbs=update

type BrokerReconciler struct {
	Manager multicluster.Manager
}

func SetupBrokerController(_ context.Context, mgr multicluster.Manager, namespace string) error {
	return mcbuilder.ControllerManagedBy(mgr).
		For(
			&redpandav1alpha2.Broker{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).Complete(
		controller.FilterNamespaceReconciler(
			namespace,
			observability.Wrap[mcreconcile.Request](&BrokerReconciler{
				Manager: mgr,
			}, "Broker", periodicRequeue)))
}

func (r *BrokerReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("BrokerReconciler.Reconcile")
	l.Info("Reconciling", "object", req.NamespacedName.String(), "cluster", req.ClusterName)

	k8sCluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	broker := &redpandav1alpha2.Broker{}
	if err := k8sCluster.GetClient().Get(ctx, req.NamespacedName, broker); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return ctrl.Result{}, nil
}
