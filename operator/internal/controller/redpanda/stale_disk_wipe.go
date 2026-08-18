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
	"encoding/json"
	"fmt"
	"net"
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

	entv1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

// defaultStaleDiskWipeNotReadyThreshold is how long a broker pod must stay
// not-Ready before the wipe may destroy its disk. It must exceed normal startup
// and recovery so a transiently unready but healthy broker is never wiped.
const defaultStaleDiskWipeNotReadyThreshold = 5 * time.Minute

// identityCollision reports whether a sick broker's self-reported (node_id,
// uuid) collides with the cluster-authoritative node_id->uuid map. A collision
// means the disk holds a retired identity (the node_id was decommissioned, or a
// replacement took it under a different uuid) and the broker cannot rejoin.
//
// Because a wipe is irreversible, it reports NO collision whenever it cannot
// confirm one: empty cluster map, empty self uuid, or a member with no known
// cluster-side uuid.
func identityCollision(clusterUUIDs map[int]string, selfNodeID int, selfUUID string) (bool, string) {
	if len(clusterUUIDs) == 0 {
		return false, "cluster member list unavailable; cannot confirm collision"
	}
	if selfUUID == "" {
		return false, "sick broker self uuid unavailable; cannot confirm collision"
	}

	clusterUUID, present := clusterUUIDs[selfNodeID]
	switch {
	case !present:
		return true, fmt.Sprintf("node_id %d not present in cluster (decommissioned/removed); disk identity %s is retired", selfNodeID, selfUUID)
	case clusterUUID == "":
		// A current member with no known uuid: cannot confirm staleness, so
		// don't risk wiping a live member's disk.
		return false, fmt.Sprintf("node_id %d is a member but its cluster uuid is unknown; cannot confirm collision", selfNodeID)
	case clusterUUID != selfUUID:
		return true, fmt.Sprintf("node_id %d maps to uuid %s in cluster but disk holds %s (superseded)", selfNodeID, clusterUUID, selfUUID)
	default:
		return false, fmt.Sprintf("node_id %d uuid %s matches cluster; broker is a legitimate member", selfNodeID, selfUUID)
	}
}

// podAdminEndpoint returns the admin-API endpoint for the named pod from the
// full endpoint list; every renderer emits endpoints whose first DNS label is
// the pod name. Returns ("", false) if none match.
//
// It reports ambiguous=true when more than one endpoint matches: identically-
// named BrokerPools across StretchCluster members yield cluster-unqualified
// endpoints that collide on the same first label. The caller must defer rather
// than risk reading one broker and wiping another's disk.
func podAdminEndpoint(endpoints []string, podName string) (endpoint string, ambiguous bool) {
	for _, ep := range endpoints {
		host := ep
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if firstDNSLabel(host) == podName {
			if endpoint != "" {
				return "", true // a second match: ambiguous
			}
			endpoint = ep
		}
	}
	return endpoint, false
}

// staleDiskWipeConfirmationInterval is the minimum age of a retired-identity
// observation before a re-observation may authorize the wipe (see
// wipeDebounce). It outlives the transient membership windows in which an
// identity can look retired without being so (e.g. an assigned id whose
// registration hasn't yet reached the queried peer). Those resolve in seconds;
// a genuine bad_rejoin is permanent, so the delay only defers real wipes by
// about one requeue.
const staleDiskWipeConfirmationInterval = 30 * time.Second

// wipeDebounce requires the wipe to observe the SAME retired identity on a pod
// across two passes at least staleDiskWipeConfirmationInterval apart before
// acting: a transient false-collision changes between observations while a real
// bad_rejoin identity is immutable. In-memory only; an operator restart just
// restarts the window, which is fail-safe (delays, never authorizes).
type wipeDebounce struct {
	mu   sync.Mutex
	seen map[string]wipeObservation
}

type wipeObservation struct {
	nodeID    int
	uuid      string
	firstSeen time.Time
}

// confirm records the observation and reports whether the SAME (nodeID, uuid)
// was first seen at least `interval` ago.
//
// A change of (nodeID, uuid) restarts the window, but a long time gap between
// unchanged sightings does NOT: recovery is detected by content, not time, and
// inter-pass timing is unbounded (error backoff), so a time-based restart would
// keep a slow-but-real bad_rejoin from ever converging. A recovered identity is
// instead dropped by the caller's forget-on-member-read.
func (d *wipeDebounce) confirm(key string, nodeID int, uuid string, now time.Time, interval time.Duration) (bool, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = map[string]wipeObservation{}
	}
	prev, ok := d.seen[key]
	if !ok || prev.nodeID != nodeID || prev.uuid != uuid {
		d.seen[key] = wipeObservation{nodeID: nodeID, uuid: uuid, firstSeen: now}
		return false, fmt.Sprintf("first observation of retired identity (node %d); deferring for re-confirmation in >= %s", nodeID, interval)
	}
	if age := now.Sub(prev.firstSeen); age < interval {
		return false, fmt.Sprintf("retired identity re-observed only %s after first sighting (< %s); deferring", age, interval)
	}
	return true, fmt.Sprintf("retired identity stable since %s ago", now.Sub(prev.firstSeen))
}

// forget drops the pod's observation after a wipe (or when the pod is gone)
// so a future incarnation starts a fresh confirmation window.
func (d *wipeDebounce) forget(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.seen, key)
}

// wipeDebounceKey identifies a pod across passes for wipe confirmation.
func wipeDebounceKey(pod *lifecycle.MulticlusterPod) string {
	return pod.GetCanonicalClusterName() + "/" + pod.GetNamespace() + "/" + pod.GetName()
}

// stagedOverrideUUIDs returns the uuids named by the cluster's
// node_id_overrides config. Such an entry is a data-preserving identity
// migration: until the broker restarts and adopts its new identity it still
// boots its OLD uuid and can look exactly like a bad_rejoin, so wiping it would
// destroy the data the override was meant to preserve.
//
// Matching is per-identity by uuid so a stale entry left after a completed
// migration never blocks an unrelated bad_rejoin. All uuid-shaped strings under
// the key are collected (both current_uuid and new_uuid) to stay robust to
// field-name drift; a mid-migration broker's on-disk uuid is current_uuid,
// which is always present.
func stagedOverrideUUIDs(rawNode []byte) map[string]struct{} {
	out := map[string]struct{}{}
	if len(rawNode) == 0 {
		return out
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(rawNode, &node); err != nil {
		return out
	}
	raw, ok := node["node_id_overrides"]
	if !ok {
		return out
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return out
	}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			// Store the canonical form: the CR is free-form JSON so an operator
			// may spell the uuid in any redpanda-accepted way, but the on-disk
			// uuid the wipe compares against is lowercase-hyphenated. A raw match
			// would miss a valid override and wipe the migration target.
			if c := canonicalUUID(t); c != "" {
				out[c] = struct{}{}
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(decoded)
	return out
}

// looksLikeUUID matches redpanda's dashed-hex node uuid shape (see parseBootUUID).
func looksLikeUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// canonicalUUID normalizes any redpanda-accepted uuid spelling (uppercase,
// brace-wrapped, hyphenated or dash-less) to its 32-char lowercase hex form, or
// "" if s is not a 128-bit hex uuid. Both sides of the override-guard match run
// through it so equal uuids compare equal regardless of spelling.
func canonicalUUID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	var b strings.Builder
	b.Grow(32)
	for _, r := range s {
		switch {
		case r == '-':
			continue
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			b.WriteRune(r)
		case r >= 'A' && r <= 'F':
			b.WriteRune(r + ('a' - 'A'))
		default:
			return "" // non-hex character: not a uuid
		}
	}
	if b.Len() != 32 {
		return ""
	}
	return b.String()
}

// unwipeableDataDir returns a non-empty reason when deleting the pod and its
// StatefulSet-managed PVCs would NOT reset the on-disk identity (the datadir
// survives both), else "". A hostPath datadir survives pod deletion, and a PVC
// whose claim name isn't the StatefulSet pattern is not deleted by
// DeletePVCsForPod; wiping either loops forever, so the caller must defer.
func unwipeableDataDir(pod *lifecycle.MulticlusterPod) string {
	for _, vol := range pod.Spec.Volumes {
		if vol.Name != "datadir" {
			continue
		}
		switch {
		case vol.EmptyDir != nil:
			return ""
		case vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == vol.Name+"-"+pod.GetName():
			return ""
		case vol.PersistentVolumeClaim != nil:
			return fmt.Sprintf("datadir PVC %q is not a StatefulSet-managed claim and will not be deleted", vol.PersistentVolumeClaim.ClaimName)
		default:
			return "datadir is a hostPath (or other node-local) volume that survives pod deletion"
		}
	}
	return "pod has no datadir volume to reset"
}

// podDeleter is the subset of the lifecycle client the wipe needs to destroy a
// broker's stale state; both ResourceClient instantiations satisfy it.
type podDeleter interface {
	DeletePVCsForPod(ctx context.Context, pod *lifecycle.MulticlusterPod) error
	DeletePod(ctx context.Context, pod *lifecycle.MulticlusterPod) error
	// GetLivePod returns the live pod (nil if gone), so the wipe can confirm it
	// is still the same instance (UID) it evaluated before destroying storage.
	GetLivePod(ctx context.Context, pod *lifecycle.MulticlusterPod) (*corev1.Pod, error)
}

// podLogsReader reads one container's logs via the Kubernetes API, which is
// kubelet-attributed to exactly that pod and so cannot be misdirected by stale
// DNS or pod-IP reuse (unlike a dial). Satisfied by ResourceClient.GetPodLogs;
// nil disables the log-evidence fallback below.
type podLogsReader func(ctx context.Context, pod *lifecycle.MulticlusterPod, opts *corev1.PodLogOptions) (string, error)

// The log-evidence fallback exists because a bad_rejoin broker cannot answer
// its admin API at all: Redpanda binds it only after cluster discovery, and a
// bad_rejoin broker loops inside discovery forever. Its logs still carry the
// on-disk identity (bootUUIDLogPrefix) and the controller's refusal verdict
// (badRejoinLogToken). Anchor drift across Redpanda versions degrades to
// "no evidence -> defer", never to a wipe.
const (
	// bootUUIDLogPrefix precedes the on-disk uuid in the startup logs.
	bootUUIDLogPrefix = "Loaded existing UUID for node: "
	// badRejoinLogToken appears in every refused join retry of a broker whose
	// (id, uuid) was decommissioned.
	badRejoinLogToken = "bad_rejoin"
	// badRejoinFreshness bounds how recent a bad_rejoin retry must be to count
	// as evidence; the retry cadence (~5s) means a live loop always has lines
	// in this window while a resolved crashloop does not. Filtered server-side
	// via SinceSeconds.
	badRejoinFreshness = 2 * time.Minute
	// badRejoinLogTail bounds the identity read. The once-per-start boot line
	// stays within this window for hours; beyond that, defer rather than read
	// unbounded logs.
	badRejoinLogTail = int64(10000)
	// overrideUUIDLogToken / overrideNodeIDLogToken mark a broker applying a
	// node_id_overrides entry at boot. Redpanda logs the pre-override identity
	// before the override line, so an override line appearing AFTER the parsed
	// boot-identity line means the on-disk identity is mid-migration: never wipe.
	overrideUUIDLogToken   = "Overriding UUID for node: "
	overrideNodeIDLogToken = "Overriding node ID: "
)

// parseBootUUID returns the uuid from the NEWEST boot-identity line in the
// log content, or "" when absent/unparseable (callers defer).
func parseBootUUID(logs string) string {
	idx := strings.LastIndex(logs, bootUUIDLogPrefix)
	if idx < 0 {
		return ""
	}
	rest := logs[idx+len(bootUUIDLogPrefix):]
	if end := strings.IndexAny(rest, " \t\r\n"); end >= 0 {
		rest = rest[:end]
	}
	// Anything but dashed hex means the line format drifted; treat as
	// unparseable rather than feed garbage into the membership cross-check.
	if !looksLikeUUID(rest) {
		return ""
	}
	return rest
}

// bootIdentityOverridden reports whether an override line appears AFTER the
// newest boot-identity line, meaning the broker is applying a node_id_overrides
// entry this boot. The uuid parseBootUUID returned is then the pre-override
// identity being migrated (data preserved), not a retired ghost's; defer.
func bootIdentityOverridden(logs string) bool {
	base := strings.LastIndex(logs, bootUUIDLogPrefix)
	if base < 0 {
		return false
	}
	rest := logs[base:]
	return strings.Contains(rest, overrideUUIDLogToken) || strings.Contains(rest, overrideNodeIDLogToken)
}

// readBadRejoinEvidence returns the broker's on-disk uuid IFF its current
// redpanda-container logs also show a live bad_rejoin retry loop. Any
// unreadable, stale, or unparseable state returns ok=false with a reason
// (callers defer). Both reads target the current container instance so identity
// and refusal verdict describe the same boot.
func readBadRejoinEvidence(ctx context.Context, read podLogsReader, pod *lifecycle.MulticlusterPod) (uuid string, reason string, ok bool) {
	container := "redpanda"

	// Freshness read: a live loop always has retries inside the window.
	fresh, err := read(ctx, pod, &corev1.PodLogOptions{
		Container:    container,
		SinceSeconds: ptr.To(int64(badRejoinFreshness / time.Second)),
		TailLines:    ptr.To(int64(400)),
	})
	if err != nil {
		return "", fmt.Sprintf("pod logs unreadable: %v", err), false
	}
	if !strings.Contains(fresh, badRejoinLogToken) {
		return "", "no recent bad_rejoin evidence in pod logs", false
	}

	// Identity read: boot line from the same container instance.
	full, err := read(ctx, pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: ptr.To(badRejoinLogTail),
	})
	if err != nil {
		return "", fmt.Sprintf("pod logs unreadable: %v", err), false
	}
	uuid = parseBootUUID(full)
	if uuid == "" {
		return "", "pod logs show bad_rejoin but no parseable boot identity line", false
	}
	if bootIdentityOverridden(full) {
		// Data-preserving node_id_overrides recovery in progress; the parsed
		// uuid is the old identity being migrated, not a retired ghost's.
		return "", "pod logs show a node_id_overrides identity migration in progress; deferring", false
	}
	return uuid, "", true
}

// leaderAdmin returns an admin client pinned to the controller leader's pod for
// a pass's authoritative cluster reads (membership, broker uuids, health).
//
// It must be the leader: a non-leader serves /v1/brokers from its local
// members_table, a Raft follower state that can lag the leader, so a Ready-but-
// lagging follower can report a just-registered live member as absent and the
// collision check would wipe a healthy broker's disk. Two followers can lag
// identically, so cross-checking another does not help. The leader writes
// committed membership and cannot lag its own commits.
//
// It bootstraps via any reachable Ready pod (authorityAdmin), asks it for the
// leader id and address, then dials the leader (reusing the bootstrap if it is
// the leader). ok=false (no reachable Ready pod, no elected leader, or an
// unresolvable/unreachable/ambiguous leader) means callers must DEFER all
// destructive work — deferring is the safe outcome and the work is never
// time-critical.
func leaderAdmin(ctx context.Context, pods []*lifecycle.MulticlusterPod, endpoints []string, dial podIdentityDialer, now time.Time, logger logr.Logger) (*rpadmin.AdminAPI, string, bool) {
	boot, bootPod, ok := authorityAdmin(ctx, pods, endpoints, dial, now, logger)
	if !ok {
		return nil, "", false
	}

	leaderID, err := getLeaderID(ctx, boot)
	if err != nil || leaderID < 0 {
		boot.Close()
		logger.V(log.DebugLevel).Info("no elected controller leader (or unreadable); deferring authoritative reads this pass", "bootstrap", bootPod, "leaderID", leaderID, "error", errString(err))
		return nil, "", false
	}

	brokers, err := func() ([]rpadmin.Broker, error) {
		bctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
		defer cancel()
		return boot.Brokers(bctx)
	}()
	if err != nil {
		boot.Close()
		logger.V(log.DebugLevel).Info("could not read brokers to resolve the controller leader's address; deferring", "bootstrap", bootPod, "error", err.Error())
		return nil, "", false
	}
	leaderPod := ""
	for _, b := range brokers {
		if b.NodeID == leaderID {
			leaderPod = leaderPodName(hostOnly(b.InternalRPCAddress), pods)
			break
		}
	}
	if leaderPod == "" {
		boot.Close()
		logger.V(log.DebugLevel).Info("controller leader id not found in the broker list (or its address unresolvable to a pod); deferring", "bootstrap", bootPod, "leaderID", leaderID)
		return nil, "", false
	}
	if leaderPod == bootPod {
		// Bootstrap pod is the leader; reuse it (already probed and it named
		// itself leader via GetLeaderID, so it self-confirms).
		return boot, bootPod, true
	}
	boot.Close()

	endpoint, ambiguous := podAdminEndpoint(endpoints, leaderPod)
	if ambiguous || endpoint == "" {
		logger.V(log.DebugLevel).Info("controller leader's pod endpoint unresolved/ambiguous; deferring", "leaderPod", leaderPod, "ambiguous", ambiguous)
		return nil, "", false
	}
	admin, err := dial(ctx, endpoint)
	if err != nil {
		logger.V(log.DebugLevel).Info("could not dial the controller leader's pod; deferring", "leaderPod", leaderPod, "error", err.Error())
		return nil, "", false
	}
	if _, err := fetchHealthOverview(ctx, admin); err != nil {
		admin.Close()
		logger.V(log.DebugLevel).Info("controller leader's admin API did not respond to a probe; deferring", "leaderPod", leaderPod, "endpoint", endpoint, "error", err.Error())
		return nil, "", false
	}
	// Confirm the dialed node agrees it is the leader: the id/address came from
	// the bootstrap follower's view, which can name a stale ex-leader after a
	// leadership change. A stepped-down node reports the new leader here (a
	// mismatch -> defer). A partitioned node that still believes it leads passes
	// this but sees the majority down, so the downstream health gate rejects it.
	if lid, err := getLeaderID(ctx, admin); err != nil || lid != leaderID {
		admin.Close()
		logger.V(log.DebugLevel).Info("dialed node does not confirm it is the controller leader (leadership changed since bootstrap); deferring",
			"leaderPod", leaderPod, "expectedLeaderID", leaderID, "reportedLeaderID", lid, "error", errString(err))
		return nil, "", false
	}
	return admin, leaderPod, true
}

// leaderPodName maps the leader's advertised host to a pod name: a DNS
// address's first label is the pod name, a bare IP is matched by PodIP. "" if
// unresolved.
func leaderPodName(host string, pods []*lifecycle.MulticlusterPod) string {
	if net.ParseIP(host) != nil {
		for _, p := range pods {
			if p.Status.PodIP == host {
				return p.GetName()
			}
		}
		return ""
	}
	return firstDNSLabel(host)
}

// getLeaderID returns the controller (admin API) leader's broker id, or -1 when
// there is no elected leader (mid-election). Bounded by brokerFetchTimeout.
func getLeaderID(ctx context.Context, admin *rpadmin.AdminAPI) (int, error) {
	lctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
	defer cancel()
	id, err := admin.GetLeaderID(lctx)
	if err != nil {
		return -1, err
	}
	if id == nil {
		return -1, nil // rpadmin returns nil for leader_id == -1 (no leader)
	}
	return *id, nil
}

// hostOnly strips a :port suffix from an advertised address, leaving the host.
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// errString renders an error for structured logging, tolerating nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// authorityAdmin finds some reachable Ready, not-being-deleted broker pod to
// bootstrap leaderAdmin (it is not itself authoritative for membership).
//
// Pods are tried in name order for determinism; the first whose endpoint
// resolves unambiguously and whose admin API answers a bounded probe wins. The
// probe is load-bearing: the dialers only construct a client (no request), so
// without it an unreachable Ready pod would be pinned forever. ok=false means
// no reachable Ready pod this pass.
func authorityAdmin(ctx context.Context, pods []*lifecycle.MulticlusterPod, endpoints []string, dial podIdentityDialer, now time.Time, logger logr.Logger) (*rpadmin.AdminAPI, string, bool) {
	sorted := make([]*lifecycle.MulticlusterPod, len(pods))
	copy(sorted, pods)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].GetName() < sorted[j].GetName() })
	for _, pod := range sorted {
		if pod.GetDeletionTimestamp() != nil || podNotReadyFor(pod.Pod, now) != 0 {
			continue
		}
		endpoint, ambiguous := podAdminEndpoint(endpoints, pod.GetName())
		if ambiguous || endpoint == "" {
			continue
		}
		admin, err := dial(ctx, endpoint)
		if err != nil {
			logger.V(log.DebugLevel).Info("ready pod did not answer a bootstrap dial; trying next", "pod", pod.GetName(), "error", err.Error())
			continue
		}
		if _, err := fetchHealthOverview(ctx, admin); err != nil {
			// Constructed but unreachable: rotate to the next Ready pod.
			admin.Close()
			logger.V(log.DebugLevel).Info("ready pod's admin API did not respond to a bootstrap probe; trying next", "pod", pod.GetName(), "endpoint", endpoint, "error", err.Error())
			continue
		}
		return admin, pod.GetName(), true
	}
	return nil, "", false
}

// staleDiskWipeParams carries the shared wipe core's dependencies; both
// reconcilers populate it from their own state and clients.
type staleDiskWipeParams struct {
	pods      []*lifecycle.MulticlusterPod
	endpoints lazyEndpoints
	dial      podIdentityDialer
	deleter   podDeleter
	threshold time.Duration
	// logs enables the bad_rejoin log-evidence fallback; nil disables it.
	logs podLogsReader
	// debounce + confirmInterval require the same retired identity across passes
	// before any destruction (see wipeDebounce).
	debounce        *wipeDebounce
	confirmInterval time.Duration
	// overrideUUIDs name staged node_id_overrides migration targets; a candidate
	// whose on-disk uuid is in this set is deferred per-identity, never wiped.
	overrideUUIDs map[string]struct{}
}

// staleDiskWipe recovers a decommissioned-broker bad_rejoin crashloop
// (K8S-843): a broker whose data directory outlived its decommission boots the
// stale state, claims a retired identity, is rejected, and crashloops. This
// finds such a pod (persistently not-Ready, on-disk identity colliding with the
// authoritative member-uuid view) and wipes it by deleting its PVCs (if any)
// and the pod so it reschedules onto fresh storage with a new identity; on
// emptyDir clusters the stale identity dies with the pod alone. At most one disk
// is wiped per pass. Unlike the non-destructive PVCUnbinder, this only fires on
// a retired-identity collision confirmed by reading the broker itself.
//
// The guards (see decideStaleDiskWipe) also make wiping the last live broker
// impossible: a confirmed collision means the identity is NOT a current member,
// and requiring IsHealthy with zero nodes down means the other members already
// form a healthy quorum without this disk. The escape hatch is
// --wipe-stale-disk-after=-1s, needed before manual recovery surgery that
// discards data cluster-side or stages an override outside the CR (both of
// which the operator otherwise cannot see).
func staleDiskWipe(ctx context.Context, p staleDiskWipeParams, logger logr.Logger) (ctrl.Result, error) {
	now := time.Now()

	// Cheapest gate first: collect candidates before any endpoint rendering or
	// network read, so a quiesced cluster pays for neither.
	var candidates []*lifecycle.MulticlusterPod
	for _, pod := range p.pods {
		// A Ready pod reports zero not-ready duration, so the (always positive)
		// threshold skips healthy pods.
		if podNotReadyFor(pod.Pod, now) < p.threshold {
			continue
		}
		// A pod already being deleted needs no wipe. Without this gate the wipe
		// re-fires every debounce window for the whole termination grace period,
		// since identity evidence stays re-confirmable until the disk is gone.
		if pod.GetDeletionTimestamp() != nil {
			logger.Info("pod is already being deleted; skipping stale-disk wipe candidate", "pod", pod.GetName())
			if p.debounce != nil {
				p.debounce.forget(wipeDebounceKey(pod))
			}
			continue
		}
		candidates = append(candidates, pod)
	}
	if len(candidates) == 0 {
		return ctrl.Result{}, nil
	}

	endpoints := p.endpoints()
	if len(endpoints) == 0 {
		// Endpoint rendering failed or produced nothing; skip loudly since it
		// costs bad_rejoin recovery coverage until rendering recovers.
		logger.Info("no per-pod admin endpoints available; skipping stale-disk wipe candidates this pass")
		return ctrl.Result{}, nil
	}

	// Authoritative view (membership, uuids, health) comes from the controller
	// leader, which cannot lag its committed membership (see leaderAdmin). No
	// reachable leader -> defer the whole pass.
	authority, authorityPod, ok := leaderAdmin(ctx, p.pods, endpoints, p.dial, now, logger)
	if !ok {
		logger.Info("controller leader unavailable to serve authoritative cluster reads; deferring stale-disk wipe this pass",
			"candidates", len(candidates))
		return ctrl.Result{}, nil
	}
	defer authority.Close()
	logger.V(log.DebugLevel).Info("stale-disk wipe pinned to controller leader", "pod", authorityPod)

	health, err := fetchHealthOverview(ctx, authority)
	if err != nil {
		logger.V(log.DebugLevel).Info("cluster health unavailable from authority, skipping stale-disk wipe this pass", "error", err)
		return ctrl.Result{}, nil
	}
	clusterUUIDs, nodeByUUID, err := clusterMemberUUIDs(ctx, authority)
	if err != nil {
		// Without the member map no collision is confirmable; skip this pass.
		logger.V(log.DebugLevel).Info("cluster member uuids unavailable, skipping stale-disk wipe this pass", "error", err)
		return ctrl.Result{}, nil
	}
	downNodes := len(health.NodesDown)

	// Set when a retired identity is waiting out its debounce window. That wait
	// is a pure timer with no watch event behind it, so without an explicit
	// requeue it rounds up to the controller's periodic sync (e.g. ~30s -> ~3m).
	pendingConfirmation := false

	for _, pod := range candidates {
		notReadyFor := podNotReadyFor(pod.Pod, now)

		endpoint, ambiguous := podAdminEndpoint(endpoints, pod.GetName())
		if ambiguous {
			// The pod name resolves to more than one endpoint (identically-named
			// cross-cluster BrokerPools); reading one broker and wiping another's
			// disk would lose data, so defer.
			logger.Info("pod name maps to more than one admin endpoint across member clusters; skipping stale-disk wipe to avoid acting on the wrong broker", "pod", pod.GetName())
			continue
		}
		if endpoint == "" {
			logger.Info("no admin endpoint resolved for not-ready pod; skipping stale-disk wipe candidate", "pod", pod.GetName())
			continue
		}

		// Read the broker's self (node_id, uuid) and advertised address. A
		// crashlooping broker may not answer -> defer.
		var nodeID int
		var uuid string
		var collision bool
		var collisionReason string
		dialedID, dialedUUID, advertisedHost, dialErr := func() (int, string, string, error) {
			selfAdmin, err := p.dial(ctx, endpoint)
			if err != nil {
				return -1, "", "", err
			}
			defer selfAdmin.Close()
			sctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
			defer cancel()
			return brokerSelfIdentity(sctx, selfAdmin)
		}()
		switch {
		case dialErr == nil:
			if matches, why := responderMatchesPod(advertisedHost, pod.GetName(), pod.Status.PodIP); !matches {
				// The answer is not provably this pod's broker, so it must not
				// confirm a collision that destroys this pod's storage.
				logger.Info("self-identity answer not attributable to the pod; deferring stale-disk wipe",
					"pod", pod.GetName(), "endpoint", endpoint, "reason", why)
				continue
			}
			nodeID, uuid = dialedID, dialedUUID
			collision, collisionReason = identityCollision(clusterUUIDs, nodeID, uuid)
		case p.logs != nil:
			// Log-evidence fallback: a bad_rejoin broker never binds its admin
			// API, so the dial above can never reach it, but its logs carry the
			// same identity plus the refusal verdict (see the anchor constants).
			logUUID, why, ok := readBadRejoinEvidence(ctx, p.logs, pod)
			if !ok {
				logger.Info("could not read self identity of not-ready broker, deferring stale-disk wipe",
					"pod", pod.GetName(), "dialError", dialErr.Error(), "logEvidence", why)
				continue
			}
			id, known := nodeByUUID[logUUID]
			if !known {
				// Never recorded by the cluster: possibly a fresh identity whose
				// registration hasn't replicated, never a retired one. Defer.
				logger.Info("pod logs show bad_rejoin but the boot identity is unknown to the cluster; deferring stale-disk wipe",
					"pod", pod.GetName(), "uuid", logUUID)
				continue
			}
			if _, member := clusterUUIDs[id]; member {
				// Maps to a current member, so not retired. Defer.
				logger.Info("pod logs show bad_rejoin but the boot identity maps to a current member; deferring stale-disk wipe",
					"pod", pod.GetName(), "uuid", logUUID, "nodeID", id)
				continue
			}
			nodeID, uuid = id, logUUID
			collision = true
			collisionReason = fmt.Sprintf("pod logs show a live bad_rejoin retry loop and boot identity %s maps to removed node %d", uuid, nodeID)
			logger.Info("bad_rejoin confirmed via pod logs; treating on-disk identity as retired",
				"pod", pod.GetName(), "nodeID", nodeID, "uuid", uuid, "dialError", dialErr.Error())
		default:
			logger.Info("could not read self identity of not-ready broker, deferring stale-disk wipe", "pod", pod.GetName(), "error", dialErr)
			continue
		}
		// If a staged node_id_overrides migration targets this on-disk identity,
		// its data must be preserved for the reassignment; defer this candidate
		// only (per-identity, so unrelated bad_rejoins are unaffected).
		if collision {
			if _, staged := p.overrideUUIDs[canonicalUUID(uuid)]; staged {
				logger.Info("on-disk identity is the target of a staged node_id_overrides migration; deferring stale-disk wipe to preserve its data",
					"pod", pod.GetName(), "nodeID", nodeID, "uuid", uuid)
				continue
			}
		}
		if !collision && p.debounce != nil {
			// Read as a legitimate member (or unconfirmable): drop any window so
			// an old retired-identity sighting can't chain forward and shortcut
			// the debounce.
			p.debounce.forget(wipeDebounceKey(pod))
		}
		wipe, decision := decideStaleDiskWipe(collision, notReadyFor, p.threshold, health.IsHealthy, downNodes)
		logger.V(log.DebugLevel).Info("stale-disk wipe decision",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(),
			"nodeID", nodeID, "uuid", uuid, "collision", collision, "collisionReason", collisionReason,
			"notReadyFor", notReadyFor.String(), "wipe", wipe, "decision", decision)
		if !wipe {
			continue
		}

		// If neither delete can clear the datadir (e.g. a hostPath), the broker
		// would reschedule onto the same stale disk and the wipe would loop
		// forever; defer with an actionable log instead (needs manual recovery).
		if reason := unwipeableDataDir(pod); reason != "" {
			logger.Info("confirmed retired-identity bad_rejoin but the datadir cannot be reset by the wipe; manual recovery required",
				"pod", pod.GetName(), "nodeID", nodeID, "uuid", uuid, "reason", reason)
			continue
		}

		// Only an identity still the same retired (node_id, uuid) a confirmation
		// interval later is acted on (a mid-registration collision is transient).
		key := wipeDebounceKey(pod)
		confirmed, why := p.debounce.confirm(key, nodeID, uuid, time.Now(), p.confirmInterval)
		if !confirmed {
			logger.Info("stale-disk wipe pending re-confirmation", "pod", pod.GetName(), "nodeID", nodeID, "reason", why)
			pendingConfirmation = true
			continue
		}

		// Re-check health immediately before destruction (same pinned
		// authority) so a fault that started mid-pass defers the wipe.
		fresh, err := fetchHealthOverview(ctx, authority)
		if err != nil {
			logger.Info("could not re-check cluster health before stale-disk wipe; deferring", "pod", pod.GetName(), "error", err)
			continue
		}
		if !fresh.IsHealthy || len(fresh.NodesDown) > 0 {
			logger.Info("cluster health changed since pass start; deferring stale-disk wipe",
				"pod", pod.GetName(), "healthy", fresh.IsHealthy, "nodesDown", len(fresh.NodesDown))
			continue
		}

		// Re-confirm it is still the SAME instance before destroying anything:
		// if it was deleted and recreated (same name, new UID) meanwhile, the
		// by-name deletes would hit the innocent replacement and its fresh PVC.
		// A UID mismatch (or vanished pod) aborts and restarts the window.
		live, err := p.deleter.GetLivePod(ctx, pod)
		if err != nil {
			logger.Info("could not confirm pod identity before stale-disk wipe; deferring", "pod", pod.GetName(), "error", err)
			continue
		}
		if live == nil || live.GetUID() != pod.GetUID() {
			logger.Info("pod changed since it was evaluated (deleted/recreated); skipping stale-disk wipe", "pod", pod.GetName())
			p.debounce.forget(key)
			continue
		}

		logger.Info("wiping stale disk of decommissioned broker stuck in bad_rejoin; deleting PVCs (if any) + pod for clean reschedule",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(), "nodeID", nodeID, "uuid", uuid, "reason", decision)

		if err := p.deleter.DeletePVCsForPod(ctx, pod); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting PVCs for broker pod")
		}
		if err := p.deleter.DeletePod(ctx, pod); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting broker pod after stale-disk wipe")
		}
		p.debounce.forget(key)

		// One wipe per pass; requeue to let the replacement come up first.
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	if pendingConfirmation && p.confirmInterval > 0 {
		return ctrl.Result{RequeueAfter: p.confirmInterval}, nil
	}
	return ctrl.Result{}, nil
}

// reconcileStaleDiskWipe (StretchCluster) — see staleDiskWipe.
func (r *MulticlusterReconciler) reconcileStaleDiskWipe(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("reconcileStaleDiskWipe")

	if r.staleDiskWipeDisabled() {
		logger.V(log.TraceLevel).Info("stale-disk wipe disabled via non-positive threshold; skipping")
		return ctrl.Result{}, nil
	}

	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}

	// Skip the destructive wipe while the broker-pool view is incomplete: the
	// candidate set and endpoints render from observed pools, so a partial view
	// risks a wrong evaluation or endpoint. Return clean, NOT RequeueAfter: a
	// RequeueAfter aborts the Phase-3 chain, and a member-cluster outage can last
	// hours, starving the downstream steps meant to run during exactly that.
	if len(state.unobservedBrokerPoolClusters) > 0 {
		logger.Info("broker pool view incomplete; skipping stale-disk wipe this pass until all member clusters are observed",
			"unobserved", strings.Join(state.unobservedBrokerPoolClusters, ", "))
		return ctrl.Result{}, nil
	}

	return staleDiskWipe(ctx, staleDiskWipeParams{
		pods:            state.pools.ExistingPods(),
		endpoints:       state.podEndpoints,
		dial:            r.podAdminDialer(state),
		deleter:         r.LifecycleClient,
		threshold:       r.staleDiskWipeThreshold(),
		logs:            r.LifecycleClient.GetPodLogs,
		debounce:        &r.staleDiskWipeDebounce,
		confirmInterval: staleDiskWipeConfirmationInterval,
		overrideUUIDs:   configuredOverrideUUIDsStretch(state.cluster.StretchCluster.Spec.Config),
	}, logger)
}

// reconcileStaleDiskWipe (single-cluster Redpanda) — see staleDiskWipe.
func (r *RedpandaReconciler) reconcileStaleDiskWipe(ctx context.Context, state *clusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("reconcileStaleDiskWipe")

	if r.staleDiskWipeDisabled() {
		logger.V(log.TraceLevel).Info("stale-disk wipe disabled via non-positive threshold; skipping")
		return ctrl.Result{}, nil
	}

	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}

	return staleDiskWipe(ctx, staleDiskWipeParams{
		pods:            state.pools.ExistingPods(),
		endpoints:       state.podEndpoints,
		dial:            r.podAdminDialer(state),
		deleter:         r.LifecycleClient,
		threshold:       r.staleDiskWipeThreshold(),
		logs:            r.LifecycleClient.GetPodLogs,
		debounce:        &r.staleDiskWipeDebounce,
		confirmInterval: staleDiskWipeConfirmationInterval,
		overrideUUIDs:   configuredOverrideUUIDs(clusterSpecConfig(state.cluster.Redpanda.Spec.ClusterSpec)),
	}, logger)
}

// configuredOverrideUUIDs extracts the staged node_id_overrides uuids from a
// cluster's *Config (nil-safe), for the per-identity wipe defer. Shared by both
// reconcilers so the guard applies to single-cluster AND StretchCluster.
func configuredOverrideUUIDs(cfg *redpandav1alpha2.Config) map[string]struct{} {
	if cfg == nil || cfg.Node == nil {
		return nil
	}
	return stagedOverrideUUIDs(cfg.Node.Raw)
}

// configuredOverrideUUIDsStretch mirrors configuredOverrideUUIDs for the
// enterprise Config type carried by StretchCluster specs.
func configuredOverrideUUIDsStretch(cfg *entv1alpha2.Config) map[string]struct{} {
	if cfg == nil || cfg.Node == nil {
		return nil
	}
	return stagedOverrideUUIDs(cfg.Node.Raw)
}

// clusterSpecConfig returns the *Config from a (possibly nil) RedpandaClusterSpec.
func clusterSpecConfig(cs *redpandav1alpha2.RedpandaClusterSpec) *redpandav1alpha2.Config {
	if cs == nil {
		return nil
	}
	return cs.Config
}

func (r *MulticlusterReconciler) staleDiskWipeThreshold() time.Duration {
	if r.StaleDiskWipeNotReadyThreshold > 0 {
		return r.StaleDiskWipeNotReadyThreshold
	}
	return defaultStaleDiskWipeNotReadyThreshold
}

// staleDiskWipeDisabled reports whether the wipe is off. Zero or negative
// disables it (matching the sibling --unbind-pvcs-after convention); any
// positive value tunes the not-ready threshold. Defaults differ: StretchCluster
// 5m (on), single-cluster 0 (off, opt-in).
func (r *MulticlusterReconciler) staleDiskWipeDisabled() bool {
	return r.StaleDiskWipeNotReadyThreshold <= 0
}

func (r *RedpandaReconciler) staleDiskWipeThreshold() time.Duration {
	if r.StaleDiskWipeNotReadyThreshold > 0 {
		return r.StaleDiskWipeNotReadyThreshold
	}
	return defaultStaleDiskWipeNotReadyThreshold
}

// staleDiskWipeDisabled — see the MulticlusterReconciler counterpart.
func (r *RedpandaReconciler) staleDiskWipeDisabled() bool {
	return r.StaleDiskWipeNotReadyThreshold <= 0
}

// clusterMemberUUIDs returns the node_id->uuid map of the cluster's CURRENT
// members plus a reverse index of EVERY uuid the cluster has ever recorded.
//
// Membership comes from Brokers(), not GetBrokerUuids(): broker_uuids retains a
// decommissioned node's entry indefinitely, so trusting it for presence would
// mask the very bad_rejoin this must catch. Members are taken from Brokers()
// and each one's uuid attached from GetBrokerUuids(). The reverse index
// (including retired uuids) serves the log-evidence fallback: a log-derived
// uuid must resolve to a known, non-member id before it counts as retired.
func clusterMemberUUIDs(ctx context.Context, admin *rpadmin.AdminAPI) (map[int]string, map[string]int, error) {
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "fetching cluster brokers")
	}
	uuids, err := admin.GetBrokerUuids(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "fetching cluster broker uuids")
	}
	uuidByNode := make(map[int]string, len(uuids))
	nodeByUUID := make(map[string]int, len(uuids))
	for _, u := range uuids {
		uuidByNode[u.NodeID] = u.UUID
		nodeByUUID[u.UUID] = u.NodeID
	}
	out := make(map[int]string, len(brokers))
	for _, b := range brokers {
		// A member with no uuid entry maps to "", which identityCollision
		// treats conservatively (no false positive).
		out[b.NodeID] = uuidByNode[b.NodeID]
	}
	return out, nodeByUUID, nil
}

// fetchHealthOverview reads the cluster health view from the given (pinned)
// admin client with a bounded timeout.
func fetchHealthOverview(ctx context.Context, admin *rpadmin.AdminAPI) (rpadmin.ClusterHealthOverview, error) {
	hctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
	defer cancel()
	return admin.GetHealthOverview(hctx)
}

// brokerSelfIdentity reads one broker's own (node_id, uuid) plus its advertised
// internal RPC host; admin must point at exactly that broker. An absent self
// uuid returns "" so the caller treats the collision as unconfirmable.
//
// node_id comes from the raw /v1/node_config so a missing field maps to the -1
// sentinel, not zero — node id 0 is a real identity, and misattributing it
// would let the wipe confirm a collision against a broker that reported
// nothing. The advertised host lets callers verify the answer came from the
// pod they dialed (responderMatchesPod).
func brokerSelfIdentity(ctx context.Context, admin *rpadmin.AdminAPI) (nodeID int, uuid string, advertisedHost string, err error) {
	raw, err := admin.RawNodeConfig(ctx)
	if err != nil {
		return -1, "", "", errors.Wrap(err, "fetching broker node config")
	}
	nodeID, advertisedHost = parseNodeConfigIdentity(raw)
	uuids, err := admin.GetBrokerUuids(ctx)
	if err != nil {
		return -1, "", "", errors.Wrap(err, "fetching broker uuids")
	}
	for _, u := range uuids {
		if u.NodeID == nodeID {
			return nodeID, u.UUID, advertisedHost, nil
		}
	}
	return nodeID, "", advertisedHost, nil
}

// decideStaleDiskWipe is the guarded decision to destroy a broker's data disk.
// All guards must hold: a confirmed collision, not-ready for at least
// threshold, the cluster healthy, and zero nodes down (never wipe during a live
// partition, where the authoritative view may itself be transiently wrong).
func decideStaleDiskWipe(collision bool, notReadyFor, threshold time.Duration, clusterHealthy bool, downNodes int) (bool, string) {
	switch {
	case !collision:
		return false, "no identity collision"
	case notReadyFor < threshold:
		return false, fmt.Sprintf("not-ready for %s < threshold %s", notReadyFor, threshold)
	case !clusterHealthy:
		return false, "cluster not healthy; deferring destructive wipe"
	case downNodes > 0:
		return false, fmt.Sprintf("%d node(s) down; deferring destructive wipe until partition heals", downNodes)
	default:
		return true, "confirmed decommissioned-broker identity collision on a persistently not-ready pod"
	}
}
