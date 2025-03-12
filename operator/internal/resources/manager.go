// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultOwnerLabel     = "cluster.redpanda.com/owner"
	defaultOperatorLabel  = "cluster.redpanda.com/operator"
	defaultNamespaceLabel = "cluster.redpanda.com/namespace"
	defaultNodepoolLabel  = "cluster.redpanda.com/nodepool"
)

type ClusterStatus struct {
	Replicas          int
	DesiredReplicas   int
	OutOfDateReplicas int
	UpToDateReplicas  int
	CondemnedReplicas int
	HealthyReplicas   int
	RunningReplicas   int
	Quiesced          bool
}

type OwnershipResolver[T any, U Cluster[T]] interface {
	GetOwnerLabels(cluster U) map[string]string
	OwnerForObject(object client.Object) *types.NamespacedName
}

type SimpleResourceRenderer[T any, U Cluster[T]] interface {
	Render(ctx context.Context, cluster U) ([]client.Object, error)
	WatchedResourceTypes() []client.Object
}

type NodePoolRenderer[T any, U Cluster[T]] interface {
	Render(ctx context.Context, cluster U) ([]*appsv1.StatefulSet, error)
	IsNodePool(object client.Object) bool
}

type ClusterStatusUpdater[T any, U Cluster[T]] interface {
	Update(cluster U, status ClusterStatus) bool
}

type ResourceManagerFactory[T any, U Cluster[T]] func(mgr ctrl.Manager) (OwnershipResolver[T, U], ClusterStatusUpdater[T, U], NodePoolRenderer[T, U], SimpleResourceRenderer[T, U])

func V2ResourceManagers(mgr ctrl.Manager) (
	OwnershipResolver[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda],
	ClusterStatusUpdater[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda],
	NodePoolRenderer[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda],
	SimpleResourceRenderer[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda],
) {
	return NewV2OwnershipResolver(), NewV2ClusterStatusUpdater(), NewV2NodePoolRenderer(mgr), NewV2SimpleResourceRenderer(mgr)
}
