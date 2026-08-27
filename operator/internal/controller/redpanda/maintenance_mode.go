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
	"sync"
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

// defaultClearMaintenanceModeAfter is how long a broker must be down (pod
// not-Ready) while stuck in maintenance mode before the operator clears the
// flag. Maintenance-mode brokers are excluded from the partition balancer's
// auto-decommission, so a never-cleared flag blocks decommission forever. The
// admin API offers no "who/why set this" signal to distinguish a stuck flag
// from a deliberate long maintenance window, so the threshold trades
// responsiveness against clearing a broker still expected to return. Tune via
// --clear-maintenance-mode-after.
const defaultClearMaintenanceModeAfter = 30 * time.Minute

// podNotReadyFor returns how long the pod's Ready condition has been False; a
// Ready pod reports zero, which never reaches the (always positive) threshold.
// A pod with no Ready condition (stuck Pending on a cordoned/lost node) counts
// as not-Ready since creation, not zero — that Pending case is exactly the
// stuck state we must be able to unblock.
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

// decideClearMaintenance is the guard for clearing a broker's maintenance mode:
// it must be draining, reported not-alive, and its pod not-Ready at least the
// threshold. not-alive rejects a broker merely mid-rolling-restart; the
// threshold rejects a transient blip.
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

// brokersByPodName indexes brokers by pod name (the first DNS label of the
// advertised internal RPC address), bucketing every broker sharing a key. Pod
// names are not globally unique across StretchCluster member clusters, so a
// multi-broker bucket is ambiguous and callers must not guess. This locates a
// persistently-down broker's pod (absent from the live-only brokerMap) so the
// pod's not-Ready duration can gate the clear. A bare IP is keyed as-is.
func brokersByPodName(brokers []rpadmin.Broker) map[string][]rpadmin.Broker {
	out := make(map[string][]rpadmin.Broker, len(brokers))
	for _, b := range brokers {
		host := hostOnly(b.InternalRPCAddress)
		// An empty address is not an identity and is never indexed.
		if host == "" {
			continue
		}
		// Full host covers a bare pod IP (flat-network mode) and the FQDN.
		out[host] = append(out[host], b)
		// For a hostname, also key by the first DNS label (the pod name).
		if net.ParseIP(host) == nil {
			podName := firstDNSLabel(host)
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

// Ghost brokers are the leaked-maintenance-flag bug (redpanda#1674): a pod
// loses its data directory and re-registers under a fresh node id at the same
// stable address; its preStop hook put the OLD id into maintenance but the
// replacement's postStart hook clears only the NEW id. The old id can never
// rejoin, so clearing its flag is safe without any down-duration threshold.
// Two rules detect this, differing only in how they prove supersession:
//
//   - rule A (ghostBrokersInMaintenance): a live broker owns the address under
//     a different node id. The broker list alone is proof; no dial needed.
//   - rule B (unclaimedDrainersInMaintenance -> clearGhostsByPodIdentity): no
//     live owner in the broker list (the leader-restart deadlock, redpanda#31057,
//     where the leaked flag itself blocks the successor's registration). The
//     broker list can't prove it, so rule B dials the pod at the address and
//     trusts its self-reported identity, re-verified via responderMatchesPod.
//
// ghostBrokersInMaintenance is rule A: dead, draining brokers sharing an
// advertised host:port with a live broker under a different node id. It keys on
// the full advertised host (the pod-name label can collide across StretchCluster
// members) plus RPC port. Bare-IP addresses are skipped: Kubernetes reuses pod
// IPs, so an unrelated pod could masquerade as a live successor; flat-network
// ghosts are recovered by rule B, which is IP-reuse-safe.
func ghostBrokersInMaintenance(brokers []rpadmin.Broker) []rpadmin.Broker {
	var ghosts []rpadmin.Broker
	for _, bucket := range ghostAddressBuckets(brokers, false) {
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
			// No live owner: rule A can't distinguish a ghost from a broker
			// that will return under its old id. Rule B handles this bucket.
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

// ghostAddressBuckets buckets brokers by advertised internal RPC host:port. An
// empty address is not an identity and is never bucketed. includeBareIP admits
// flat-network (bare-IP) brokers: rule A passes false (it clears without
// dialing, and reused pod IPs make an IP an unsafe identity); rule B passes
// true (it dials and verifies via responderMatchesPod, so it is reuse-safe, and
// is the only rule that can recover a flat-network ghost).
func ghostAddressBuckets(brokers []rpadmin.Broker, includeBareIP bool) map[string][]rpadmin.Broker {
	byAddr := make(map[string][]rpadmin.Broker, len(brokers))
	for _, b := range brokers {
		host := hostOnly(b.InternalRPCAddress)
		if host == "" {
			continue
		}
		if net.ParseIP(host) != nil && !includeBareIP {
			continue
		}
		key := fmt.Sprintf("%s:%d", host, b.InternalRPCPort)
		byAddr[key] = append(byAddr[key], b)
	}
	return byAddr
}

// unclaimedDrainersInMaintenance returns the rule-B candidates: dead, draining
// brokers whose address has NO live owner in the broker list. Each is only a
// candidate — indistinguishable here from a healthy broker mid-restart — so
// callers must confirm supersession via the pod's self-reported identity
// (clearGhostsByPodIdentity) and do nothing when that evidence is unavailable.
func unclaimedDrainersInMaintenance(brokers []rpadmin.Broker) []rpadmin.Broker {
	var drainers []rpadmin.Broker
	for _, bucket := range ghostAddressBuckets(brokers, true) {
		anyAlive := false
		for _, b := range bucket {
			if brokerIsAlive(b) {
				anyAlive = true
				break
			}
		}
		if anyAlive {
			// A live owner exists: this is rule A's bucket, not rule B's.
			continue
		}
		for _, b := range bucket {
			if !brokerIsAlive(b) && brokerInMaintenance(b) {
				drainers = append(drainers, b)
			}
		}
	}
	sort.Slice(drainers, func(i, j int) bool { return drainers[i].NodeID < drainers[j].NodeID })
	return drainers
}

// decideGhostClearByPodIdentity is the rule-B guard: clear an unclaimed
// drainer's maintenance flag iff the pod at its address self-reports a
// DIFFERENT node id. Redpanda never reuses an auto-assigned node id (chart- and
// operator-managed clusters always auto-assign), so a different id proves the
// drainer's identity is gone from the only disk it could boot from — it can
// never rejoin. The drainer's OWN id means an ordinary restart; leave it. A
// negative id is "no assigned id yet" — no evidence, so defer.
func decideGhostClearByPodIdentity(drainerNodeID, podNodeID int) (bool, string) {
	switch {
	case podNodeID < 0:
		return false, fmt.Sprintf("pod's broker reports no assigned node id (%d); cannot prove node %d is superseded", podNodeID, drainerNodeID)
	case podNodeID == drainerNodeID:
		return false, fmt.Sprintf("pod is running node %d itself (ordinary restart from persistent storage); it can rejoin", drainerNodeID)
	default:
		return true, fmt.Sprintf("pod at the drainer's address runs node id %d and node ids are never reused, so node %d can never rejoin", podNodeID, drainerNodeID)
	}
}

// podIdentityDialer returns an admin client scoped to exactly the one broker
// pod behind endpoint ("<pod-dns-name>:<admin-port>"), inheriting the cluster
// client's TLS and auth. The caller Closes the returned client.
type podIdentityDialer func(ctx context.Context, endpoint string) (*rpadmin.AdminAPI, error)

// lazyEndpoints resolves the per-pod admin endpoint list on first use.
// Rendering it requires the full chart render state, so a healthy cluster must
// never pay for it — only when a candidate needs an endpoint.
type lazyEndpoints func() []string

// memoizeEndpoints wraps an endpoint-rendering function so it runs at most once
// per reconcile pass, no matter how many candidates the steps examine.
func memoizeEndpoints(f func() []string) lazyEndpoints {
	var once sync.Once
	var endpoints []string
	return func() []string {
		once.Do(func() { endpoints = f() })
		return endpoints
	}
}

// firstDNSLabel returns the first DNS label of host ("redpanda-0" for
// "redpanda-0.redpanda.ns.svc.cluster.local."), which is the pod name for every
// address form the operator renders or the brokers advertise.
func firstDNSLabel(host string) string {
	return strings.SplitN(host, ".", 2)[0]
}

// podIdentityGhostConfig carries what rule B needs to ask a pod which broker
// identity it is running: the per-pod admin endpoints and a dialer. A nil
// config disables rule B; the broker-list-only rule A still runs.
type podIdentityGhostConfig struct {
	endpoints lazyEndpoints
	dial      podIdentityDialer
}

// podLocalIdentity dials the pod's own admin API and returns the node id its
// broker self-reports plus the internal RPC host it advertises, from the raw
// /v1/node_config response. An error means the identity is unreadable (broker
// booting, crashlooping, unreachable) and callers must defer. The raw response
// is used, not the typed NodeConfig, so a missing node_id decodes to the -1
// sentinel and not to 0, which is a real identity that would fabricate evidence.
func (c *podIdentityGhostConfig) podLocalIdentity(ctx context.Context, endpoint string) (nodeID int, advertisedHost string, err error) {
	scoped, err := c.dial(ctx, endpoint)
	if err != nil {
		return -1, "", err
	}
	defer scoped.Close()
	sctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
	defer cancel()
	raw, err := scoped.RawNodeConfig(sctx)
	if err != nil {
		return -1, "", err
	}
	nodeID, advertisedHost = parseNodeConfigIdentity(raw)
	return nodeID, advertisedHost, nil
}

// parseNodeConfigIdentity extracts (node_id, advertised internal RPC host)
// from a raw /v1/node_config response. A missing/non-numeric node_id maps to -1
// and a missing advertised address to "" — both treated downstream as
// "unverifiable, defer".
func parseNodeConfigIdentity(raw rpadmin.RawNodeConfig) (nodeID int, advertisedHost string) {
	nodeID = -1
	// JSON numbers decode into any as float64.
	if f, ok := raw["node_id"].(float64); ok {
		nodeID = int(f)
	}
	if api, ok := raw["advertised_rpc_api"].(map[string]any); ok {
		if addr, ok := api["address"].(string); ok {
			advertisedHost = addr
		}
	}
	return nodeID, advertisedHost
}

// responderMatchesPod reports whether the broker that answered a pod-local dial
// is really the pod's broker, using the responder's self-advertised RPC address
// as the tie. The dial targets a DNS name and a stale record or reused IP can
// route it to a different broker (the admin listener may have no TLS to check),
// whose answer would then fabricate evidence. DNS responders must advertise the
// pod's name as their first label; flat-network responders must advertise the
// pod's current IP. Anything unverifiable is a mismatch.
func responderMatchesPod(advertisedHost, podName, podIP string) (bool, string) {
	host := hostOnly(advertisedHost)
	switch {
	case host == "":
		return false, "responder reported no advertised RPC address; cannot attribute the answer to the pod"
	case net.ParseIP(host) != nil:
		if podIP != "" && host == podIP {
			return true, "responder advertises the pod's current IP"
		}
		return false, fmt.Sprintf("responder advertises IP %q which is not the pod's current IP %q", host, podIP)
	case firstDNSLabel(host) == podName:
		return true, "responder advertises the pod's own DNS name"
	default:
		return false, fmt.Sprintf("responder advertises %q, which does not belong to pod %q", host, podName)
	}
}

// brokerIsAlive reports the cluster's liveness view of the broker, defaulting to
// alive when the field is absent (so a missing value never triggers a clear).
func brokerIsAlive(b rpadmin.Broker) bool {
	return ptr.Deref(b.IsAlive, true)
}

// isMaintenanceAlreadyClearedErr reports whether a DisableMaintenanceMode error
// means nothing is left to clear: 400 (not in maintenance) or 404 (broker
// gone). Both are the expected race between the chart's postStart hook and the
// operator clearing the same ghost; the loser must not abort the pass.
func isMaintenanceAlreadyClearedErr(err error) bool {
	var httpErr *rpadmin.HTTPResponseError
	if !errors.As(err, &httpErr) || httpErr.Response == nil {
		return false
	}
	return httpErr.Response.StatusCode == http.StatusBadRequest || httpErr.Response.StatusCode == http.StatusNotFound
}

// maintenanceWorkPending reports whether the broker list shows anything to act
// on: a ghost, an unclaimed drainer, or a not-alive broker still in maintenance
// (a stuck-path candidate). A healthy cluster shows none, so the reconcile
// returns before rendering endpoints or dialing — preserving quiescence.
func maintenanceWorkPending(brokers []rpadmin.Broker) bool {
	if len(ghostBrokersInMaintenance(brokers)) > 0 || len(unclaimedDrainersInMaintenance(brokers)) > 0 {
		return true
	}
	for _, b := range brokers {
		if brokerInMaintenance(b) && !brokerIsAlive(b) {
			return true
		}
	}
	return false
}

// clearStuckMaintenanceMode clears maintenance mode on three classes of broker,
// and is shared by the single-cluster and StretchCluster reconcilers:
//   - rule-A ghosts (ghostBrokersInMaintenance), cleared immediately;
//   - rule-B unclaimed drainers (clearGhostsByPodIdentity), cleared immediately
//     but requiring podIdentity — skipped when it is nil;
//   - stuck brokers: in maintenance, not-alive, pod not-Ready past the threshold.
//
// It uses the full broker list (which includes down brokers) so a stuck broker
// can be matched to its pod. A pod name matching more than one broker is skipped
// rather than guessed.
func clearStuckMaintenanceMode(ctx context.Context, admin *rpadmin.AdminAPI, pods []*lifecycle.MulticlusterPod, threshold time.Duration, podIdentity *podIdentityGhostConfig, logger logr.Logger) error {
	// Cheap detection read to preserve quiescence: a healthy cluster returns
	// here without rendering endpoints or dialing. This read is unpinned, but it
	// only gates WHETHER we pin the leader, never a write — a stale view that
	// hides work defers to a later pass, and one that invents work is caught by
	// the leader-pinned re-read below that keys every actual clear.
	//
	// A failed read defers the pass rather than aborting it. This step is FIRST
	// in both reconcile chains, and an error from it aborts every later step for
	// that pass — including reconcileDecommission and reconcileClusterConfig. The
	// cluster admin client resolves through the internal service
	// (PublishNotReadyAddresses), so it can dial a broker whose pod is already
	// gone: mid-roll, mid-decommission, or a BrokerPool being deleted. Those are
	// exactly the states the later steps exist to resolve, so propagating the
	// error deadlocks — the pass that would remove the unreachable broker never
	// runs, which keeps it unreachable. Deferring costs at most one pass of
	// maintenance-mode clearing, and no clear is possible without a readable
	// broker list anyway.
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		logger.Info("broker list unavailable; deferring maintenance-mode reconcile this pass", "error", err.Error())
		return nil
	}
	if !maintenanceWorkPending(brokers) {
		return nil
	}

	// The broker list and the writes must be served by the controller leader
	// (leaderAdmin), never an arbitrary responder: the cluster client resolves
	// through the internal service (PublishNotReadyAddresses), so a NotReady
	// stale-member ghost with a frozen members table could answer, and even a
	// Ready follower can lag committed membership. If the leader can't be pinned
	// (mid-election, peer outage, no endpoints) we defer — a clear can't happen
	// without a functioning controller anyway, and clearing a live broker's flag
	// from a stale view would drop its drain protection.
	//
	// (podIdentity is nil only in unit tests, which use the passed-in client.)
	rw := admin
	if podIdentity != nil {
		endpoints := podIdentity.endpoints()
		if len(endpoints) == 0 {
			logger.Info("no per-pod admin endpoints available; deferring maintenance-mode reconcile this pass")
			return nil
		}
		authority, authorityPod, ok := leaderAdmin(ctx, pods, endpoints, podIdentity.dial, time.Now(), logger)
		if !ok {
			logger.Info("controller leader unavailable to serve authoritative maintenance reads; deferring this pass")
			return nil
		}
		defer authority.Close()
		logger.V(log.DebugLevel).Info("maintenance-mode pinned to controller leader", "pod", authorityPod)
		fresh, err := authority.Brokers(ctx)
		if err != nil {
			logger.Info("leader broker re-read failed; deferring maintenance-mode reconcile this pass", "error", err.Error())
			return nil
		}
		rw, brokers = authority, fresh
	}

	// Rule-A ghosts are cleared unconditionally here: their pod is Ready again
	// (running the successor), so the threshold path below never fires for them.
	podsByName := make(map[string][]*lifecycle.MulticlusterPod, len(pods))
	podsByIP := make(map[string][]*lifecycle.MulticlusterPod, len(pods))
	for _, pod := range pods {
		podsByName[pod.GetName()] = append(podsByName[pod.GetName()], pod)
		if ip := pod.Status.PodIP; ip != "" {
			podsByIP[ip] = append(podsByIP[ip], pod)
		}
	}
	for _, ghost := range ghostBrokersInMaintenance(brokers) {
		host := hostOnly(ghost.InternalRPCAddress)
		// Only >1 same-named observed pods is ambiguous and refused: in
		// StretchCluster mesh/flat mode identically-named pools across member
		// clusters can bucket together, so a broker in one could masquerade as
		// another's successor. A count of 0 is not ambiguity and must not block:
		// the successor pod may be in an unobservable member cluster (peer
		// outage), and the clear targets a node id, not a pod — attribution just
		// falls back to the empty cluster label.
		matched := podsByName[firstDNSLabel(host)]
		if len(matched) > 1 {
			logger.Info("not clearing ghost maintenance mode: address maps to more than one observed pod (cross-cluster ambiguous)",
				"nodeID", ghost.NodeID, "address", ghost.InternalRPCAddress, "matchingPods", len(matched))
			continue
		}
		clusterName := ""
		if len(matched) == 1 {
			clusterName = matched[0].GetCanonicalClusterName()
		}
		logger.Info("clearing maintenance mode on ghost broker superseded by a live broker at the same address",
			"nodeID", ghost.NodeID, "address", ghost.InternalRPCAddress, "cluster", clusterName)
		if err := rw.DisableMaintenanceMode(ctx, ghost.NodeID, true); err != nil {
			// Losing the postStart-hook race means the work is already done.
			if isMaintenanceAlreadyClearedErr(err) {
				logger.Info("ghost broker maintenance mode was already cleared or the broker is gone",
					"nodeID", ghost.NodeID, "reason", err.Error())
				continue
			}
			return errors.Wrapf(err, "disabling maintenance mode for ghost broker %d", ghost.NodeID)
		}
		observability.MaintenanceModeGhostCleared.WithLabelValues(clusterName).Inc()
	}

	if podIdentity != nil {
		if err := clearGhostsByPodIdentity(ctx, rw, brokers, podsByName, podsByIP, podIdentity, logger); err != nil {
			return err
		}
	}

	byPod := brokersByPodName(brokers)
	now := time.Now()
	for _, pod := range pods {
		notReadyFor := podNotReadyFor(pod.Pod, now)
		if notReadyFor < threshold {
			continue
		}
		// Match by NAME only, never PodIP: reused pod IPs would let a dead
		// broker's stale advertised IP match an unrelated pod and clear it on
		// that pod's not-Ready clock. Flat-network brokers (bare IPs, not
		// name-matchable here) are recovered by rule B instead, which is
		// IP-reuse-safe via responderMatchesPod.
		candidates, ok := byPod[pod.GetName()]
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
		if err := rw.DisableMaintenanceMode(ctx, b.NodeID, true); err != nil {
			return errors.Wrapf(err, "disabling maintenance mode for broker %d", b.NodeID)
		}
		observability.MaintenanceModeCleared.WithLabelValues(pod.GetCanonicalClusterName()).Inc()
	}
	return nil
}

// clearGhostsByPodIdentity is rule B: for each unclaimed drainer, it asks the
// pod owning the drainer's address which identity it is running; a different
// node id proves the drainer can never rejoin. Every ambiguity — unreadable
// identity, unresolvable or ambiguous endpoint, or an answer not attributable
// to the pod (responderMatchesPod) — defers rather than guesses. The endpoint
// list is rendered only once a drainer exists, so a healthy cluster never pays.
func clearGhostsByPodIdentity(ctx context.Context, admin *rpadmin.AdminAPI, brokers []rpadmin.Broker, podsByName, podsByIP map[string][]*lifecycle.MulticlusterPod, podIdentity *podIdentityGhostConfig, logger logr.Logger) error {
	drainers := unclaimedDrainersInMaintenance(brokers)
	if len(drainers) == 0 {
		return nil
	}
	endpoints := podIdentity.endpoints()
	if len(endpoints) == 0 {
		// Drainers stay in maintenance until rendering recovers or the stuck
		// path's threshold fires; logged loudly since it costs coverage.
		logger.Info("no per-pod admin endpoints available; skipping pod-identity ghost checks this pass",
			"unclaimedDrainers", len(drainers))
		return nil
	}
	for _, drainer := range drainers {
		host := hostOnly(drainer.InternalRPCAddress)
		// Resolve the pod holding this address: by pod name for a DNS drainer,
		// by PodIP for a flat-network one. This only finds the candidate; the
		// dial + responderMatchesPod below re-verifies it, catching a reused IP.
		var matched []*lifecycle.MulticlusterPod
		if net.ParseIP(host) != nil {
			matched = podsByIP[host]
		} else {
			matched = podsByName[firstDNSLabel(host)]
		}
		if len(matched) != 1 {
			// No single pod whose identity can vouch for this address (missing
			// pod, or a cross-member StretchCluster collision).
			logger.V(log.DebugLevel).Info("unclaimed drainer's address does not resolve to exactly one pod; skipping pod-identity ghost check",
				"nodeID", drainer.NodeID, "address", drainer.InternalRPCAddress, "matchingPods", len(matched))
			continue
		}
		pod := matched[0]
		podName := pod.GetName()
		endpoint, ambiguous := podAdminEndpoint(endpoints, podName)
		if ambiguous {
			logger.Info("pod name maps to more than one admin endpoint across member clusters; skipping pod-identity ghost check to avoid reading the wrong broker",
				"nodeID", drainer.NodeID, "pod", podName)
			continue
		}
		if endpoint == "" {
			logger.Info("no admin endpoint resolved for unclaimed drainer's pod; skipping pod-identity ghost check",
				"nodeID", drainer.NodeID, "pod", podName)
			continue
		}
		podNodeID, advertisedHost, err := podIdentity.podLocalIdentity(ctx, endpoint)
		if err != nil {
			// Identity unreadable (booting/crashlooping); flag stays put until
			// it becomes readable or the stuck path's threshold fires.
			logger.Info("could not read pod-local broker identity; deferring ghost maintenance clear",
				"nodeID", drainer.NodeID, "pod", podName, "endpoint", endpoint, "error", err.Error())
			continue
		}
		if matches, why := responderMatchesPod(advertisedHost, podName, pod.Status.PodIP); !matches {
			// The answer is not provably the pod's broker; not usable as evidence.
			logger.Info("pod-local identity answer not attributable to the pod; deferring ghost maintenance clear",
				"nodeID", drainer.NodeID, "pod", podName, "endpoint", endpoint, "reason", why)
			continue
		}
		clearDrainer, reason := decideGhostClearByPodIdentity(drainer.NodeID, podNodeID)
		if !clearDrainer {
			logger.Info("not clearing maintenance mode on unclaimed drainer", "nodeID", drainer.NodeID, "pod", podName, "reason", reason)
			continue
		}
		logger.Info("clearing maintenance mode on ghost broker superseded by its pod's local broker identity",
			"nodeID", drainer.NodeID, "address", drainer.InternalRPCAddress, "pod", podName,
			"cluster", pod.GetCanonicalClusterName(), "podNodeID", podNodeID, "reason", reason)
		if err := admin.DisableMaintenanceMode(ctx, drainer.NodeID, true); err != nil {
			// Losing the postStart-hook race means the work is already done.
			if isMaintenanceAlreadyClearedErr(err) {
				logger.Info("ghost broker maintenance mode was already cleared or the broker is gone",
					"nodeID", drainer.NodeID, "reason", err.Error())
				continue
			}
			return errors.Wrapf(err, "disabling maintenance mode for ghost broker %d", drainer.NodeID)
		}
		observability.MaintenanceModeGhostCleared.WithLabelValues(pod.GetCanonicalClusterName()).Inc()
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
	err := clearStuckMaintenanceMode(ctx, state.admin, state.pools.ExistingPods(), r.maintenanceModeClearThreshold(), &podIdentityGhostConfig{
		endpoints: state.podEndpoints,
		dial:      r.podAdminDialer(state),
	}, logger)
	return ctrl.Result{}, err
}

// podAdminDialer returns a podIdentityDialer for one StretchCluster broker pod,
// reusing the stale-disk wipe's per-pod client construction
// (RedpandaAdminClientForStretchPod derives TLS/auth from the pool spec).
func (r *MulticlusterReconciler) podAdminDialer(state *stretchClusterReconciliationState) podIdentityDialer {
	return func(ctx context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		return r.ClientFactory.RedpandaAdminClientForStretchPod(ctx, state.cluster.StretchCluster, endpoint)
	}
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
	err := clearStuckMaintenanceMode(ctx, state.admin, state.pools.ExistingPods(), r.maintenanceModeClearThreshold(), &podIdentityGhostConfig{
		endpoints: state.podEndpoints,
		dial:      r.podAdminDialer(state),
	}, logger)
	return ctrl.Result{}, err
}

// podAdminDialer returns a podIdentityDialer for one single-cluster broker pod:
// the cluster-wide admin client scoped to the pod's endpoint via ForHost, the
// same pattern the per-broker restart probes use.
func (r *RedpandaReconciler) podAdminDialer(state *clusterReconciliationState) podIdentityDialer {
	return func(_ context.Context, endpoint string) (*rpadmin.AdminAPI, error) {
		return state.admin.ForHost(endpoint)
	}
}

func (r *RedpandaReconciler) maintenanceModeClearThreshold() time.Duration {
	if r.MaintenanceModeClearThreshold > 0 {
		return r.MaintenanceModeClearThreshold
	}
	return defaultClearMaintenanceModeAfter
}
