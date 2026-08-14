// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// recordingReporter is a lossy-writer-shaped MigrationReporter: like the V2
// Redpanda reconciler's, Report only mutates in-memory state, and NeedsRollback
// reads back whatever the last Report left there (standing in for the
// persisted condition).
type recordingReporter struct {
	reason  string
	reports []string
}

func (r *recordingReporter) Report(_ context.Context, _ corev1.ConditionStatus, reason, _ string) {
	r.reports = append(r.reports, reason)
	r.reason = reason
}

func (r *recordingReporter) NeedsCompletion(context.Context) bool {
	return r.reason != "" && r.reason != MigrationReasonComplete
}

func (r *recordingReporter) NeedsRollback(context.Context) bool {
	return r.reason != "" && r.reason != MigrationReasonRolledBack
}

// TestRollbackRederivesLostTerminalReport pins the terminal RolledBack report
// as observed-not-recorded, mirroring migration Complete's NeedsCompletion
// mechanism. The one-shot variant lost the report for good in CI: the pass
// that deletes the backup ConfigMap (the resume marker) reports RolledBack
// only in memory, the report rides the owner's end-of-pass status write, and
// that write can be lost — the V2 reconciler swallows update conflicts, and a
// crash loses it too. Every later pass then took the steady-state early
// return and the condition stayed InProgress forever.
func TestRollbackRederivesLostTerminalReport(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	// Steady state: no Broker CRs, no backup ConfigMap.
	rollback := func(t *testing.T, rep *recordingReporter) {
		acted, err := Rollback(context.Background(), RollbackConfig{
			Client:          fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:          scheme,
			Owner:           &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"}},
			ClusterSelector: k8slabels.Everything(),
			Reporter:        rep,
			Logger:          logr.Discard(),
		})
		require.NoError(t, err)
		require.False(t, acted)
	}

	t.Run("lost RolledBack is re-reported", func(t *testing.T) {
		rep := &recordingReporter{reason: MigrationReasonInProgress}
		rollback(t, rep)
		require.Equal(t, []string{MigrationReasonRolledBack}, rep.reports,
			"a rollback whose terminal report never persisted must be promoted to RolledBack in steady state")
	})

	t.Run("never-migrated cluster reports nothing", func(t *testing.T) {
		rep := &recordingReporter{}
		rollback(t, rep)
		require.Empty(t, rep.reports, "a cluster with no migration condition must not get one")
	})

	t.Run("already rolled back reports nothing", func(t *testing.T) {
		rep := &recordingReporter{reason: MigrationReasonRolledBack}
		rollback(t, rep)
		require.Empty(t, rep.reports, "a persisted RolledBack must not be re-written every pass")
	})
}
