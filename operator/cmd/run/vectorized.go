// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"context"
	"fmt"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	pkglabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// vectorizedDecommissionerAdapter is a helper struct that implements various methods
// of mapping StatefulSets through Vectorized Clusters to arguments for the
// StatefulSetDecommissioner.
type vectorizedDecommissionerAdapter struct {
	client  client.Client
	factory internalclient.ClientFactory
}

func (b *vectorizedDecommissionerAdapter) desiredReplicas(ctx context.Context, sts *appsv1.StatefulSet) (int32, error) {
	// Get Cluster CR, so we can then find its StatefulSets for a full count of desired replicas.
	vectorizedCluster, err := b.getCluster(ctx, sts)
	if err != nil {
		return 0, err
	}

	if vectorizedCluster == nil {
		return 0, nil
	}

	// We assume the cluster is fine and synced, checks have been performed in the filter already.

	// Get all nodepool-sts for this Cluster
	var stsList appsv1.StatefulSetList
	if err := b.client.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: pkglabels.ForCluster(vectorizedCluster).AsClientSelector(),
		Namespace:     vectorizedCluster.Namespace,
	}); err != nil {
		return 0, fmt.Errorf("failed to list statefulsets of Cluster: %w", err)
	}

	if len(stsList.Items) == 0 {
		return 0, errors.New("found 0 StatefulSets for this Cluster")
	}

	var allReplicas int32
	for _, sts := range stsList.Items {
		allReplicas += ptr.Deref(sts.Spec.Replicas, 0)
	}

	// Should not happen, but if it actually happens, we don't want to run ghost broker decommissioner.
	if allReplicas < 3 {
		return 0, errors.Newf("found %d desiredReplicas, but want >= 3", allReplicas)
	}

	if allReplicas != vectorizedCluster.Status.CurrentReplicas || allReplicas != vectorizedCluster.Status.Replicas {
		return 0, errors.Newf("replicas not synced. status.currentReplicas=%d,status.replicas=%d,allReplicas=%d", vectorizedCluster.Status.CurrentReplicas, vectorizedCluster.Status.Replicas, allReplicas)
	}

	return allReplicas, nil
}

func (b *vectorizedDecommissionerAdapter) filter(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	log := ctrl.LoggerFrom(ctx, "namespace", sts.Namespace).WithName("StatefulSetDecomissioner.Filter")

	vectorizedCluster, err := b.getCluster(ctx, sts)
	if err != nil {
		return false, err
	}

	if vectorizedCluster == nil {
		return false, nil
	}

	managedAnnotationKey := vectorizedv1alpha1.GroupVersion.Group + "/managed"
	if managed, exists := vectorizedCluster.Annotations[managedAnnotationKey]; exists && managed == "false" {
		log.V(1).Info("ignoring StatefulSet of unmanaged V1 Cluster", "sts", sts.Name, "namespace", sts.Namespace)
		return false, nil
	}

	// Do some "manual" checks, as ClusterlQuiescent condition is always false if a ghost broker causes unhealthy cluster
	// (and we can therefore not use it to check if the cluster is synced otherwise)
	if vectorizedCluster.Status.CurrentReplicas != vectorizedCluster.Status.Replicas {
		log.V(1).Info("replicas are not synced", "cluster", vectorizedCluster.Name, "namespace", vectorizedCluster.Namespace)
		return false, nil
	}
	if vectorizedCluster.Status.Restarting {
		log.V(1).Info("cluster is restarting", "cluster", vectorizedCluster.Name, "namespace", vectorizedCluster.Namespace)
		return false, nil
	}

	if vectorizedCluster.Status.ObservedGeneration != vectorizedCluster.Generation {
		log.V(1).Info("generation not synced", "cluster", vectorizedCluster.Name, "namespace", vectorizedCluster.Namespace, "generation", vectorizedCluster.Generation, "observedGeneration", vectorizedCluster.Status.ObservedGeneration)
		return false, nil
	}

	if vectorizedCluster.Status.DecommissioningNode != nil {
		log.V(1).Info("decommission in progress", "cluster", vectorizedCluster.Name, "namespace", vectorizedCluster.Namespace, "node", *vectorizedCluster.Status.DecommissioningNode)
		return false, nil
	}

	return true, nil
}

func (b *vectorizedDecommissionerAdapter) getAdminClient(ctx context.Context, sts *appsv1.StatefulSet) (*rpadmin.AdminAPI, error) {
	cluster, err := b.getCluster(ctx, sts)
	if err != nil {
		return nil, err
	}

	if cluster == nil {
		return nil, errors.Newf("failed to resolve %s/%s to vectorized cluster", sts.Namespace, sts.Name)
	}

	return b.factory.RedpandaAdminClient(ctx, cluster)
}

func (b *vectorizedDecommissionerAdapter) getCluster(ctx context.Context, sts *appsv1.StatefulSet) (*vectorizedv1alpha1.Cluster, error) {
	idx := slices.IndexFunc(
		sts.OwnerReferences,
		func(ownerRef metav1.OwnerReference) bool {
			return ownerRef.APIVersion == vectorizedv1alpha1.GroupVersion.String() && ownerRef.Kind == "Cluster"
		})
	if idx == -1 {
		return nil, nil
	}

	var vectorizedCluster vectorizedv1alpha1.Cluster
	if err := b.client.Get(ctx, types.NamespacedName{
		Name:      sts.OwnerReferences[idx].Name,
		Namespace: sts.Namespace,
	}, &vectorizedCluster); err != nil {
		return nil, errors.Wrap(err, "could not get Cluster")
	}

	return &vectorizedCluster, nil
}
