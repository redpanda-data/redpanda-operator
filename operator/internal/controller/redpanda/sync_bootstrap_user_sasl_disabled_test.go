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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// K8S-884: with SASL disabled, syncBootstrapUser used to return before ever
// calling SetBootstrapUserSynced, leaving the condition at its CRD default of
// Unknown/NotReconciled. Because the Quiesced aggregate requires every
// condition to be in a final state, a perpetually-Unknown BootstrapUserSynced
// blocked Quiesced — and therefore Stable — from ever reaching True on an
// otherwise fully healthy cluster.
//
// The fix must set BootstrapUserSynced to a final True state when SASL is
// disabled (there is no bootstrap user to manage, so the condition is
// trivially reconciled).
func TestSyncBootstrapUser_SASLDisabledSetsFinalTrueCondition(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b", "cluster-c"}
	// SASL disabled: no spec.auth at all (the default in stretch-beta manifests).
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "stretch", Namespace: "default"},
		Spec:       redpandav1alpha2.StretchClusterSpec{},
	}
	require.False(t, sc.Spec.Auth.IsSASLEnabled(), "precondition: SASL must be disabled")

	state := newTestState(sc, clusterNames)

	// SASL-disabled short-circuits before touching the manager, so a bare
	// reconciler is sufficient.
	r := &MulticlusterReconciler{}

	result, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)

	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond, "BootstrapUserSynced must be set (not left Unknown) when SASL is disabled")
	require.Equal(t, metav1.ConditionTrue, cond.Status,
		"BootstrapUserSynced must reach a final True state so Quiesced/Stable can converge")
}
