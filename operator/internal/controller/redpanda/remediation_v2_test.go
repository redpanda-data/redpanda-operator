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
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RedpandaReconciler-side pins for the shared remediation behavior whose
// cores live in the enterprise controller package (see maintenance_mode_v2.go
// and stale_disk_wipe_v2.go). The MulticlusterReconciler's chains are pinned
// by the equivalent tests in enterprise/operator/controller.

// reconcilerStepName returns the qualified runtime name of a reconciler-chain
// step's bound method value (e.g.
// ".../redpanda.(*RedpandaReconciler).reconcileMaintenanceMode-fm"). Method
// values are not comparable with == across evaluations, so tests identify a
// step by this stable name rather than by pointer identity.
func reconcilerStepName(fn any) string {
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// indexOfReconcilerStep returns the position of the chain step whose name
// contains "methodName", or -1 if absent.
func indexOfReconcilerStep(stepNames []string, methodName string) int {
	for i, name := range stepNames {
		if strings.Contains(name, "."+methodName+"-fm") {
			return i
		}
	}
	return -1
}

// TestRedpandaReconcilerRunsMaintenanceModeBeforeDecommission:
// reconcileMaintenanceMode must precede reconcileDecommission, or a broker
// stuck in maintenance can never be unblocked — reconcileDecommission requeues
// and aborts the chain while a decommission it started is not yet Finished, and
// the partition balancer refuses to drain a maintenance-mode node.
func TestRedpandaReconcilerRunsMaintenanceModeBeforeDecommission(t *testing.T) {
	r := &RedpandaReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	maintenanceIdx := indexOfReconcilerStep(names, "reconcileMaintenanceMode")
	decommissionIdx := indexOfReconcilerStep(names, "reconcileDecommission")
	require.GreaterOrEqual(t, maintenanceIdx, 0, "reconcileMaintenanceMode not found in chain: %v", names)
	require.GreaterOrEqual(t, decommissionIdx, 0, "reconcileDecommission not found in chain: %v", names)
	assert.Less(t, maintenanceIdx, decommissionIdx,
		"reconcileMaintenanceMode must run before reconcileDecommission, or a broker stuck in maintenance mode can never be unblocked")
}

// TestRedpandaReconcilerRunsStaleDiskWipeBeforeDecommission:
// reconcileStaleDiskWipe must precede reconcileDecommission, or a bad_rejoin
// broker's stale disk can never be wiped — reconcileDecommission requeues and
// aborts the chain whenever HasRecentlyReplacedPods() is true, which a
// bad_rejoin's stuck-not-Ready pod always is (K8S-843).
func TestRedpandaReconcilerRunsStaleDiskWipeBeforeDecommission(t *testing.T) {
	r := &RedpandaReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	staleDiskWipeIdx := indexOfReconcilerStep(names, "reconcileStaleDiskWipe")
	decommissionIdx := indexOfReconcilerStep(names, "reconcileDecommission")
	require.GreaterOrEqual(t, staleDiskWipeIdx, 0, "reconcileStaleDiskWipe not found in chain: %v", names)
	require.GreaterOrEqual(t, decommissionIdx, 0, "reconcileDecommission not found in chain: %v", names)
	assert.Less(t, staleDiskWipeIdx, decommissionIdx,
		"reconcileStaleDiskWipe must run before reconcileDecommission, or a bad_rejoin broker's stale disk can never be wiped (reconcileDecommission requeues on not-ready recently-replaced pods and aborts the chain)")
}

// TestRedpandaReconcilerRunsMaintenanceModeBeforeStaleDiskWipe: the
// non-destructive maintenance clear (removes a leaked flag) must be attempted
// before the destructive stale-disk wipe (deletes storage and the pod).
func TestRedpandaReconcilerRunsMaintenanceModeBeforeStaleDiskWipe(t *testing.T) {
	r := &RedpandaReconciler{}
	chain := r.clusterReconcilers()
	names := make([]string, len(chain))
	for i, fn := range chain {
		names[i] = reconcilerStepName(fn)
	}
	maintenanceIdx := indexOfReconcilerStep(names, "reconcileMaintenanceMode")
	staleDiskWipeIdx := indexOfReconcilerStep(names, "reconcileStaleDiskWipe")
	require.GreaterOrEqual(t, maintenanceIdx, 0, "reconcileMaintenanceMode not found in chain: %v", names)
	require.GreaterOrEqual(t, staleDiskWipeIdx, 0, "reconcileStaleDiskWipe not found in chain: %v", names)
	assert.Less(t, maintenanceIdx, staleDiskWipeIdx,
		"the non-destructive maintenance clear must be attempted before the destructive stale-disk wipe")
}

// TestRedpandaStaleDiskWipeThresholdAndDisable pins the --wipe-stale-disk-after
// disable/tune semantics on the RedpandaReconciler: a zero or negative value
// disables the destructive wipe entirely (never falling back to an internal
// default); a positive value tunes the not-ready threshold.
func TestRedpandaStaleDiskWipeThresholdAndDisable(t *testing.T) {
	cases := []struct {
		name          string
		threshold     time.Duration
		wantDisabled  bool
		wantThreshold time.Duration // only checked when not disabled
	}{
		{name: "zero disables (explicit operator off switch)", threshold: 0, wantDisabled: true},
		{name: "positive tunes the threshold", threshold: 12 * time.Minute, wantDisabled: false, wantThreshold: 12 * time.Minute},
		{name: "negative disables entirely", threshold: -1 * time.Second, wantDisabled: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &RedpandaReconciler{StaleDiskWipeNotReadyThreshold: tc.threshold}
			assert.Equal(t, tc.wantDisabled, r.staleDiskWipeDisabled())
			if !tc.wantDisabled {
				assert.Equal(t, tc.wantThreshold, r.staleDiskWipeThreshold())
			}
		})
	}
}
