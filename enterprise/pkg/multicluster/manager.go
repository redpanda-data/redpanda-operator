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
//
// This is a structural mirror of the OSS operator's
// pkg/multicluster.Manager: the method sets must stay identical so the raft
// manager built here satisfies the OSS interface (and vice versa) without
// either module importing the other. Compile-time satisfaction assertions in
// the OSS operator (operator/cmd/multicluster) pin the two together — if you
// change a method here, the OSS build breaks until its copy matches.
type Manager interface {
	mcmanager.Manager
	// GetLeader returns the name of the current raft leader cluster, or
	// empty if no leader has been elected yet.
	GetLeader() string
	// GetClusterNames returns the names of all clusters known to this manager.
	GetClusterNames() []string
	// GetConfiguredClusterNames returns the names of every cluster this
	// manager is configured to manage, whether or not the cluster has
	// completed runtime registration yet. For the raft manager this is the
	// static peer list from its configuration: bootstrap-mode peers appear
	// here from process start even though they are registered with the
	// runtime provider (and so appear in GetClusterNames) only after raft
	// leadership is acquired and their kubeconfig fetched. Use this, not
	// GetClusterNames, when the caller needs the complete expected cluster
	// set — e.g. to detect that a cross-cluster view is partial (K8S-891).
	GetConfiguredClusterNames() []string
	// GetLocalClusterName returns the canonical name of the local cluster.
	// In a multicluster setup this is the raft node name (e.g. "cluster-1").
	// In a single-cluster setup this returns mcmanager.LocalCluster ("").
	GetLocalClusterName() string
	// AddOrReplaceCluster registers or replaces a cluster. Cancelling ctx
	// stops the cluster.
	AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error
	// Health reports whether the manager's raft group is healthy.
	Health(req *http.Request) error
	// IsClusterReachable reports whether the Kubernetes API server of the
	// named cluster was recently reachable. The raft manager probes each
	// engaged cluster's API server in the background; this method returns
	// the cached result without making a network call. For single-cluster
	// and static managers the local cluster is always considered reachable.
	IsClusterReachable(clusterName string) bool
}
