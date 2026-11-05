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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBrokerDeletionPolicyParse(t *testing.T) {
	t.Parallel()
	b := &Broker{}

	for _, valid := range []string{"cascade", "orphan", "Orphan", " CASCADE "} {
		b.SetBrokerDeletionPolicy(valid)
		assert.Contains(t, []string{"cascade", "orphan"}, b.GetBrokerDeletionPolicy())
	}

	b = &Broker{}
	for _, invalid := range []string{"orphaned", "retain", "delete", ""} {
		b.SetBrokerDeletionPolicy(invalid)
		assert.Equalf(t, b.GetBrokerDeletionPolicy(), BrokerDeletionPolicyCascade, "value %q is rejected in set, get should fallback to cascade, not %q", invalid)
	}
}

func TestNoDrainRollGrant(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(RollGrantTTL)

	b := &Broker{}
	b.SetNoDrainRollGrant("hash", deadline)
	assert.True(t, b.HasNoDrainRollGrant())

	// A normal grant over a leftover marker must not act as a no-drain grant.
	b.SetRollGrant("hash", deadline)
	assert.True(t, b.HasRollGrant())
	assert.False(t, b.HasNoDrainRollGrant())

	b.SetNoDrainRollGrant("hash", deadline)
	assert.True(t, b.RemoveRollGrant())
	assert.False(t, b.HasRollGrant())
	assert.NotContains(t, b.Annotations, RollGrantNoDrainAnnotation)
}
