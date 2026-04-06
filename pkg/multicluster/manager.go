// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// Manager extends the multicluster manager interface with raft leader
// awareness, dynamic cluster management, and health checking.
type Manager interface {
	mcmanager.Manager
	// GetLeader returns the name of the current raft leader cluster, or
	// empty if no leader has been elected yet.
	GetLeader() string
	// GetClusterNames returns the names of all clusters known to this manager.
	GetClusterNames() []string
	// GetLocalClusterName returns the canonical name of the local cluster.
	// In a multicluster setup this is the raft node name (e.g. "cluster-1").
	// In a single-cluster setup this returns mcmanager.LocalCluster ("").
	GetLocalClusterName() string
	// AddOrReplaceCluster registers or replaces a cluster. Cancelling ctx
	// stops the cluster.
	AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error
	// Health reports whether the manager's raft group is healthy.
	Health(req *http.Request) error
}
