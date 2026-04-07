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
	"k8s.io/apimachinery/pkg/types"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func operatorIsRunning(ctx context.Context, t framework.TestingT) {
	var dep appsv1.Deployment

	// The shared operator is installed in a dedicated namespace during
	// BeforeSuite, not in the feature's namespace.
	key := types.NamespacedName{Name: "redpanda-operator", Namespace: OperatorNamespace}
	require.Eventually(t, func() bool {
		if err := t.Get(ctx, key, &dep); err != nil {
			t.Logf("operator deployment not found yet: %v", err)
			return false
		}
		return dep.Status.AvailableReplicas == 1 &&
			dep.Status.ReadyReplicas == 1 &&
			dep.Status.UnavailableReplicas == 0
	}, 2*time.Minute, 5*time.Second, "operator deployment never became ready")
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
