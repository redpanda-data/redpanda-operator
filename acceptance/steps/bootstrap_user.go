// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

// bootstrapUserPasswordKey is used to stash the pre-deletion password on the
// scenario context so later steps can assert the operator actually rotated it.
type bootstrapUserPasswordKey string

func bootstrapUserSecretName(cluster string) string {
	return fmt.Sprintf("%s-bootstrap-user", cluster)
}

// iDeleteTheBootstrapUserSecretForCluster records the current bootstrap user
// password and then deletes the Secret. The recorded password is stored on the
// returned context so a follow-up step can confirm the operator regenerates it
// with a *different* value.
func iDeleteTheBootstrapUserSecretForCluster(ctx context.Context, t framework.TestingT, cluster string) context.Context {
	name := bootstrapUserSecretName(cluster)

	var secret corev1.Secret
	require.NoError(t, t.Get(ctx, t.ResourceKey(name), &secret))

	password := string(secret.Data["password"])
	require.NotEmpty(t, password, "bootstrap user secret %q has no password", name)

	t.Logf("Recorded original bootstrap user password (length %d), deleting secret %q", len(password), name)
	require.NoError(t, t.Delete(ctx, &secret))

	require.Eventually(t, func() bool {
		var check corev1.Secret
		err := t.Get(ctx, t.ResourceKey(name), &check)
		return apierrors.IsNotFound(err)
	}, 30*time.Second, 2*time.Second, "secret %q was never deleted", name)

	return context.WithValue(ctx, bootstrapUserPasswordKey(cluster), password)
}

// theBootstrapUserSecretForClusterIsRegenerated waits until the operator has
// recreated the Secret and confirms the new password differs from the recorded
// original. This guards against the test accidentally observing the pre-delete
// Secret before reconciliation runs.
func theBootstrapUserSecretForClusterIsRegenerated(ctx context.Context, t framework.TestingT, cluster string) {
	name := bootstrapUserSecretName(cluster)
	original, _ := ctx.Value(bootstrapUserPasswordKey(cluster)).(string)
	require.NotEmpty(t, original, "no recorded bootstrap user password for cluster %q — did the delete step run?", cluster)

	require.Eventually(t, func() bool {
		var secret corev1.Secret
		if err := t.Get(ctx, t.ResourceKey(name), &secret); err != nil {
			t.Logf("waiting for secret %q to reappear: %v", name, err)
			return false
		}
		current := string(secret.Data["password"])
		if current == "" {
			t.Logf("secret %q has no password yet", name)
			return false
		}
		if current == original {
			t.Logf("secret %q still holds the original password", name)
			return false
		}
		t.Logf("secret %q now holds a regenerated password (length %d, differs from original)", name, len(current))
		return true
	}, 2*time.Minute, 5*time.Second, "secret %q was never regenerated with a different password", name)
}

// iRestartAllPodsInCluster deletes every Pod owned by the cluster's
// StatefulSet so that each replacement Pod's `RPK_USER` / `RPK_PASS` env vars
// are re-materialized from the current bootstrap user Secret. The StatefulSet
// controller takes care of bringing the replacement Pods back.
func iRestartAllPodsInCluster(ctx context.Context, t framework.TestingT, cluster string) {
	var sts appsv1.StatefulSet
	require.NoError(t, t.Get(ctx, t.ResourceKey(cluster), &sts))

	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	require.NoError(t, err)

	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, client.InNamespace(t.Namespace()), client.MatchingLabelsSelector{Selector: selector}))
	require.NotEmpty(t, pods.Items, "no pods found for cluster %q", cluster)

	for i := range pods.Items {
		pod := &pods.Items[i]
		t.Logf("Deleting pod %q to force re-read of bootstrap user env vars", pod.Name)
		require.NoError(t, t.Delete(ctx, pod))
	}
}
