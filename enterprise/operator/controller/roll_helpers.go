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
	"context"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/rpadmin"
)

// Rolling-restart safety helpers, deliberate duplicates of the OSS redpanda
// controller package's originals (operator/internal/controller/redpanda/
// redpanda_controller.go), which the single-cluster RedpandaReconciler's roll
// loop keeps using. This module must not import that package, so the helpers
// are duplicated here; every function in this file is pinned to its OSS
// original by TestRollSafetyHelpersDrift in
// operator/internal/enterprisedrift/source_drift_test.go (comments excluded,
// code compared exactly).

// brokerIDForPod resolves the pod to a broker ID in the (dual-keyed)
// brokerMap. It looks up by pod name first (the common case, when
// InternalRPCAddress is the per-pod FQDN whose first DNS label is the pod
// name), then falls back to the pod IP. The IP fallback covers a bare-IP
// InternalRPCAddress (a rare misconfiguration): brokerMap then holds that IP
// only as a raw-host key, which a pod-name lookup can't match — without the
// fallback a live, in-sync broker would be misclassified as an orphan and
// deleted without first running its pre-restart probe.
//
// A bucket with more than one broker ID — a StretchCluster with
// identically-named BrokerPools across member clusters can produce this,
// since a StatefulSet/pod name has no member-cluster component — is reported
// as ambiguous rather than resolved to either candidate. Callers MUST NOT
// treat ambiguous the same as unresolved: unresolved means "no broker known
// for this pod" (the roll loop's and scaleDown's orphan-pod paths), whereas
// ambiguous means "this pod maps to more than one real broker and guessing
// could apply a safety check or a decommission to the wrong one."
func brokerIDForPod(brokerMap map[string][]int, podName, podIP string) (brokerID int, resolved bool, ambiguous bool) {
	if ids, ok := brokerMap[podName]; ok {
		if len(ids) > 1 {
			return 0, false, true
		}
		return ids[0], true, false
	}
	if podIP != "" {
		if ids, ok := brokerMap[podIP]; ok {
			if len(ids) > 1 {
				return 0, false, true
			}
			return ids[0], true, false
		}
	}
	return 0, false, false
}

// decideRollAction encodes the per-pod rolling-restart safety decision shared
// by the RedpandaReconciler and MulticlusterReconciler roll loops. Inputs:
// whether the pod maps to a known broker (after the pod-name/pod-IP lookup),
// the cluster-wide health, and — for mapped pods — the per-broker pre-restart
// probe result (brokerSafe, probeErr). It returns whether to roll (delete) the
// pod now, whether to keep evaluating the rest of the rollSet this reconcile
// (proceed=false ⇒ requeue after acting), and a reason for logging.
//
// The table is deliberately conservative:
//   - At most one pod rolls per reconcile (roll=true always pairs with
//     proceed=false), so a transient mis-classification can't tear down
//     multiple brokers at once.
//   - An unmapped pod is deleted only while the cluster is healthy. We have no
//     broker ID for it, so we can't run its pre-restart probe; if the broker
//     map is stale/mis-parsed the "orphan" may be a live, in-sync replica, and
//     rolling it under-replicated risks data loss (RFC cases 3/4). When
//     unhealthy we defer and let mapped pods be gated by their own probes.
//   - A mapped pod rolls only when its pre-restart probe says it's safe; a
//     probe error skips the pod conservatively (retry next reconcile).
func decideRollAction(inBrokerMap, clusterHealthy, brokerSafe bool, probeErr error) (roll, proceed bool, reason string) {
	switch {
	case !inBrokerMap && !clusterHealthy:
		return false, true, "unmapped pod but cluster not healthy; deferring deletion (cannot pre-restart-probe an unidentified broker)"
	case !inBrokerMap:
		return true, false, "unmapped pod, cluster healthy; deleting one pod and requeuing"
	case probeErr != nil:
		return false, true, "pre-restart probe error; skipping pod, will retry"
	case brokerSafe:
		return true, false, "broker safe to restart; rolling"
	default:
		return false, true, "broker not safe to restart right now; skipping pod"
	}
}

func brokerSafeToRestart(ctx context.Context, admin *rpadmin.AdminAPI, brokerID int, clusterIsHealthy bool, logger logr.Logger, podName string) (bool, error) {
	brokerURL, err := admin.BrokerIDToURL(ctx, brokerID)
	if err != nil {
		return false, errors.Wrapf(err, "resolving broker %d URL", brokerID)
	}
	scoped, err := admin.ForHost(brokerURL)
	if err != nil {
		return false, errors.Wrapf(err, "scoping admin client to broker %d (%s)", brokerID, brokerURL)
	}
	defer scoped.Close()

	result, err := scoped.PreRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			// Pre-25.1 broker — fall back to the cluster-wide
			// heuristic. This preserves behavior on older clusters
			// while letting 25.1+ benefit from the precise probe.
			logger.V(log.DebugLevel).Info("pre-restart probe unsupported on broker, falling back to cluster IsHealthy", "pod", podName, "brokerID", brokerID)
			return clusterIsHealthy, nil
		}
		return false, errors.Wrapf(err, "fetching pre-restart probe for broker %d", brokerID)
	}

	if n := len(result.Risks.Acks1DataLoss); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: acks=1 data loss risk", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.Unavailable); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: partitions would become unavailable", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.FullAcksProduceUnavailable); n > 0 {
		logger.V(log.DebugLevel).Info("broker not safe to restart: acks=-1 produce would be rejected", "pod", podName, "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.RF1Offline); n > 0 {
		logger.V(log.TraceLevel).Info("broker restart will briefly offline RF=1 partitions (acceptable)", "pod", podName, "partitions", n)
	}
	return true, nil
}

// brokerCaughtUp consults the broker's post-restart probe (Redpanda 25.1+ —
// /v1/broker/post_restart_probe) to decide whether the broker has finished
// replaying partition state since its last restart. Returns true when
// LoadReclaimedPercent >= threshold (DefaultPostRestartCaughtUpPercent gives
// the strictest 100% reading).
//
// On 404 the function returns (true, nil): the endpoint is absent on
// pre-25.1 brokers, and we've never previously gated on this signal, so the
// safe behavior is to act as though the broker is caught up (the
// HasRecentlyReplacedPods K8s-Ready gate is still in place above).
//
// Any other (non-404) error is wrapped and returned. The rpadmin client already
// applies a bounded retry/backoff (MaxRetries, ~1.5s backoff) to transient
// 5xx/network failures, so reaching this return means the failure persisted.
// The caller MUST treat a returned error as "cannot confirm recovery — defer
// the roll" (fail closed), not proceed: proceeding could roll the next broker
// mid-recovery, especially in a mixed-version cluster where the next broker's
// pre-restart probe 404s and falls back to cluster-wide IsHealthy and therefore
// can't catch it either.
func brokerCaughtUp(ctx context.Context, admin *rpadmin.AdminAPI, brokerID, threshold int, logger logr.Logger, podName string) (bool, error) {
	brokerURL, err := admin.BrokerIDToURL(ctx, brokerID)
	if err != nil {
		return false, errors.Wrapf(err, "resolving broker %d URL", brokerID)
	}
	scoped, err := admin.ForHost(brokerURL)
	if err != nil {
		return false, errors.Wrapf(err, "scoping admin client to broker %d (%s)", brokerID, brokerURL)
	}
	defer scoped.Close()

	result, err := scoped.PostRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			logger.V(log.DebugLevel).Info("post-restart probe unsupported on broker, treating as caught up", "pod", podName, "brokerID", brokerID)
			return true, nil
		}
		return false, errors.Wrapf(err, "fetching post-restart probe for broker %d", brokerID)
	}

	if result.LoadReclaimedPercent < threshold {
		logger.V(log.DebugLevel).Info("broker still post-restart recovering",
			"pod", podName, "brokerID", brokerID,
			"load_reclaimed_pc", result.LoadReclaimedPercent, "threshold", threshold)
		return false, nil
	}
	return true, nil
}

// brokersStillRecovering returns true when any broker in brokerMap reports
// load_reclaimed_pc < threshold via the post-restart probe. The roll loop uses
// this to wait for a just-restarted broker to finish replaying its in-sync
// replicas before proceeding to the next pod.
//
// threshold is the caught-up percentage the operator requires before rolling
// the next broker. It defaults to probes.DefaultPostRestartCaughtUpPercent
// (100 — the strictest reading) and is tunable via the operator's
// --post-restart-caught-up-percent flag for clusters that want to accept
// partial recovery at this gate.
//
// Implementation note: we query every broker in the map rather than
// tracking which specific pods were "recently rolled," because the probe
// answer is per-broker and consistent regardless — a broker that has been
// running for hours and is fully caught up returns 100 every time. The
// extra cost is one admin call per broker per reconcile, gated on
// len(rollSet) > 0 so steady-state clusters don't pay it.
func brokersStillRecovering(ctx context.Context, admin *rpadmin.AdminAPI, brokerMap map[string][]int, threshold int, logger logr.Logger) (bool, error) {
	// Deduplicate broker IDs — brokerMap intentionally double-keys (by
	// first DNS label and raw host) so iterating values directly would
	// query each broker twice. Ambiguity (a bucket with more than one broker
	// ID) doesn't matter here: this scan wants every broker currently known
	// to the map, not a specific pod-to-broker resolution.
	seen := map[int]struct{}{}
	var firstErr error
	for podName, brokerIDs := range brokerMap {
		for _, brokerID := range brokerIDs {
			if _, dup := seen[brokerID]; dup {
				continue
			}
			seen[brokerID] = struct{}{}

			caughtUp, err := brokerCaughtUp(ctx, admin, brokerID, threshold, logger, podName)
			if err != nil {
				// A probe error on one broker must not short-circuit the scan.
				// brokerMap iteration order is random, and the caller treats a
				// returned error as non-fatal and proceeds with the roll — so
				// bailing on the first error could let us roll the next pod while
				// a *different* broker is still recovering, reopening the
				// under-replication window this gate exists to close (RFC
				// cases 2/4). Record the first error and keep scanning; a
				// confirmed still-recovering broker below takes precedence.
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if !caughtUp {
				return true, nil
			}
		}
	}
	return false, firstErr
}
