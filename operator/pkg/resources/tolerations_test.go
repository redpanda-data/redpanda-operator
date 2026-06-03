// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

// TestInjectNodeUnavailableTolerations verifies that the helper adds
// NotReady/Unreachable tolerations only when the user hasn't already
// specified them, and uses the configured seconds value (or nil for
// tolerate-forever) consistently.
func TestInjectNodeUnavailableTolerations(t *testing.T) {
	t.Run("empty input gets both default tolerations", func(t *testing.T) {
		got := resources.InjectNodeUnavailableTolerations(nil, ptr.To(int64(1800)))
		require.Len(t, got, 2)

		var notReady, unreachable *corev1.Toleration
		for i := range got {
			switch got[i].Key {
			case corev1.TaintNodeNotReady:
				notReady = &got[i]
			case corev1.TaintNodeUnreachable:
				unreachable = &got[i]
			}
		}
		require.NotNil(t, notReady)
		require.Equal(t, corev1.TolerationOpExists, notReady.Operator)
		require.Equal(t, corev1.TaintEffectNoExecute, notReady.Effect)
		require.Equal(t, int64(1800), *notReady.TolerationSeconds)

		require.NotNil(t, unreachable)
		require.Equal(t, corev1.TolerationOpExists, unreachable.Operator)
		require.Equal(t, corev1.TaintEffectNoExecute, unreachable.Effect)
		require.Equal(t, int64(1800), *unreachable.TolerationSeconds)
	})

	t.Run("nil seconds means tolerate forever", func(t *testing.T) {
		got := resources.InjectNodeUnavailableTolerations(nil, nil)
		require.Len(t, got, 2)
		for _, tol := range got {
			require.Nil(t, tol.TolerationSeconds, "key=%s", tol.Key)
		}
	})

	t.Run("existing NotReady toleration is preserved", func(t *testing.T) {
		existing := []corev1.Toleration{{
			Key:               corev1.TaintNodeNotReady,
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: ptr.To(int64(99)),
		}}
		got := resources.InjectNodeUnavailableTolerations(existing, ptr.To(int64(1800)))
		require.Len(t, got, 2)
		// First entry is the user's, with their value preserved.
		require.Equal(t, corev1.TaintNodeNotReady, got[0].Key)
		require.Equal(t, int64(99), *got[0].TolerationSeconds)
		// Second entry is the operator-added Unreachable.
		require.Equal(t, corev1.TaintNodeUnreachable, got[1].Key)
		require.Equal(t, int64(1800), *got[1].TolerationSeconds)
	})

	t.Run("existing Unreachable toleration is preserved", func(t *testing.T) {
		existing := []corev1.Toleration{{
			Key:               corev1.TaintNodeUnreachable,
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: ptr.To(int64(42)),
		}}
		got := resources.InjectNodeUnavailableTolerations(existing, ptr.To(int64(1800)))
		require.Len(t, got, 2)
		require.Equal(t, corev1.TaintNodeUnreachable, got[0].Key)
		require.Equal(t, int64(42), *got[0].TolerationSeconds)
		require.Equal(t, corev1.TaintNodeNotReady, got[1].Key)
		require.Equal(t, int64(1800), *got[1].TolerationSeconds)
	})

	t.Run("both already present means no-op", func(t *testing.T) {
		existing := []corev1.Toleration{
			{Key: corev1.TaintNodeNotReady, TolerationSeconds: ptr.To(int64(10))},
			{Key: corev1.TaintNodeUnreachable, TolerationSeconds: ptr.To(int64(20))},
		}
		got := resources.InjectNodeUnavailableTolerations(existing, ptr.To(int64(1800)))
		require.Len(t, got, 2)
		require.Equal(t, int64(10), *got[0].TolerationSeconds, "user's NotReady preserved")
		require.Equal(t, int64(20), *got[1].TolerationSeconds, "user's Unreachable preserved")
	})

	t.Run("unrelated tolerations are preserved", func(t *testing.T) {
		existing := []corev1.Toleration{{
			Key:      "custom.example.com/maintenance",
			Operator: corev1.TolerationOpExists,
		}}
		got := resources.InjectNodeUnavailableTolerations(existing, ptr.To(int64(1800)))
		require.Len(t, got, 3)
		require.Equal(t, "custom.example.com/maintenance", got[0].Key)
	})
}

// TestMaybeInjectNodeUnavailableTolerations verifies the
// flag-driven wrapper: 0 = feature off, positive = bounded seconds,
// negative = tolerate forever.
func TestMaybeInjectNodeUnavailableTolerations(t *testing.T) {
	existing := []corev1.Toleration{{Key: "custom", Operator: corev1.TolerationOpExists}}

	t.Run("zero duration leaves tolerations unchanged (feature off)", func(t *testing.T) {
		got := resources.MaybeInjectNodeUnavailableTolerations(existing, 0)
		require.Len(t, got, 1)
		require.Equal(t, "custom", got[0].Key)
	})

	t.Run("positive duration adds NotReady+Unreachable with bounded seconds", func(t *testing.T) {
		got := resources.MaybeInjectNodeUnavailableTolerations(existing, 30*time.Minute)
		require.Len(t, got, 3)
		var notReady, unreachable *corev1.Toleration
		for i := range got {
			switch got[i].Key {
			case corev1.TaintNodeNotReady:
				notReady = &got[i]
			case corev1.TaintNodeUnreachable:
				unreachable = &got[i]
			}
		}
		require.NotNil(t, notReady)
		require.NotNil(t, notReady.TolerationSeconds)
		require.Equal(t, int64(1800), *notReady.TolerationSeconds)
		require.NotNil(t, unreachable)
		require.NotNil(t, unreachable.TolerationSeconds)
		require.Equal(t, int64(1800), *unreachable.TolerationSeconds)
	})

	t.Run("negative duration adds tolerations without TolerationSeconds (forever)", func(t *testing.T) {
		got := resources.MaybeInjectNodeUnavailableTolerations(existing, -1)
		require.Len(t, got, 3)
		for _, tol := range got {
			if tol.Key == corev1.TaintNodeNotReady || tol.Key == corev1.TaintNodeUnreachable {
				require.Nil(t, tol.TolerationSeconds, "key=%s", tol.Key)
			}
		}
	})
}
