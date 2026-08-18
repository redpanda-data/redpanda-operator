// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
	"github.com/redpanda-data/redpanda-operator/enterprise/pkg/multicluster"
)

// These shared controller-package constants and helpers are deliberate
// duplicates of the OSS redpanda controller package's originals
// (operator/internal/controller/redpanda: redpanda_controller.go and
// nodepool_controller.go), which stay behind in the OSS operator because its
// other controllers use them too. This module must not import that package,
// so the values are duplicated here; equality with the originals is pinned by
// the drift test in the OSS controller package
// (operator/internal/controller/redpanda/enterprise_shared_drift_test.go).

const (
	// FinalizerKey is the finalizer applied to managed resources.
	FinalizerKey = "operator.redpanda.com/finalizer"

	// RequeueTimeout is the time that the reconciler will wait before
	// requeueing a cluster to be reconciled when we've explicitly aborted a
	// reconciliation loop early due to an in-progress operation.
	RequeueTimeout = 10 * time.Second

	// FinalizerRequeueTimeout is the time that the reconciler will wait
	// before requeueing a reconciliation after patching a finalizer.
	FinalizerRequeueTimeout = 1 * time.Second

	// PeriodicRequeue is the maximal period between re-examining a cluster;
	// this is used to ensure that we regularly reassert cluster configuration
	// (which may depend on external secrets, for which there's no change
	// event we can latch onto).
	PeriodicRequeue = 3 * time.Minute

	// DefaultReconcileTimeout is a defense-in-depth ceiling on a single
	// reconcile pass when the reconciler struct leaves ReconcileTimeout at
	// zero. See the OSS original for sizing rationale.
	DefaultReconcileTimeout = 2 * time.Minute

	// messageNoBrokers is the status message used when a cluster has no
	// desired brokers.
	messageNoBrokers = "Cluster has no desired brokers"
)

// ignoreConflict ignores errors that happen due to optimistic locking
// checks. This is safe because it means that the client-side cache
// hasn't yet received the update of the resource from the server, and
// once it does reconciliation will be retriggered. To be safe, we
// also explicitly trigger a requeue.
func ignoreConflict(err error) (ctrl.Result, error) {
	if apierrors.IsConflict(err) {
		return ctrl.Result{RequeueAfter: RequeueTimeout}, nil
	}
	return ctrl.Result{}, err
}

// createCanonicalClusterNameList returns the canonical names of every cluster
// known to the manager, for log lines that enumerate the known clusters.
func createCanonicalClusterNameList(mgr multicluster.Manager) []string {
	var canonicalClusterList []string
	for _, clusterName := range mgr.GetClusterNames() {
		canonicalClusterList = append(canonicalClusterList, lifecycle.CanonicalClusterName(clusterName, mgr.GetLocalClusterName))
	}
	return canonicalClusterList
}
