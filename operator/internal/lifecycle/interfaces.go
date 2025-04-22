// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"

	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterStatus represents a generic status of a cluster
// based on its desired and actual state as well as whether
// it has reached a finalized reconciliation state.
type ClusterStatus struct {
	// Pools contains the status for each of our pools
	Pools []PoolStatus
	// Status is a generated status conditions container for clusters
	Status *statuses.ClusterStatus
}

type PoolStatus struct {
	// Name is the name of the pool
	Name string
	// Replicas is the number of actual replicas currently across
	// the node pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all node pools.
	DesiredReplicas int32
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their node pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32
	// UpToDateReplicas is the number of replicas that currently match
	// their node pool definitions.
	UpToDateReplicas int32
	// CondemnedReplicas is the number of replicas that will be decommissioned
	// as part of a scaling down operation.
	CondemnedReplicas int32
	// ReadyReplicas is the number of replicas whose readiness probes are
	// currently passing.
	ReadyReplicas int32
	// RunningReplicas is the number of replicas that are actively in a running
	// state.
	RunningReplicas int32
}

// NewClusterStatus creates a cluster status object to be used in reconciliation
func NewClusterStatus() *ClusterStatus {
	return &ClusterStatus{
		Status: statuses.NewCluster(),
	}
}

// OwnershipResolver is responsible for determining what
// labels get placed on every resource, including both
// node pools and simple resources, as well as mapping
// an object back to the particular cluster that it was
// created by. Rather than purely using owner references,
// the labels allow us to "own" both cluster and namespace
// scoped resources.
type OwnershipResolver[T any, U Cluster[T]] interface {
	// GetOwnerLabels returns the minimal set of labels that
	// can identify ownership of an object.
	GetOwnerLabels(cluster U) map[string]string
	// AddLabels returns the labels to be applied
	// to every resource created by the cluster.
	AddLabels(cluster U) map[string]string
	// OwnerForObject maps an "owned" object back to a
	// particular cluster. If the object does not map
	// to a cluster, return nil.
	OwnerForObject(object client.Object) *types.NamespacedName
}

// SimpleResourceRenderer handles compilation of all desired
// resources to be created by a cluster. These resources should
// be "simple" in nature in that we don't need to manually control
// their lifecycles and can easily create/update/delete them as
// necessary.
type SimpleResourceRenderer[T any, U Cluster[T]] interface {
	// Render returns the list of all simple resources to create
	// for a given cluster.
	Render(ctx context.Context, cluster U) ([]client.Object, error)
	// WatchedResourceTypes returns a list of all resources that
	// our controller should watch for changes to trigger reconciliation.
	WatchedResourceTypes() []client.Object
}

// NodePoolRender handles returning the node pools for a given cluster.
// These are handled separately from "simple" resources because we need
// to manage their lifecycle, decommissioning broker nodes and scaling
// clusters up and down as necessary.
type NodePoolRenderer[T any, U Cluster[T]] interface {
	// Render returns the list of node pools to create for a given cluster.
	Render(ctx context.Context, cluster U) ([]*appsv1.StatefulSet, error)
	// IsNodePool allows us to distinguish owned resources that are StatefulSets
	// but not node pools from resources that are. This is important if we
	// create StatefulSets that we don't need to consider part of a cluster's
	// node pools.
	IsNodePool(object client.Object) bool
}

// ClusterStatusUpdater handles back propagating the unified ClusterStatus onto
// the given cluster.
type ClusterStatusUpdater[T any, U Cluster[T]] interface {
	// Update updates the internal cluster's status based on the ClusterStatus it
	// is given. If any fields are updated it should return `true` so that the
	// status of the cluster can be synced.
	Update(cluster U, status *ClusterStatus) bool
}

// ResourceManagerFactory bundles together concrete implementations of OwnershipResolver
// ClusterStatusUpdater, NodePoolRenderer, and SimpleResourceRenderer for our various
// cluster versions.
type ResourceManagerFactory[T any, U Cluster[T]] func(mgr ctrl.Manager) (OwnershipResolver[T, U], ClusterStatusUpdater[T, U], NodePoolRenderer[T, U], SimpleResourceRenderer[T, U])
