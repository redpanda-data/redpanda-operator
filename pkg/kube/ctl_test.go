// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube_test

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
)

func TestCtl(t *testing.T) {
	ctx := t.Context()
	ctl := kubetest.NewEnv(t)

	require.NoError(t, ctl.Apply(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hello-world",
		},
	}))

	ns, err := kube.Get[corev1.Namespace](ctx, ctl, kube.ObjectKey{Name: "hello-world"})
	require.NoError(t, err)

	// Cond is explicitly a *Namespace here!
	require.EqualError(t, kube.WaitFor(ctx, ctl, ns, func(ns *corev1.Namespace, err error) (bool, error) {
		return false, errors.New("passed through")
	}), "passed through")

	var seen []string
	require.NoError(t, kube.ApplyAllAndWait(ctx, ctl, func(cm *corev1.ConfigMap, err error) (bool, error) {
		seen = append(seen, cm.Name)
		return true, nil
	},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "hello-world", Name: "cm-0"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "hello-world", Name: "cm-1"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "hello-world", Name: "cm-2"}},
	))
	require.Equal(t, []string{"cm-0", "cm-1", "cm-2"}, seen)

	cms, err := kube.List[corev1.ConfigMapList](ctx, ctl, kube.InNamespace("hello-world"))
	require.NoError(t, err)
	require.Len(t, cms.Items, 3)

	t.Run("Delete", func(t *testing.T) {
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "delete-me",
				Namespace: "hello-world",
				// Set a finalizer to stall deletion.
				Finalizers: []string{"i-prevent.com/deletion"},
			},
		}

		require.NoError(t, ctl.Apply(ctx, s))
		require.NotZero(t, s.UID)

		require.NoError(t, ctl.Delete(ctx, s))

		// Refresh s and assert that it's now deleting.
		require.NoError(t, ctl.Get(ctx, kube.AsKey(s), s))
		require.NotNil(t, s.DeletionTimestamp)

		// Re-issue delete with DeletionTimestamp already set to showcase that
		// doesn't result in errors.
		require.NoError(t, ctl.Delete(ctx, s))

		// Clear finalizers to allow deletion to progress.
		s.Finalizers = nil
		require.NoError(t, ctl.ApplyAndWait(ctx, s, kube.IsDeleted))

		// No 404s if we attempt o delete an already deleted object.
		require.NoError(t, ctl.Delete(ctx, s))
	})
}
