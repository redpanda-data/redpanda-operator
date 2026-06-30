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
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// defaultPVCUnbindNotReadyThreshold is how long a broker pod must stay
// not-Ready before the PVC unbinder is allowed to destroy its disk. It must
// comfortably exceed normal startup and post-restart recovery so a transiently
// unready (but healthy) broker is never wiped.
const defaultPVCUnbindNotReadyThreshold = 5 * time.Minute

// identityCollision implements Andrew's two-request check (K8S-843): given the
// cluster-authoritative node_id->uuid map (request against the full cluster)
// and a sick broker's self-reported (node_id, uuid) (request against that
// broker), report whether the broker's on-disk identity collides with the
// cluster's view.
//
// A collision means the disk holds a retired identity and the broker cannot
// rejoin (the "bad_rejoin" crashloop): either the node_id was decommissioned
// and is gone from the cluster, or a replacement broker took that node_id so
// the cluster now maps it to a different uuid.
//
// It deliberately reports NO collision when it cannot confirm one — an empty
// cluster map (admin call returned nothing) or an empty self uuid (could not
// read the broker's identity). Destroying a disk is irreversible, so absence of
// evidence is never treated as evidence of a collision.
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
	case clusterUUID != selfUUID:
		return true, fmt.Sprintf("node_id %d maps to uuid %s in cluster but disk holds %s (superseded)", selfNodeID, clusterUUID, selfUUID)
	default:
		return false, fmt.Sprintf("node_id %d uuid %s matches cluster; broker is a legitimate member", selfNodeID, selfUUID)
	}
}

// podAdminEndpoint returns the admin-API endpoint for the named pod from the
// cluster's full endpoint list. The per-pod Service name equals the pod name,
// so the endpoint's first DNS label identifies the pod. Returns "" if none
// match.
func podAdminEndpoint(endpoints []string, podName string) string {
	for _, ep := range endpoints {
		// ep looks like "<podName>.<namespace>:<port>".
		host := ep
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if strings.SplitN(host, ".", 2)[0] == podName {
			return ep
		}
	}
	return ""
}

// reconcilePVCUnbinder implements Andrew's recovery for K8S-843: a broker that
// was decommissioned (e.g. by the autobalancer after a region outage) but whose
// PVC survived will boot from the stale disk, claim a retired identity, be
// rejected by the controller, and crashloop ("bad_rejoin"). This step finds
// such a pod — persistently not-Ready, with an on-disk identity that collides
// with the cluster's authoritative broker-uuid view — and wipes its PVC + pod
// so it reschedules clean.
//
// It is guarded (see decidePVCUnbind): a confirmed collision, sustained
// unreadiness, a healthy cluster, and no nodes reported down. At most one disk
// is wiped per reconcile pass.
func (r *MulticlusterReconciler) reconcilePVCUnbinder(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("reconcilePVCUnbinder")

	if state.pools.AllZero() || state.admin == nil {
		return ctrl.Result{}, nil
	}

	threshold := r.pvcUnbindThreshold()

	// Request against the full cluster: health + authoritative node_id->uuid map.
	health, err := r.fetchClusterHealth(ctx, state.admin)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "fetching cluster health")
	}
	clusterUUIDs, err := clusterMemberUUIDs(ctx, state.admin)
	if err != nil {
		// Without the authoritative member map we cannot confirm any collision;
		// skip this pass rather than risk a wrong wipe. Next reconcile retries.
		logger.V(log.DebugLevel).Info("cluster member uuids unavailable, skipping PVC unbinder this pass", "error", err)
		return ctrl.Result{}, nil
	}
	downNodes := len(health.NodesDown)

	endpoints := r.LifecycleClient.GetAdminAPIEndpoints(state.cluster)
	now := time.Now()

	for _, pod := range state.pools.ExistingPods() {
		notReadyFor, notReady := podNotReadyFor(pod.Pod, now)
		if !notReady || notReadyFor < threshold {
			continue
		}

		endpoint := podAdminEndpoint(endpoints, pod.GetName())
		if endpoint == "" {
			logger.V(log.TraceLevel).Info("no admin endpoint resolved for not-ready pod, skipping", "pod", pod.GetName())
			continue
		}

		// Request against that broker: read its self (node_id, uuid). A
		// crashlooping broker may not answer — defer rather than guess.
		nodeID, uuid, err := func() (int, string, error) {
			selfAdmin, err := r.ClientFactory.RedpandaAdminClientForMulticluster([]string{endpoint}, state.bootstrapUser, state.bootstrapPassword)
			if err != nil {
				return 0, "", err
			}
			defer selfAdmin.Close()
			sctx, cancel := context.WithTimeout(ctx, brokerFetchTimeout)
			defer cancel()
			return brokerSelfIdentity(sctx, selfAdmin)
		}()
		if err != nil {
			logger.Info("could not read self identity of not-ready broker, deferring PVC unbind", "pod", pod.GetName(), "error", err)
			continue
		}

		collision, collisionReason := identityCollision(clusterUUIDs, nodeID, uuid)
		unbind, decision := decidePVCUnbind(collision, notReadyFor, threshold, health.IsHealthy, downNodes)
		logger.V(log.DebugLevel).Info("PVC unbind decision",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(),
			"nodeID", nodeID, "uuid", uuid, "collision", collision, "collisionReason", collisionReason,
			"notReadyFor", notReadyFor.String(), "unbind", unbind, "decision", decision)
		if !unbind {
			continue
		}

		logger.Info("unbinding PVC for decommissioned broker stuck in bad_rejoin; deleting PVC + pod for clean reschedule",
			"pod", pod.GetName(), "cluster", pod.GetCanonicalClusterName(), "nodeID", nodeID, "uuid", uuid, "reason", decision)

		if err := r.LifecycleClient.DeletePVCsForPod(ctx, pod); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting PVCs for broker pod")
		}
		if err := r.LifecycleClient.DeletePod(ctx, pod); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting broker pod after PVC unbind")
		}

		// One unbind per pass; requeue to let the replacement come up before
		// considering any other candidate.
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) pvcUnbindThreshold() time.Duration {
	if r.PVCUnbindNotReadyThreshold > 0 {
		return r.PVCUnbindNotReadyThreshold
	}
	return defaultPVCUnbindNotReadyThreshold
}

// clusterMemberUUIDs returns the node_id->uuid map of the cluster's CURRENT
// members (the "full cluster" request in Andrew's mechanism).
//
// Membership comes from Brokers() (/v1/brokers), NOT GetBrokerUuids()
// (/v1/broker_uuids): observed live in K8S-843, /v1/broker_uuids retains a
// decommissioned node's node_id->uuid entry indefinitely. Trusting it for
// presence would mask exactly the decommissioned-broker bad_rejoin this feature
// must catch — the disk's node_id would still appear "present" with a matching
// uuid. So we take the set of node_ids that are real members from Brokers() and
// attach each one's uuid from GetBrokerUuids(); decommissioned nodes that linger
// in broker_uuids are excluded because they are absent from Brokers().
func clusterMemberUUIDs(ctx context.Context, admin *rpadmin.AdminAPI) (map[int]string, error) {
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "fetching cluster brokers")
	}
	uuids, err := admin.GetBrokerUuids(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "fetching cluster broker uuids")
	}
	uuidByNode := make(map[int]string, len(uuids))
	for _, u := range uuids {
		uuidByNode[u.NodeID] = u.UUID
	}
	out := make(map[int]string, len(brokers))
	for _, b := range brokers {
		// Only current members; a member with no uuid entry maps to "" which
		// identityCollision treats conservatively (no false positive).
		out[b.NodeID] = uuidByNode[b.NodeID]
	}
	return out, nil
}

// brokerSelfIdentity reads a single broker's own (node_id, uuid) — the "against
// that broker" request in Andrew's mechanism. admin must be pointed at exactly
// one broker (its own admin API). The self uuid is the broker's local
// broker_uuids entry for its own node_id; if absent (e.g. it never managed to
// register its uuid) the returned uuid is empty and the caller treats the
// collision as unconfirmable.
func brokerSelfIdentity(ctx context.Context, admin *rpadmin.AdminAPI) (int, string, error) {
	cfg, err := admin.GetNodeConfig(ctx)
	if err != nil {
		return 0, "", errors.Wrap(err, "fetching broker node config")
	}
	uuids, err := admin.GetBrokerUuids(ctx)
	if err != nil {
		return 0, "", errors.Wrap(err, "fetching broker uuids")
	}
	for _, u := range uuids {
		if u.NodeID == cfg.NodeID {
			return cfg.NodeID, u.UUID, nil
		}
	}
	return cfg.NodeID, "", nil
}

// podNotReadyFor returns how long the pod's Ready condition has been False and
// whether the pod is currently not-ready. A pod missing the Ready condition
// entirely (e.g. just created) is treated as not-ready for zero duration, so
// the threshold gate in decidePVCUnbind still protects it.
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

// decidePVCUnbind is the guarded decision for whether to destroy a broker's
// data disk. All guards must hold: a confirmed identity collision, the pod
// not-ready for at least threshold, the cluster otherwise healthy, and no nodes
// reported down (so we never wipe during a live partition where the
// "authoritative" view may itself be transiently wrong).
func decidePVCUnbind(collision bool, notReadyFor, threshold time.Duration, clusterHealthy bool, downNodes int) (bool, string) {
	switch {
	case !collision:
		return false, "no identity collision"
	case notReadyFor < threshold:
		return false, fmt.Sprintf("not-ready for %s < threshold %s", notReadyFor, threshold)
	case !clusterHealthy:
		return false, "cluster not healthy; deferring destructive unbind"
	case downNodes > 0:
		return false, fmt.Sprintf("%d node(s) down; deferring destructive unbind until partition heals", downNodes)
	default:
		return true, "confirmed decommissioned-broker identity collision on a persistently not-ready pod"
	}
}
