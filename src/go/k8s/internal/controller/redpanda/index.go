// Copyright 2024 Redpanda Data, Inc.
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
	"slices"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	userClusterIndex        = "__user_referencing_cluster"
	deploymentClusterIndex  = "__deployment_referencing_cluster"
	statefulsetClusterIndex = "__statefulset_referencing_cluster"
)

func userCluster(user *redpandav1alpha2.User) types.NamespacedName {
	return types.NamespacedName{Namespace: user.Namespace, Name: user.Spec.ClusterSource.ClusterRef.Name}
}

func registerUserClusterIndex(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &redpandav1alpha2.User{}, userClusterIndex, indexUserCluster)
}

func indexUserCluster(o client.Object) []string {
	user := o.(*redpandav1alpha2.User)
	source := user.Spec.ClusterSource

	clusters := []string{}
	if source != nil && source.ClusterRef != nil {
		clusters = append(clusters, userCluster(user).String())
	}

	return clusters
}

func usersForCluster(ctx context.Context, c client.Client, nn types.NamespacedName) ([]reconcile.Request, error) {
	childList := &redpandav1alpha2.UserList{}
	err := c.List(ctx, childList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(userClusterIndex, nn.String()),
	})
	if err != nil {
		return nil, err
	}

	requests := []reconcile.Request{}
	for _, item := range childList.Items { //nolint:gocritic // this is necessary
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			},
		})
	}

	return requests, nil
}

func registerDeploymentClusterIndex(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &appsv1.Deployment{}, deploymentClusterIndex, indexHelmManagedObjectCluster)
}

func registerStatefulSetClusterIndex(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &appsv1.StatefulSet{}, statefulsetClusterIndex, indexHelmManagedObjectCluster)
}

func clusterForHelmManagedObject(o client.Object) (types.NamespacedName, bool) {
	labels := o.GetLabels()
	clusterName := labels["app.kubernetes.io/instance"]
	if clusterName == "" {
		return types.NamespacedName{}, false
	}

	role := labels["app.kubernetes.io/name"]
	if !slices.Contains([]string{"redpanda", "console"}, role) {
		return types.NamespacedName{}, false
	}

	if _, ok := labels["batch.kubernetes.io/job-name"]; ok {
		return types.NamespacedName{}, false
	}

	return types.NamespacedName{
		Namespace: o.GetNamespace(),
		Name:      clusterName,
	}, true
}

func indexHelmManagedObjectCluster(o client.Object) []string {
	nn, found := clusterForHelmManagedObject(o)
	if !found {
		return nil
	}
	role := o.GetLabels()["app.kubernetes.io/name"]

	// we add two cache keys:
	// 1. namespace/name of the cluster
	// 2. namespace/name/role to identify if this is a console or redpanda component

	baseID := nn.String()
	roleID := baseID + "/" + role

	return []string{baseID, roleID}
}

func consoleDeploymentsForCluster(ctx context.Context, c client.Client, cluster *redpandav1alpha2.Redpanda) ([]*appsv1.Deployment, error) {
	key := client.ObjectKeyFromObject(cluster).String() + "/console"

	deploymentList := &appsv1.DeploymentList{}
	err := c.List(ctx, deploymentList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(deploymentClusterIndex, key),
	})
	if err != nil {
		return nil, err
	}

	return mapFn(ptr.To, deploymentList.Items), nil
}

func redpandaStatefulSetsForCluster(ctx context.Context, c client.Client, cluster *redpandav1alpha2.Redpanda) ([]*appsv1.StatefulSet, error) {
	key := client.ObjectKeyFromObject(cluster).String() + "/redpanda"

	ssList := &appsv1.StatefulSetList{}
	err := c.List(ctx, ssList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(statefulsetClusterIndex, key),
	})
	if err != nil {
		return nil, err
	}

	return mapFn(ptr.To, ssList.Items), nil
}

func mapFn[T any, U any](fn func(T) U, a []T) []U {
	s := make([]U, len(a))
	for i := 0; i < len(a); i++ {
		s[i] = fn(a[i])
	}
	return s
}
