// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package nodepools exists currently because it is required to be a separate due to import conflicts.
// Since nearly every package depends on vectorizedv1alpha1, we have to move
// some functions out of it, because vectorizedv1alpha1 can not import anything
// that depends on it (and since almost everything depends on
// vectorizedv1alpha1, it can basically import almost nothing)
//
// Practically, vectorizedv1alpha1 -> pkg/labels is impossible, so we move these functions out of vectorizedv1alpha1.
package nodepools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetNodePools gets all NodePoolSpecs within a cluster.
// This also includes deleted node pools. These are removed from spec,
// therefore a NodePoolSpec is synthesized by searching for existing StatefulSets.
// To include information that a NodePool has been deleted, the NodePoolSpec is
// wrapped into a type with an extra Deleted boolean.
func GetNodePools(ctx context.Context, cluster *vectorizedv1alpha1.Cluster, k8sClient client.Reader) ([]*vectorizedv1alpha1.NodePoolSpecWithDeleted, error) {
	var nodePoolsWithDeleted []*vectorizedv1alpha1.NodePoolSpecWithDeleted

	nps := cluster.GetNodePoolsFromSpec()
	for i := range nps {
		np := nps[i]

		nodePoolsWithDeleted = append(nodePoolsWithDeleted, &vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: np,
		})
	}

	// Also add "virtual NodePools" based on StatefulSets found.
	// These represent deleted NodePools - they will not show up in spec.NodePools.
	var stsList appsv1.StatefulSetList
	err := k8sClient.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.ForCluster(cluster).AsClientSelector(),
	})
	if err != nil {
		return nil, err
	}
outer:
	for i := range stsList.Items {
		sts := stsList.Items[i]

		// Extra paranoid sanity check so we don't touch STS we don't own.
		if !slices.ContainsFunc(
			sts.OwnerReferences,
			func(ownerRef metav1.OwnerReference) bool {
				return ownerRef.UID == cluster.UID
			}) {
			continue outer
		}

		var npName string
		if strings.EqualFold(cluster.Name, sts.Name) {
			npName = vectorizedv1alpha1.DefaultNodePoolName
		} else {
			// STS name for a non-default NodePool is <CLUSTERNAME>-<NPNAME>.
			// So we ignore leading cluster name and additional character (`-`) that
			// separates cluster name with node pool name
			npName = strings.TrimPrefix(sts.Name, fmt.Sprintf("%s-", cluster.Name))
		}

		// Have seen it in NodePoolSpec - therefore, it's not a deleted NodePool,
		// so we don't need to reconstruct it based on the STS.
		if slices.ContainsFunc(nps, func(np vectorizedv1alpha1.NodePoolSpec) bool {
			return np.Name == npName
		}) {
			continue
		}

		replicas := sts.Spec.Replicas
		if st, ok := cluster.Status.NodePools[npName]; ok {
			replicas = &st.CurrentReplicas
		}

		var redpandaContainer *corev1.Container
		for i := range sts.Spec.Template.Spec.Containers {
			container := sts.Spec.Template.Spec.Containers[i]
			if container.Name == "redpanda" {
				redpandaContainer = &container
				break
			}
		}
		if redpandaContainer == nil {
			return nil, fmt.Errorf("redpanda container not defined in STS %s template", sts.Name)
		}

		var datadirVcCapacity resource.Quantity
		var datadirVcStorageClassName string

		var cacheVcExists bool
		var cacheVcCapacity resource.Quantity
		var cacheVcStorageClassName string

		for i := range sts.Spec.VolumeClaimTemplates {
			vct := sts.Spec.VolumeClaimTemplates[i]
			if vct.Name == "datadir" {
				datadirVcCapacity = vct.Spec.Resources.Requests[corev1.ResourceStorage]
				if vct.Spec.StorageClassName != nil {
					datadirVcStorageClassName = ptr.Deref(vct.Spec.StorageClassName, "")
				}
			}
			if vct.Name == "shadow-index-cache" {
				cacheVcExists = true
				cacheVcCapacity = vct.Spec.Resources.Requests[corev1.ResourceStorage]
				if vct.Spec.StorageClassName != nil {
					cacheVcStorageClassName = ptr.Deref(vct.Spec.StorageClassName, "")
				}
			}
		}

		np := vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{
				Name:     npName,
				Replicas: replicas,
				Resources: vectorizedv1alpha1.RedpandaResourceRequirements{
					ResourceRequirements: redpandaContainer.Resources,
				},
				Tolerations:  sts.Spec.Template.Spec.Tolerations,
				NodeSelector: sts.Spec.Template.Spec.NodeSelector,
				Storage: vectorizedv1alpha1.StorageSpec{
					Capacity:         datadirVcCapacity,
					StorageClassName: datadirVcStorageClassName,
				},
			},
			Deleted: true,
		}
		if cacheVcExists {
			np.CloudCacheStorage = vectorizedv1alpha1.StorageSpec{
				Capacity:         cacheVcCapacity,
				StorageClassName: cacheVcStorageClassName,
			}
		}
		nodePoolsWithDeleted = append(nodePoolsWithDeleted, &np)
	}
	return nodePoolsWithDeleted, nil
}
