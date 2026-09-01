// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package feature

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
)

const (
	v2Prefix = "cluster.redpanda.com"
	v1Prefix = "redpanda.vectorized.io"
)

// V1UseBrokerCR controls whether a V1 Cluster creates Broker CRs
// instead of StatefulSets.
// Valid Value(s): true
var V1UseBrokerCR = Register(V1Flags, AnnotationFeatureFlag[bool]{
	Key:     "operator.redpanda.com/use-broker-cr",
	Default: "false",
	Parse: func(s string) (bool, error) {
		return s == "true", nil
	},
})

// V2UseBrokerCR controls whether a V2 Redpanda creates Broker CRs instead of
// StatefulSets — the same annotation as V1UseBrokerCR, applied to Redpanda
// resources. Deliberately NOT registered in the V2 bundle: SetDefaults would
// stamp "false" onto every Redpanda and re-add the annotation whenever a user
// removes it, and removal is the documented rollback trigger.
// Valid Value(s): true
var V2UseBrokerCR = &AnnotationFeatureFlag[bool]{
	Key:     "operator.redpanda.com/use-broker-cr",
	Default: "false",
	Parse: func(s string) (bool, error) {
		return s == "true", nil
	},
}

// V1Managed controls whether a Cluster resource is
// reconciled or by the cluster controller(s) or not.
// Valid Value(s): false
var V1Managed = Register(V1Flags, AnnotationFeatureFlag[bool]{
	// redpanda.vectorized.io/managed
	Key:     v2Prefix + "/managed",
	Default: "true",
	Parse: func(s string) (bool, error) {
		return s != "false", nil
	},
})

var (
	// V2Managed controls whether a Redpanda resource is
	// reconciled or by the redpanda controller(s) or not.
	// Valid Value(s): false
	V2Managed = Register(V2Flags, AnnotationFeatureFlag[bool]{
		// cluster.redpanda.com/managed
		Key:     v2Prefix + "/managed",
		Default: "true",
		Parse: func(s string) (bool, error) {
			return s != "false", nil
		},
	})

	// RestartOnConfigChange controls whether or not the Redpanda controller
	// will restart a cluster by injecting its cluster config version into its
	// PodSpec.
	// Valid Value(s): true
	RestartOnConfigChange = Register(V2Flags, AnnotationFeatureFlag[bool]{
		Key:     "operator.redpanda.com/restart-cluster-on-config-change",
		Default: "false",
		Parse: func(s string) (bool, error) {
			return s == "true", nil
		},
	})

	// ClusterConfigSyncMode controls how the Redpanda controller
	// synchronizes the cluster's cluster config.
	// Valid Value(s):
	// - additive: Set all keys, don't unset keys not explicit set
	// - declarative: Set all keys, unset any keys not explicitly set
	// - disabled: Don't sync, cluster config at all.
	ClusterConfigSyncMode = Register(V2Flags, AnnotationFeatureFlag[syncclusterconfig.SyncerMode]{
		Key:     "operator.redpanda.com/config-sync-mode",
		Default: "additive",
		Parse:   syncclusterconfig.StringToMode,
	})

	// RollGrant is set by the cluster controller on a Broker CR to permit
	// the Broker controller to perform a disruptive action on its pod
	// (rotate, PV-affinity remediation). The cluster controller grants to at
	// most one Broker at a time and revokes the grant once the roll
	// completes. Value format: <config-checksum>/<deadline-timestamp>. The
	// checksum doubles as the grant's generation: a grant whose checksum does
	// not match the Broker's desired pod template checksum is stale and
	// rejected.
	// Not registered in any bundle — has no default and is never auto-set.
	RollGrant = &AnnotationFeatureFlag[string]{
		Key: "operator.redpanda.com/roll-grant",
		Parse: func(s string) (string, error) {
			return s, nil
		},
	}

	// BrokerDeletionPolicy decides what happens to a Broker's pod and PVCs
	// when its owning cluster is deleted: "cascade" (default) lets the GC
	// delete them with the Broker CR — whole-cluster teardown — while
	// "orphan" releases them (ownerRefs stripped, data survives). Users set
	// it on the Cluster; the owning cluster controller (once wired up) will
	// propagate it onto each Broker CR so it stays readable after the
	// Cluster object is gone.
	// Deletion of a single Broker CR while its cluster is alive always
	// releases the pod and PVCs, regardless of this policy.
	// Not registered in any bundle — never auto-set.
	BrokerDeletionPolicy = &AnnotationFeatureFlag[string]{
		Key:     "operator.redpanda.com/broker-deletion-policy",
		Default: "cascade",
		// Only the two known policies are accepted (case-insensitively).
		// Anything else errors so Get falls back to the documented default
		// with a logged complaint — a typo like "orphaned" must not be
		// silently interpreted as the destructive cascade branch without
		// leaving a trace.
		Parse: func(s string) (string, error) {
			switch normalized := strings.ToLower(strings.TrimSpace(s)); normalized {
			case "cascade", "orphan":
				return normalized, nil
			default:
				return "", fmt.Errorf("invalid broker deletion policy %q: must be %q or %q", s, "cascade", "orphan")
			}
		},
	}
)

// RollGrantTTL is the lease duration of a roll-grant. It is a safety valve
// against grants leaking across controller restarts or wedged rolls, not a
// pacing mechanism: an expired grant is treated as released and the cluster
// controller re-grants (health-gated) if the roll is still outstanding.
const RollGrantTTL = 10 * time.Minute

// FormatRollGrant encodes a roll-grant annotation value,
// <config-checksum>/<unix-deadline>.
func FormatRollGrant(checksum string, deadline time.Time) string {
	return fmt.Sprintf("%s/%d", checksum, deadline.Unix())
}

// ParseRollGrant decodes a roll-grant annotation value. ok is false when the
// value is malformed; expiry is left to the caller.
func ParseRollGrant(grant string) (checksum string, deadline time.Time, ok bool) {
	checksum, deadlineStr, found := strings.Cut(grant, "/")
	if !found || checksum == "" {
		return "", time.Time{}, false
	}
	unix, err := strconv.ParseInt(deadlineStr, 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	return checksum, time.Unix(unix, 0), true
}
