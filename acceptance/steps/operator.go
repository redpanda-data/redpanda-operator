// Copyright 2025 Redpanda Data, Inc.
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

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

func operatorIsRunning(ctx context.Context, t framework.TestingT) {
	var dep appsv1.Deployment
	require.NoError(t, t.Get(ctx, t.ResourceKey("redpanda-operator"), &dep))

	// make sure the resource is stable
	checkStableResource(ctx, t, &dep)

	require.Equal(t, dep.Status.AvailableReplicas, int32(1))
	require.Equal(t, dep.Status.Replicas, int32(1))
	require.Equal(t, dep.Status.ReadyReplicas, int32(1))
	require.Equal(t, dep.Status.UnavailableReplicas, int32(0))
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

func createClusterRoleBinding(ctx context.Context, serviceAccountName, clusterRoleName string) {
	t := framework.T(ctx)

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

func iCanUpgradeToTheLatestOperatorWithTheValues(ctx context.Context, t framework.TestingT, values *godog.DocString) {
	file, err := os.CreateTemp("", "values-*.yaml")
	require.NoError(t, err)

	_, err = file.Write([]byte(values.Content))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, os.RemoveAll(file.Name()))
	})

	t.UpgradeLocalHelmChart(ctx, "../operator/chart", "redpanda-operator", helm.UpgradeOptions{
		Namespace:  t.Namespace(),
		ValuesFile: file.Name(),
	})
}

func iInstallRedpandaHelmChartVersionWithTheValues(ctx context.Context, t framework.TestingT, version string, values *godog.DocString) {
	file, err := os.CreateTemp("", "values-*.yaml")
	require.NoError(t, err)

	_, err = file.Write([]byte(values.Content))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, os.RemoveAll(file.Name()))
	})

	// these are needed for old versions of the operator
	t.ApplyManifest(ctx, fmt.Sprintf("https://raw.githubusercontent.com/redpanda-data/redpanda-operator/refs/tags/%s/operator/config/crd/bases/toolkit.fluxcd.io/helm-controller.yaml", version))
	t.ApplyManifest(ctx, fmt.Sprintf("https://raw.githubusercontent.com/redpanda-data/redpanda-operator/refs/tags/%s/operator/config/crd/bases/toolkit.fluxcd.io/source-controller.yaml", version))

	t.Cleanup(func(ctx context.Context) {
		// make sure we remove all finalizers for these or the CRD cleanup will get wedged
		removeAllFinalizers(ctx, t, schema.GroupVersionKind{
			Group:   "helm.toolkit.fluxcd.io",
			Kind:    "HelmRelease",
			Version: "v2beta2",
		})
		removeAllFinalizers(ctx, t, schema.GroupVersionKind{
			Group:   "source.toolkit.fluxcd.io",
			Kind:    "HelmRepository",
			Version: "v1beta1",
		})
		removeAllFinalizers(ctx, t, schema.GroupVersionKind{
			Group:   "source.toolkit.fluxcd.io",
			Kind:    "HelmChart",
			Version: "v1beta2",
		})
	})

	t.InstallHelmChart(ctx, "https://charts.redpanda.com", "redpanda", "operator", helm.InstallOptions{
		Name:       "redpanda-operator",
		Namespace:  t.Namespace(),
		Version:    version,
		ValuesFile: file.Name(),
	})
}
