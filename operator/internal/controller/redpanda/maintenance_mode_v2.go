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
	"time"

	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	entcontroller "github.com/redpanda-data/redpanda-operator/enterprise/operator/controller"
)

// The maintenance-mode remediation core (ClearStuckMaintenanceMode) lives in
// the enterprise controller package alongside the StretchCluster reconciler
// that shares it; this file holds the single-cluster RedpandaReconciler's
// thin entry point, which converts its OSS lifecycle pods at the boundary
// (see lifecycle_conversion.go) and injects its own dialer.

// reconcileMaintenanceMode (single-cluster Redpanda) clears maintenance mode on
// brokers that have been down past the threshold — see
// entcontroller.ClearStuckMaintenanceMode.
func (r *RedpandaReconciler) reconcileMaintenanceMode(ctx context.Context, state *clusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx).WithName("reconcileMaintenanceMode")
	err := entcontroller.ClearStuckMaintenanceMode(ctx, state.admin, toEnterprisePods(state.pools.ExistingPods()), r.maintenanceModeClearThreshold(),
		entcontroller.NewPodIdentityGhostConfig(state.podEndpoints, r.podAdminDialer(state)), logger)
	return ctrl.Result{}, err
}

// podAdminDialer returns a PodIdentityDialer for one single-cluster broker pod:
// the cluster-wide admin client scoped to the pod's endpoint via ForHost, the
// same pattern the per-broker restart probes use.
func (r *RedpandaReconciler) podAdminDialer(state *clusterReconciliationState) entcontroller.PodIdentityDialer {
	return func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		return state.admin.ForHost(endpoint)
	}
}

func (r *RedpandaReconciler) maintenanceModeClearThreshold() time.Duration {
	if r.MaintenanceModeClearThreshold > 0 {
		return r.MaintenanceModeClearThreshold
	}
	return entcontroller.DefaultClearMaintenanceModeAfter
}
