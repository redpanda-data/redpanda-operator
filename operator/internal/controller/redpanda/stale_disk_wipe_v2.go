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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	entcontroller "github.com/redpanda-data/redpanda-operator/enterprise/operator/controller"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// The stale-disk wipe core (StaleDiskWipe) lives in the enterprise controller
// package alongside the StretchCluster reconciler that shares it; this file
// holds the single-cluster RedpandaReconciler's thin entry point, which
// converts its OSS lifecycle pods and clients at the boundary (see
// lifecycle_conversion.go) and extracts the staged node_id_overrides guard
// from its own (OSS-typed) spec.

// reconcileStaleDiskWipe (single-cluster Redpanda) — see
// entcontroller.StaleDiskWipe.
func (r *RedpandaReconciler) reconcileStaleDiskWipe(ctx context.Context, state *clusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("reconcileStaleDiskWipe")

	if r.staleDiskWipeDisabled() {
		logger.V(log.TraceLevel).Info("stale-disk wipe disabled via non-positive threshold; skipping")
		return ctrl.Result{}, nil
	}

	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}

	return entcontroller.StaleDiskWipe(ctx, entcontroller.StaleDiskWipeParams{
		Pods:            toEnterprisePods(state.pools.ExistingPods()),
		Endpoints:       state.podEndpoints,
		Dial:            r.podAdminDialer(state),
		Deleter:         ossPodDeleter{client: r.LifecycleClient},
		Threshold:       r.staleDiskWipeThreshold(),
		Logs:            ossPodLogsReader(r.LifecycleClient),
		Debounce:        &r.staleDiskWipeDebounce,
		ConfirmInterval: entcontroller.StaleDiskWipeConfirmationInterval,
		OverrideUUIDs:   configuredOverrideUUIDs(clusterSpecConfig(state.cluster.Redpanda.Spec.ClusterSpec)),
	}, logger)
}

// configuredOverrideUUIDs extracts the staged node_id_overrides uuids from a
// cluster's *Config (nil-safe), for the per-identity wipe defer. The
// StretchCluster reconciler applies the same guard with its own
// (enterprise-typed) helper before calling StaleDiskWipe.
func configuredOverrideUUIDs(cfg *redpandav1alpha2.Config) map[string]struct{} {
	if cfg == nil || cfg.Node == nil {
		return nil
	}
	return entcontroller.StagedOverrideUUIDs(cfg.Node.Raw)
}

// clusterSpecConfig returns the *Config from a (possibly nil) RedpandaClusterSpec.
func clusterSpecConfig(cs *redpandav1alpha2.RedpandaClusterSpec) *redpandav1alpha2.Config {
	if cs == nil {
		return nil
	}
	return cs.Config
}

func (r *RedpandaReconciler) staleDiskWipeThreshold() time.Duration {
	if r.StaleDiskWipeNotReadyThreshold > 0 {
		return r.StaleDiskWipeNotReadyThreshold
	}
	return entcontroller.DefaultStaleDiskWipeNotReadyThreshold
}

// staleDiskWipeDisabled reports whether the wipe is off. Zero or negative
// disables it (matching the sibling --unbind-pvcs-after convention); any
// positive value tunes the not-ready threshold. Defaults differ: StretchCluster
// 5m (on), single-cluster 0 (off, opt-in).
func (r *RedpandaReconciler) staleDiskWipeDisabled() bool {
	return r.StaleDiskWipeNotReadyThreshold <= 0
}
