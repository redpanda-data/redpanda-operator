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
// User-set tolerations for these two keys are preserved when they
// already cover the NoExecute taint we care about. A toleration counts
// as covering when it (a) targets a matching key, (b) has effect
// NoExecute or the empty string (which matches all effects), and (c)
// is broadly applicable — Operator=Exists, or Operator=Equal with an
// empty Value. A NoSchedule-only or Equal-with-specific-value
// toleration on the same key does NOT count as covering, since the
// pod would still be evicted on NoExecute; in that case the operator
// appends its own toleration alongside the user's.
func InjectNodeUnavailableTolerations(existing []corev1.Toleration, seconds *int64) []corev1.Toleration {
	hasNotReady := false
	hasUnreachable := false
	for _, t := range existing {
		if !coversNoExecute(t) {
			continue
		}
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

// coversNoExecute reports whether `t` would actually prevent
// NoExecute-based eviction for the broker pod. The NotReady and
// Unreachable taints are applied by the node lifecycle controller
// with an empty value, so a toleration covers iff:
//
//   - effect == NoExecute (or empty, which matches every effect), AND
//   - operator == Exists (matches any value), OR
//     operator == Equal (or empty, the default) with an empty Value.
//
// A NoSchedule-only toleration or an Equal-with-specific-value
// toleration on the same key does not cover, since the pod would
// still be evicted on NoExecute.
func coversNoExecute(t corev1.Toleration) bool {
	if t.Effect != corev1.TaintEffectNoExecute && t.Effect != "" {
		return false
	}
	switch t.Operator {
	case corev1.TolerationOpExists:
		return true
	case corev1.TolerationOpEqual, "":
		return t.Value == ""
	default:
		return false
	}
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
