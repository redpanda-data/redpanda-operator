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
	"net/http"
	"sort"
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
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
)

// defaultClearMaintenanceModeAfter is how long a broker must be down (its pod
// not-Ready) while stuck in maintenance mode before the operator clears the
// maintenance flag. A broker left in maintenance mode is excluded from the
// partition balancer's auto-decommission (see Redpanda
// partition_balancer_planner: maintenance-mode nodes are filtered out), so a
// broker whose maintenance flag was never cleared (e.g. the pod's preStop hook
// enabled it but the postStart hook never ran because the pod can't schedule)
// will never be auto-decommissioned. Nothing distinguishes that stuck state
// from a broker an operator intentionally put into a longer planned
// maintenance window (there's no "who/why set this flag" signal available
// from the admin API — see rpadmin.MaintenanceStatus), so this default trades
// off responsiveness against the risk of clearing a broker that's still
// expected to come back. Default 30m — well past a normal rolling-restart
// window and most routine planned-maintenance reboots (e.g. an OS patch
// cycle); tune via --clear-maintenance-mode-after if your maintenance windows
// commonly run longer.
const defaultClearMaintenanceModeAfter = 30 * time.Minute

// podNotReadyFor returns how long the pod's Ready condition has been False; a
// Ready pod reports zero, which never reaches the (always positive) clear
// threshold. The transition time lives on the pod, so this survives operator
// restarts. A pod with no Ready condition at all (e.g. stuck Pending before
// the scheduler/kubelet ever gets to report Ready — the exact state of a pod
// whose node is cordoned or lost) is not-Ready since the pod was created, not
// for a fixed zero duration: a pod that's been Pending for the full threshold
// is precisely the stuck case decideClearMaintenance exists to unblock, and
// pinning the duration at 0 would mean its threshold gate could never fire
// for it. A freshly-created pod (CreationTimestamp ~= now) still comes out to
// ~zero duration, so it's no less protected than before.
func podNotReadyFor(pod *corev1.Pod, now time.Time) time.Duration {
	for _, cond := range pod.Status.Conditions {
		if cond.Type != corev1.PodReady {
			continue
		}
		if cond.Status == corev1.ConditionTrue {
			return 0
		}
		return now.Sub(cond.LastTransitionTime.Time)
	}
	return now.Sub(pod.CreationTimestamp.Time)
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
// label of each broker's advertised internal RPC address — bucketing every
// broker that shares a key rather than picking one. StatefulSet/pod names are
// not guaranteed globally unique across a StretchCluster's member clusters (a
// BrokerPool name collision across two member clusters yields the identical
// pod name in both), so a key can legitimately bucket more than one broker;
// callers must treat a multi-broker bucket as ambiguous rather than acting on
// whichever broker happened to be indexed last. Collision detection is
// key-relative, not broker-relative: it only catches brokers that land in the
// same bucket, not two brokers whose identities happen to collide across
// different keys (e.g. one broker's short pod-name key equalling a different
// broker's raw-IP key) — a currently unhandled, considerably rarer case. This
// lets us locate a
// persistently-down broker's pod (which is absent from the live-only brokerMap
// built during decommission) so the pod's not-Ready duration can gate the
// maintenance clear. Handles both "host" and "host:port" address forms; a bare
// IP address is keyed as-is.
func brokersByPodName(brokers []rpadmin.Broker) map[string][]rpadmin.Broker {
	out := make(map[string][]rpadmin.Broker, len(brokers))
	for _, b := range brokers {
		host := b.InternalRPCAddress
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// An empty address (malformed/unexpected admin API response) is not an
		// identity — indexing it would let it collide with the empty-string
		// lookup key clearStuckMaintenanceMode's PodIP fallback can produce for
		// an unscheduled pod (empty pod.Status.PodIP), mismatching an unrelated
		// broker to an unrelated pod.
		if host == "" {
			continue
		}
		// Full host covers a bare pod IP (flat-network mode, matched against
		// pod.Status.PodIP) and the FQDN.
		out[host] = append(out[host], b)
		// For a hostname (non-IP), also key by the first DNS label, which is
		// the pod name (single-cluster / stretch DNS mode).
		if net.ParseIP(host) == nil {
			podName := strings.SplitN(host, ".", 2)[0]
			out[podName] = append(out[podName], b)
		}
	}
	return out
}

// brokerInMaintenance reports whether the broker is currently draining for
// maintenance.
func brokerInMaintenance(b rpadmin.Broker) bool {
	return b.Maintenance != nil && b.Maintenance.Draining
}

// ghostBrokersInMaintenance returns brokers that are in maintenance mode,
// reported not-alive, and whose advertised internal RPC host:port is also
// advertised by a broker that IS alive under a different node id. That state
// arises when a pod loses its data directory (e.g. no persistent storage) and
// its broker re-registers under a fresh node id at the same stable address:
// the pod's preStop hook put the old id into maintenance mode, and the
// postStart hook of the replacement clears only the *new* id, leaking the old
// one (redpanda-data/redpanda-operator#1674). Because the live broker owns the
// address and node ids are never reused, the dead sharer can never rejoin —
// clearing its maintenance flag is safe without any down-duration threshold.
// Detection is keyed on the full advertised host (not the pod-name label, which
// can collide across StretchCluster members) plus the RPC port, so distinct
// brokers that merely share a hostname are never conflated. Only stable DNS
// names participate: a bare-IP advertised address (flat-network mode) is
// skipped outright, because Kubernetes reuses pod IPs — a gracefully
// restarting broker's IP can be handed to an unrelated pod whose broker would
// then masquerade as a "live successor", force-clearing maintenance on a
// broker that is still expected to come back under its old id. Flat-network
// ghosts are therefore never detected here (the replacement pod advertises a
// new IP, so no address is shared with the ghost anyway).
func ghostBrokersInMaintenance(brokers []rpadmin.Broker) []rpadmin.Broker {
	byAddr := make(map[string][]rpadmin.Broker, len(brokers))
	for _, b := range brokers {
		host := b.InternalRPCAddress
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "" {
			continue
		}
		if net.ParseIP(host) != nil {
			continue
		}
		key := fmt.Sprintf("%s:%d", host, b.InternalRPCPort)
		byAddr[key] = append(byAddr[key], b)
	}
	var ghosts []rpadmin.Broker
	for _, bucket := range byAddr {
		if len(bucket) < 2 {
			continue
		}
		anyAlive := false
		for _, b := range bucket {
			if brokerIsAlive(b) {
				anyAlive = true
				break
			}
		}
		if !anyAlive {
			// Nobody owns this address right now (e.g. the pod is
			// mid-restart); without a live successor we can't distinguish a
			// ghost from a broker that will come back under its old id.
			continue
		}
		for _, b := range bucket {
			if !brokerIsAlive(b) && brokerInMaintenance(b) {
				ghosts = append(ghosts, b)
			}
		}
	}
	sort.Slice(ghosts, func(i, j int) bool { return ghosts[i].NodeID < ghosts[j].NodeID })
	return ghosts
}

// brokerIsAlive reports the cluster's liveness view of the broker, defaulting to
// alive when the field is absent (so a missing value never triggers a clear).
func brokerIsAlive(b rpadmin.Broker) bool {
	return ptr.Deref(b.IsAlive, true)
}

// isMaintenanceAlreadyClearedErr reports whether an error from
// DisableMaintenanceMode means there is nothing left to clear: 400 (the broker
// is not in maintenance mode) or 404 (the broker id is gone entirely). Both
// are expected races on the ghost path — the chart's postStart hook and the
// operator clear the same ghost by design — and the loser of that race must
// not abort the rest of the reconcile pass.
func isMaintenanceAlreadyClearedErr(err error) bool {
	var httpErr *rpadmin.HTTPResponseError
	if !errors.As(err, &httpErr) || httpErr.Response == nil {
		return false
	}
	return httpErr.Response.StatusCode == http.StatusBadRequest || httpErr.Response.StatusCode == http.StatusNotFound
}

// clearStuckMaintenanceMode clears maintenance mode on two classes of broker:
// ghost brokers (dead ids superseded by a live broker at the same advertised
// address — cleared immediately, see ghostBrokersInMaintenance) and stuck
// brokers that are in maintenance, reported not-alive by the cluster, and
// whose pod has been not-Ready for at least the threshold. Shared by the
// single-cluster and StretchCluster reconcilers. It queries the full broker
// list (which includes down brokers, unlike the live-only brokerMap built
// during decommission) so a stuck-down broker can be matched to its pod by
// name (DNS mode) or IP (flat-network mode). A pod name that ambiguously
// matches more than one broker (see brokersByPodName) is skipped rather than
// guessed, since acting on the wrong broker would incorrectly clear
// maintenance mode on a broker that never satisfied the threshold.
func clearStuckMaintenanceMode(ctx context.Context, admin *rpadmin.AdminAPI, pods []*lifecycle.MulticlusterPod, threshold time.Duration, logger logr.Logger) error {
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return errors.Wrap(err, "listing brokers for maintenance-mode reconcile")
	}

	// Ghost brokers — dead ids superseded by a live broker at the same
	// advertised address — are cleared unconditionally: their pod is Ready
	// again (running the successor broker), so the not-Ready threshold below
	// would never fire for them, and the pod-name lookup would refuse the
	// two-brokers-one-name bucket as ambiguous. See ghostBrokersInMaintenance.
	podsByName := make(map[string][]*lifecycle.MulticlusterPod, len(pods))
	for _, pod := range pods {
		podsByName[pod.GetName()] = append(podsByName[pod.GetName()], pod)
	}
	for _, ghost := range ghostBrokersInMaintenance(brokers) {
		host := ghost.InternalRPCAddress
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// Cluster attribution for the log/metric is best-effort (the clear
		// itself targets a node id): pod names can collide across
		// StretchCluster members, so only attribute when the name maps to
		// exactly one pod rather than guessing and naming the wrong member.
		clusterName := ""
		if matched := podsByName[strings.SplitN(host, ".", 2)[0]]; len(matched) == 1 {
			clusterName = matched[0].GetCanonicalClusterName()
		}
		logger.Info("clearing maintenance mode on ghost broker superseded by a live broker at the same address",
			"nodeID", ghost.NodeID, "address", ghost.InternalRPCAddress, "cluster", clusterName)
		if err := admin.DisableMaintenanceMode(ctx, ghost.NodeID, true); err != nil {
			// The chart's postStart hook races the operator to clear the same
			// ghost by design; losing that race (400 not-in-maintenance, 404
			// broker gone) means the work is already done, not a failure.
			if isMaintenanceAlreadyClearedErr(err) {
				logger.Info("ghost broker maintenance mode was already cleared or the broker is gone",
					"nodeID", ghost.NodeID, "reason", err.Error())
				continue
			}
			return errors.Wrapf(err, "disabling maintenance mode for ghost broker %d", ghost.NodeID)
		}
		observability.MaintenanceModeGhostCleared.WithLabelValues(clusterName).Inc()
	}

	byPod := brokersByPodName(brokers)
	now := time.Now()
	for _, pod := range pods {
		// A Ready pod reports a zero not-ready duration, so the threshold
		// gate alone (threshold is always positive) skips healthy pods.
		notReadyFor := podNotReadyFor(pod.Pod, now)
		if notReadyFor < threshold {
			continue
		}
		candidates, ok := byPod[pod.GetName()]
		if !ok && pod.Status.PodIP != "" {
			// Only fall back to the PodIP key when the pod actually has one —
			// an unscheduled/Pending pod's PodIP is empty, and brokersByPodName
			// never indexes an empty key, but guarding here too keeps the two
			// lookups independently correct rather than relying on that.
			candidates, ok = byPod[pod.Status.PodIP]
		}
		if !ok {
			continue
		}
		if len(candidates) > 1 {
			logger.Info("not clearing maintenance mode: pod name matches multiple brokers, refusing to guess which one it is",
				"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(), "matchingBrokers", len(candidates))
			observability.MaintenanceModeClearSkippedAmbiguous.WithLabelValues(pod.GetCanonicalClusterName()).Inc()
			continue
		}
		b := candidates[0]
		clearBroker, reason := decideClearMaintenance(brokerInMaintenance(b), brokerIsAlive(b), notReadyFor, threshold)
		if !clearBroker {
			logger.Info("not clearing maintenance mode", "pod", pod.GetName(), "nodeID", b.NodeID, "reason", reason)
			continue
		}
		logger.Info("clearing stuck maintenance mode for long-down broker to unblock auto-decommission",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(), "nodeID", b.NodeID, "notReadyFor", notReadyFor.String(), "reason", reason)
		if err := admin.DisableMaintenanceMode(ctx, b.NodeID, true); err != nil {
			return errors.Wrapf(err, "disabling maintenance mode for broker %d", b.NodeID)
		}
		observability.MaintenanceModeCleared.WithLabelValues(pod.GetCanonicalClusterName()).Inc()
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
