// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// RollGrantTTL is the lease duration of a roll-grant. It is a safety valve
	// against grants leaking across controller restarts or wedged rolls, not a
	// pacing mechanism: an expired grant is treated as released and the cluster
	// controller re-grants (health-gated) if the roll is still outstanding.
	RollGrantTTL = 10 * time.Minute

	// RollGrantAnnotation is set by the cluster controller on a Broker CR to permit
	// the Broker controller to perform a disruptive action on its pod
	// (rotate, PV-affinity remediation). The cluster controller grants to at
	// most one Broker at a time and revokes the grant once the roll
	// completes. Value format: <config-checksum>/<deadline-timestamp>. The
	// checksum doubles as the grant's generation: a grant whose checksum does
	// not match the Broker's desired pod template checksum is stale and
	// rejected.
	RollGrantAnnotation = "operator.redpanda.com/roll-grant"

	// BrokerDeletionPolicyAnnotation is an escape hatch that lets you
	// keep the PVCs after Redpanda cluster teardown;
	// "orphan"  releases them,
	// "cascade" (default) deletes them explicitly.
	BrokerDeletionPolicyAnnotation = "operator.redpanda.com/broker-deletion-policy"
	BrokerDeletionPolicyOrphan     = "orphan"
	BrokerDeletionPolicyCascade    = "cascade"
	BrokerDeletionPolicyDefault    = BrokerDeletionPolicyCascade
)

func (b *Broker) HasRollGrant() bool {
	if b.Annotations == nil {
		return false
	}
	_, ok := b.Annotations[RollGrantAnnotation]
	return ok
}

func (b *Broker) HasValidRollGrant() bool {
	if !b.HasRollGrant() {
		return false
	}
	grant := b.Annotations[RollGrantAnnotation]
	grantChecksum, deadline, ok := parseRollGrant(grant)
	if !ok {
		return false
	}
	if grantChecksum != b.Spec.PodTemplate.Annotations[BrokerPodTemplateHashAnnotation] {
		return false
	}
	return time.Now().Before(deadline)
}

func (b *Broker) HasUnexpiredRollGrant() bool {
	if !b.HasRollGrant() {
		return false
	}
	grant := b.Annotations[RollGrantAnnotation]
	_, deadline, ok := parseRollGrant(grant)
	if !ok {
		return false
	}
	return time.Now().Before(deadline)
}

func (b *Broker) SetRollGrant(checksum string, deadline time.Time) {
	grant := formatRollGrant(checksum, deadline)
	if b.Annotations == nil {
		b.Annotations = make(map[string]string)
	}
	b.Annotations[RollGrantAnnotation] = grant
}

func (b *Broker) RemoveRollGrant() (removed bool) {
	if b.Annotations == nil {
		return
	}
	if _, ok := b.Annotations[RollGrantAnnotation]; ok {
		delete(b.Annotations, RollGrantAnnotation)
		removed = true
	}
	return
}

func (b *Broker) ParseRollGrant() (checksum string, deadline time.Time, ok bool) {
	if b.Annotations == nil {
		return "", time.Time{}, false
	}
	rollGrant, ok := b.Annotations[RollGrantAnnotation]
	if !ok {
		return "", time.Time{}, false
	}
	return parseRollGrant(rollGrant)
}

func (b *Broker) GetBrokerDeletionPolicy() string {
	if b.Annotations == nil {
		return BrokerDeletionPolicyDefault
	}
	policy, ok := b.Annotations[BrokerDeletionPolicyAnnotation]
	if !ok {
		return BrokerDeletionPolicyDefault
	}
	normalizedPolicy := normalizeBrokerDeletionPolicy(policy)
	if normalizedPolicy == "" {
		return BrokerDeletionPolicyDefault
	}
	return normalizedPolicy
}

func (b *Broker) SetBrokerDeletionPolicy(policy string) {
	normalizedPolicy := normalizeBrokerDeletionPolicy(policy)
	if normalizedPolicy == "" {
		return
	}
	if b.Annotations == nil {
		b.Annotations = make(map[string]string)
	}
	b.Annotations[BrokerDeletionPolicyAnnotation] = normalizedPolicy
}

func normalizeBrokerDeletionPolicy(policy string) string {
	normalizedPolicy := strings.ToLower(strings.TrimSpace(policy))
	if normalizedPolicy != BrokerDeletionPolicyOrphan && normalizedPolicy != BrokerDeletionPolicyCascade {
		// refuse to set invalid policy
		return ""
	}
	return normalizedPolicy
}

// FormatRollGrant encodes a roll-grant annotation value,
// <config-checksum>/<unix-deadline>.
func formatRollGrant(checksum string, deadline time.Time) string {
	return fmt.Sprintf("%s/%d", checksum, deadline.Unix())
}

// ParseRollGrant decodes a roll-grant annotation value. ok is false when the
// value is malformed; expiry is left to the caller.
func parseRollGrant(grant string) (checksum string, deadline time.Time, ok bool) {
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
