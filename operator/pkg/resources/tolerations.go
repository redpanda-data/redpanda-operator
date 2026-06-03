// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// InjectNodeUnavailableTolerations returns a toleration slice that
// guarantees the broker pod will tolerate `node.kubernetes.io/not-ready`
// and `node.kubernetes.io/unreachable` taints (effect NoExecute) for
// the configured duration. The default K8s `tolerationSeconds` of 300s
// might be too short for stateful workloads with local storage — brief
// transient node unreachability triggers eviction and ultimately leads
// to PVC deletion / data loss via the unbinder.
//
// Behavior:
//
//   - If `seconds` is nil, the toleration is added with no
//     tolerationSeconds field — the pod tolerates the taint forever.
//     Suitable for cloud-managed K8s where Node-object deletion is the
//     authoritative signal of permanent node loss.
//   - If `seconds` is a non-nil pointer to a number, that number is
//     used as `tolerationSeconds`.
//
// User-set tolerations for these two keys are preserved (the helper
// only appends defaults when no toleration for that specific key
// already exists). This ensures operator-level fleet defaults don't
// silently override per-cluster overrides provided via the CR or chart.
func InjectNodeUnavailableTolerations(existing []corev1.Toleration, seconds *int64) []corev1.Toleration {
	hasNotReady := false
	hasUnreachable := false
	for _, t := range existing {
		switch t.Key {
		case corev1.TaintNodeNotReady:
			hasNotReady = true
		case corev1.TaintNodeUnreachable:
			hasUnreachable = true
		}
	}
	out := existing
	if !hasNotReady {
		out = append(out, corev1.Toleration{
			Key:               corev1.TaintNodeNotReady,
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: seconds,
		})
	}
	if !hasUnreachable {
		out = append(out, corev1.Toleration{
			Key:               corev1.TaintNodeUnreachable,
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: seconds,
		})
	}
	return out
}

// MaybeInjectNodeUnavailableTolerations is the operator-flag-driven
// caller for InjectNodeUnavailableTolerations. It maps the
// `--broker-pod-node-unavailable-toleration` duration value to the
// helper's behavior:
//
//   - dur == 0: feature OFF (the operator default). Existing
//     tolerations are returned unchanged. Preserves backward
//     compatibility for clusters that aren't opted in.
//   - dur > 0: inject with tolerationSeconds = dur.Seconds().
//   - dur < 0: inject with no tolerationSeconds (tolerate forever).
//     Pass `-1` (or any negative duration) when you want pods to stay
//     attached to their node indefinitely; appropriate for cloud K8s
//     where Node-object deletion is the authoritative signal of
//     permanent node loss.
func MaybeInjectNodeUnavailableTolerations(existing []corev1.Toleration, dur time.Duration) []corev1.Toleration {
	if dur == 0 {
		return existing
	}
	var seconds *int64
	if dur > 0 {
		s := int64(dur.Seconds())
		seconds = &s
	}
	return InjectNodeUnavailableTolerations(existing, seconds)
}
