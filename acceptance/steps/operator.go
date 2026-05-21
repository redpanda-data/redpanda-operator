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
	"regexp"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

// operatorReadyTimeout caps how long we wait for the operator Deployment to
// reach 1/1 Ready during acceptance tests. On v25.2.x k3d runners the helm
// install hook returns before the operator pod is Ready, and image pull +
// scheduling under contention can take well over a minute. The previous
// implementation only waited for the Deployment object's ResourceVersion to
// settle (which it does immediately at AvailableReplicas=0) and then
// asserted readiness once, producing flaky 10s timeouts in metrics
// scenarios.
const operatorReadyTimeout = 2 * time.Minute

// operatorReadyPoll is the polling interval for the operator-ready loop.
// Short enough to keep failure logs useful when the operator is genuinely
// stuck.
const operatorReadyPoll = 2 * time.Second

func operatorIsRunning(ctx context.Context, t framework.TestingT) {
	key := t.ResourceKey("redpanda-operator")
	var dep appsv1.Deployment

	t.Logf("Waiting for operator deployment %q to become Ready", key.String())
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, key, &dep); err != nil {
			t.Logf("Failed to get operator deployment %q: %v", key.String(), err)
			return false
		}
		ready := dep.Status.AvailableReplicas == 1 &&
			dep.Status.Replicas == 1 &&
			dep.Status.ReadyReplicas == 1 &&
			dep.Status.UnavailableReplicas == 0
		if !ready {
			t.Logf("Operator deployment %q not yet Ready: replicas=%d available=%d ready=%d unavailable=%d",
				key.String(), dep.Status.Replicas, dep.Status.AvailableReplicas, dep.Status.ReadyReplicas, dep.Status.UnavailableReplicas)
		}
		return ready
	}, operatorReadyTimeout, operatorReadyPoll, "Operator deployment %q never became Ready", key.String())
	t.Logf("Operator deployment %q is Ready", key.String())
}

func requestMetricsEndpointPlainHTTP(ctx context.Context, statusCode string) {
	clientsForOperator(ctx, false, "", statusCode).ExpectRequestRejected(ctx)
}

func requestMetricsEndpointWithTLSAndRandomToken(ctx context.Context, statusCode string) {
	clientsForOperator(ctx, true, "", statusCode).ExpectRequestRejected(ctx)
}

func acceptServiceAccountMetricsRequest(ctx context.Context, serviceAccountName string) {
	clientsForOperator(ctx, true, serviceAccountName, "").ExpectCorrectMetricsResponse(ctx)
}

func createClusterRoleBinding(ctx context.Context, serviceAccountName, clusterRoleRegexp string) {
	t := framework.T(ctx)

	crs := &rbacv1.ClusterRoleList{}
	require.NoError(t, t.List(ctx, crs))
	clusterRoleName := ""
	for _, cr := range crs.Items {
		if regexp.MustCompile(clusterRoleRegexp).Match([]byte(cr.Name)) {
			clusterRoleName = cr.Name
		}
	}

	require.NoError(t, t.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: t.Namespace(),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: t.Namespace(),
			},
		},
	}))

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, t.Delete(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceAccountName,
				Namespace: t.Namespace(),
			},
		}))
	})
}
