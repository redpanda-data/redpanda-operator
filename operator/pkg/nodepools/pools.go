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
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
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

		var np vectorizedv1alpha1.NodePoolSpec
		if nodePoolSpecJSON, ok := sts.Annotations[labels.NodePoolSpecKey]; ok {
			if err := json.Unmarshal([]byte(nodePoolSpecJSON), &np); err != nil {
				return nil, fmt.Errorf("failed to synthesize deleted nodePool %s from its annotation %s", npName, labels.NodePoolSpecKey)
			}
		} else {
			return nil, fmt.Errorf("could not find annotation %s on StatefulSet with name %s", labels.NodePoolSpecKey, sts.Name)
		}

		// Desired replicas for deleted NodePools is always zero.
		np.Replicas = ptr.To(int32(0))

		nodePoolsWithDeleted = append(nodePoolsWithDeleted, &vectorizedv1alpha1.NodePoolSpecWithDeleted{
			NodePoolSpec: np,
			Deleted:      true,
		})
	}
	return nodePoolsWithDeleted, nil
}
