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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// theLicenseIsAvailableAsSecret stores the enterprise license under the key
// the operator chart's enterprise.licenseSecretRef examples use.
func theLicenseIsAvailableAsSecret(ctx context.Context, t framework.TestingT, name string) {
	license := os.Getenv(LicenseEnvVar)
	require.NotEmpty(t, license, LicenseEnvVar+" env var must be set")

	require.NoError(t, t.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: t.Namespace()},
		Data:       map[string][]byte{"redpanda.license": []byte(license)},
	}))
}

func setAnnotationOnNodePool(ctx context.Context, t framework.TestingT, annotationKey, annotationValue, poolName string) {
	key := t.ResourceKey(poolName)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var pool redpandav1alpha2.NodePool
		if err := t.Get(ctx, key, &pool); err != nil {
			return err
		}
		if pool.Annotations == nil {
			pool.Annotations = map[string]string{}
		}
		pool.Annotations[annotationKey] = annotationValue
		return t.Update(ctx, &pool)
	}))
	t.Logf("Set annotation %s=%s on NodePool %q", annotationKey, annotationValue, poolName)
}

// deleteNodePool waits for the NodePool to be gone, so later steps see the
// pool's new owner rather than a NodePool still finalizing.
func deleteNodePool(ctx context.Context, t framework.TestingT, poolName string) {
	key := t.ResourceKey(poolName)
	require.NoError(t, t.Delete(ctx, &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
	}))
	require.Eventually(t, func() bool {
		err := t.Get(ctx, key, &redpandav1alpha2.NodePool{})
		return apierrors.IsNotFound(err)
	}, 2*time.Minute, 2*time.Second, "NodePool %q was never deleted", key)
}

func objectShouldHaveConditionWithReason(ctx context.Context, t framework.TestingT, kind, name, conditionType, reason string) {
	objectShouldHaveConditionWithMessage(ctx, t, kind, name, conditionType, reason, "")
}

func objectShouldHaveConditionWithMessage(ctx context.Context, t framework.TestingT, kind, name, conditionType, reason, message string) {
	key := t.ResourceKey(name)

	var last *metav1.Condition
	require.Eventually(t, func() bool {
		conditions, err := conditionsOf(ctx, t, kind, key)
		if err != nil {
			t.Logf("Failed to get %s %q: %v", kind, key, err)
			return false
		}
		last = apimeta.FindStatusCondition(conditions, conditionType)
		if last == nil {
			return false
		}
		t.Logf("%s %q condition %s: status=%s reason=%s message=%q", kind, key, conditionType, last.Status, last.Reason, last.Message)
		return last.Reason == reason && strings.Contains(last.Message, message)
	}, 5*time.Minute, 5*time.Second, "%s %q never had condition %s with reason %q and a message containing %q; last: %+v", kind, key, conditionType, reason, message, last)
}

func conditionsOf(ctx context.Context, t framework.TestingT, kind string, key runtimeclient.ObjectKey) ([]metav1.Condition, error) {
	switch kind {
	case redpandav1alpha2.NodePoolKind:
		var pool redpandav1alpha2.NodePool
		err := t.Get(ctx, key, &pool)
		return pool.Status.Conditions, err
	case redpandav1alpha2.BrokerPoolKind:
		var pool redpandav1alpha2.RedpandaBrokerPool
		err := t.Get(ctx, key, &pool)
		return pool.Status.Conditions, err
	case redpandav1alpha2.RedpandaKind:
		var cluster redpandav1alpha2.Redpanda
		err := t.Get(ctx, key, &cluster)
		return cluster.Status.Conditions, err
	}
	return nil, fmt.Errorf("unsupported kind %q", kind)
}

// poolStatefulSetShouldBeRenderedFromGeneration waits for the operator to
// render the pool's StatefulSet from its owner at the given generation. A
// NodePool and the RedpandaBrokerPool taking its pool render the same
// StatefulSet but for this label, so it shows which one the operator used.
func poolStatefulSetShouldBeRenderedFromGeneration(ctx context.Context, t framework.TestingT, poolName, clusterName string, generation int) {
	var last string
	require.Eventually(t, func() bool {
		sts, err := poolStatefulSet(ctx, t, poolName, clusterName)
		if err != nil {
			t.Logf("%v", err)
			return false
		}
		last = sts.Labels["cluster.redpanda.com/nodepool-generation"]
		return last == strconv.Itoa(generation)
	}, 5*time.Minute, 5*time.Second, "StatefulSet for pool %q was never rendered from generation %d; last: %q", poolName, generation, last)
}

// poolStatefulSetShouldKeepReplicas fails if the pool's StatefulSet ever
// leaves replicas during the window: dropping a pool from the desired set
// decommissions its brokers one at a time, so the first change is a scale-down.
func poolStatefulSetShouldKeepReplicas(ctx context.Context, t framework.TestingT, poolName, clusterName string, replicas, seconds int) {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		sts, err := poolStatefulSet(ctx, t, poolName, clusterName)
		require.NoError(t, err)
		require.Equal(t, int32(replicas), ptr.Deref(sts.Spec.Replicas, 0), "StatefulSet %q changed replicas", sts.Name)
		time.Sleep(5 * time.Second)
	}
}

func poolStatefulSet(ctx context.Context, t framework.TestingT, poolName, clusterName string) (*appsv1.StatefulSet, error) {
	var sets appsv1.StatefulSetList
	if err := t.List(ctx, &sets, runtimeclient.InNamespace(t.ResourceKey(clusterName).Namespace), runtimeclient.MatchingLabels{
		"app.kubernetes.io/instance":         clusterName,
		"cluster.redpanda.com/nodepool-name": poolName,
	}); err != nil {
		return nil, err
	}
	if len(sets.Items) != 1 {
		return nil, fmt.Errorf("found %d StatefulSets for pool %q of cluster %q, want 1", len(sets.Items), poolName, clusterName)
	}
	return &sets.Items[0], nil
}
