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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	userClusterIndex = "__user_referencing_cluster"
	podClusterIndex  = "__pod_referencing_cluster"
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

func registerPodClusterIndex(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, podClusterIndex, indexPodCluster)
}

func clusterForPod(o client.Object) (types.NamespacedName, bool) {
	pod := o.(*corev1.Pod)
	clusterName := pod.Labels["app.kubernetes.io/instance"]
	if clusterName == "" {
		return types.NamespacedName{}, false
	}

	role := pod.Labels["app.kubernetes.io/name"]
	if !slices.Contains([]string{"redpanda", "console"}, role) {
		return types.NamespacedName{}, false
	}

	if _, ok := pod.Labels["batch.kubernetes.io/job-name"]; ok {
		return types.NamespacedName{}, false
	}

	return types.NamespacedName{
		Namespace: o.GetNamespace(),
		Name:      clusterName,
	}, true
}

func consolePodsForCluster(ctx context.Context, c client.Client, cluster *redpandav1alpha2.Redpanda) ([]corev1.Pod, error) {
	return podsForClusterByRole(ctx, c, cluster, "console")
}

func redpandaPodsForCluster(ctx context.Context, c client.Client, cluster *redpandav1alpha2.Redpanda) ([]corev1.Pod, error) {
	return podsForClusterByRole(ctx, c, cluster, "redpanda")
}

func podsForClusterByRole(ctx context.Context, c client.Client, cluster *redpandav1alpha2.Redpanda, role string) ([]corev1.Pod, error) {
	key := client.ObjectKeyFromObject(cluster).String() + "/" + role

	podList := &corev1.PodList{}
	err := c.List(ctx, podList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(podClusterIndex, key),
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

func indexPodCluster(o client.Object) []string {
	nn, found := clusterForPod(o)
	if !found {
		return nil
	}
	role := o.GetLabels()["app.kubernetes.io/name"]

	// we add two cache keys:
	// 1. namespace/name of the cluster
	// 2. namespace/name/role to identify if this is a console or redpanda pod

	baseID := nn.String()
	roleID := baseID + "/" + role

	return []string{baseID, roleID}
}
