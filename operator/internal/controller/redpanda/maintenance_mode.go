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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

// defaultClearMaintenanceModeAfter is how long a broker must be down (its pod
// not-Ready) while stuck in maintenance mode before the operator clears the
// maintenance flag. A broker left in maintenance mode is excluded from the
// partition balancer's auto-decommission (see Redpanda
// partition_balancer_planner: maintenance-mode nodes are filtered out), so a
// broker whose maintenance flag was never cleared (e.g. the pod's preStop hook
// enabled it but the postStart hook never ran because the pod can't schedule)
// will never be auto-decommissioned. Default 5m — comfortably longer than a
// normal rolling-restart maintenance window.
const defaultClearMaintenanceModeAfter = 5 * time.Minute

// podNotReadyFor returns how long the pod's Ready condition has been False and
// whether it is currently not-Ready. The transition time lives on the pod, so
// this survives operator restarts. A pod with no Ready condition (e.g. just
// created / never scheduled) is treated as not-Ready for zero duration, so the
// threshold gate in decideClearMaintenance still protects it.
func podNotReadyFor(pod *corev1.Pod, now time.Time) (time.Duration, bool) {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodReady {
			continue
		}
		if cond.Status == corev1.ConditionTrue {
			return 0, false
		}
		return now.Sub(cond.LastTransitionTime.Time), true
	}
	return 0, true
}

// decideClearMaintenance is the guard for clearing a broker's maintenance mode.
// All must hold: the broker is in maintenance (draining), the cluster reports it
// not-alive, and its pod has been not-Ready at least the threshold. The
// not-alive requirement ensures we never clear maintenance on a broker that is
// merely mid-rolling-restart (alive, briefly draining); the threshold ensures we
// only act on a sustained outage, not a transient blip.
func decideClearMaintenance(inMaintenance, isAlive bool, notReadyFor, threshold time.Duration) (bool, string) {
	switch {
	case !inMaintenance:
		return false, "broker not in maintenance mode"
	case isAlive:
		return false, "broker is alive; not a stuck-down maintenance state"
	case notReadyFor < threshold:
		return false, fmt.Sprintf("pod not-ready for %s < threshold %s", notReadyFor, threshold)
	default:
		return true, fmt.Sprintf("broker in maintenance and down for %s (>= %s); clearing to unblock auto-decommission", notReadyFor, threshold)
	}
}

// brokersByPodName indexes the cluster broker list by pod name — the first DNS
// label of each broker's advertised internal RPC address. This lets us locate a
// persistently-down broker's pod (which is absent from the live-only brokerMap
// built during decommission) so the pod's not-Ready duration can gate the
// maintenance clear. Handles both "host" and "host:port" address forms; a bare
// IP address is keyed as-is.
func brokersByPodName(brokers []rpadmin.Broker) map[string]rpadmin.Broker {
	out := make(map[string]rpadmin.Broker, len(brokers))
	for _, b := range brokers {
		host := b.InternalRPCAddress
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// Full host covers a bare pod IP (flat-network mode, matched against
		// pod.Status.PodIP) and the FQDN.
		out[host] = b
		// For a hostname (non-IP), also key by the first DNS label, which is
		// the pod name (single-cluster / stretch DNS mode).
		if net.ParseIP(host) == nil {
			out[strings.SplitN(host, ".", 2)[0]] = b
		}
	}
	return out
}

// brokerInMaintenance reports whether the broker is currently draining for
// maintenance.
func brokerInMaintenance(b rpadmin.Broker) bool {
	return b.Maintenance != nil && b.Maintenance.Draining
}

// brokerIsAlive reports the cluster's liveness view of the broker, defaulting to
// alive when the field is absent (so a missing value never triggers a clear).
func brokerIsAlive(b rpadmin.Broker) bool {
	return ptr.Deref(b.IsAlive, true)
}

// clearStuckMaintenanceMode clears maintenance mode on any broker that is in
// maintenance, reported not-alive by the cluster, and whose pod has been
// not-Ready for at least the threshold. Shared by the single-cluster and
// StretchCluster reconcilers. It queries the full broker list (which includes
// down brokers, unlike the live-only brokerMap built during decommission) so a
// stuck-down broker can be matched to its pod by name (DNS mode) or IP
// (flat-network mode).
func clearStuckMaintenanceMode(ctx context.Context, admin *rpadmin.AdminAPI, pods []*lifecycle.MulticlusterPod, threshold time.Duration, logger logr.Logger) error {
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing brokers for maintenance-mode reconcile")
	}
	byPod := brokersByPodName(brokers)
	now := time.Now()
	for _, pod := range pods {
		notReadyFor, notReady := podNotReadyFor(pod.Pod, now)
		if !notReady || notReadyFor < threshold {
			continue
		}
		b, ok := byPod[pod.GetName()]
		if !ok {
			b, ok = byPod[pod.Status.PodIP]
		}
		if !ok {
			continue
		}
		clear, reason := decideClearMaintenance(brokerInMaintenance(b), brokerIsAlive(b), notReadyFor, threshold)
		if !clear {
			logger.V(log.TraceLevel).Info("not clearing maintenance mode", "pod", pod.GetName(), "nodeID", b.NodeID, "reason", reason)
			continue
		}
		logger.Info("clearing stuck maintenance mode for long-down broker to unblock auto-decommission",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(), "nodeID", b.NodeID, "notReadyFor", notReadyFor.String(), "reason", reason)
		if err := admin.DisableMaintenanceMode(ctx, b.NodeID, true); err != nil {
			return errors.Wrapf(err, "disabling maintenance mode for broker %d", b.NodeID)
		}
	}
	return nil
}

// reconcileMaintenanceMode (StretchCluster) clears maintenance mode on brokers
// that have been down past the threshold — see clearStuckMaintenanceMode.
func (r *MulticlusterReconciler) reconcileMaintenanceMode(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx).WithName("reconcileMaintenanceMode")
	err := clearStuckMaintenanceMode(ctx, state.admin, state.pools.ExistingPods(), r.maintenanceModeClearThreshold(), logger)
	return ctrl.Result{}, err
}

func (r *MulticlusterReconciler) maintenanceModeClearThreshold() time.Duration {
	if r.MaintenanceModeClearThreshold > 0 {
		return r.MaintenanceModeClearThreshold
	}
	return defaultClearMaintenanceModeAfter
}

// reconcileMaintenanceMode (single-cluster Redpanda) clears maintenance mode on
// brokers that have been down past the threshold — see clearStuckMaintenanceMode.
func (r *RedpandaReconciler) reconcileMaintenanceMode(ctx context.Context, state *clusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}
	logger := log.FromContext(ctx).WithName("reconcileMaintenanceMode")
	err := clearStuckMaintenanceMode(ctx, state.admin, state.pools.ExistingPods(), r.maintenanceModeClearThreshold(), logger)
	return ctrl.Result{}, err
}

func (r *RedpandaReconciler) maintenanceModeClearThreshold() time.Duration {
	if r.MaintenanceModeClearThreshold > 0 {
		return r.MaintenanceModeClearThreshold
	}
	return defaultClearMaintenanceModeAfter
}
