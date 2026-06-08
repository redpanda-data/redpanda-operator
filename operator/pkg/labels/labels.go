// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package labels handles label for cluster resource
package labels

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
// TODO support "app.kubernetes.io/version"
const (
	// The name of a higher level application this one is part of
	NameKey = "app.kubernetes.io/name"
	// A unique name identifying the instance of an application
	InstanceKey = "app.kubernetes.io/instance"
	// The component within the architecture
	ComponentKey = "app.kubernetes.io/component"
	// The name of a higher level application this one is part of
	PartOfKey = "app.kubernetes.io/part-of"
	// The tool being used to manage the operation of an application
	ManagedByKey = "app.kubernetes.io/managed-by"
	// NodePoolKey is used to denote the node pool associated with the StatefulSet.
	NodePoolKey = "cluster.redpanda.com/nodepool"

	// PodNodeIDKey is used to store the Redpanda NodeID of this pod.
	PodNodeIDKey = "operator.redpanda.com/node-id"

	// NodePoolSpecKey is used to store the NodePoolSpec in a StatefulSet's annotations.
	// This allows the operator to correctly reconstruct a NodePoolSpec even
	// after it was removed from Spec already.
	NodePoolSpecKey = "cluster.redpanda.com/node-pool-spec"

	nameKeyRedpandaVal   = "redpanda"
	nameKeyConsoleVal    = "redpanda-console"
	managedByOperatorVal = "redpanda-operator"
)

// CommonLabels holds common labels that belong to all resources owned by this operator
type CommonLabels map[string]string

// ForCluster returns a set of labels that is a union of cluster labels as well as recommended default labels
// recommended by the kubernetes documentation https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func ForCluster(cluster *vectorizedv1alpha1.Cluster) CommonLabels {
	dl := defaultClusterLabels(cluster)
	labels := union(cluster.Labels, dl)

	return labels
}

// AsClientSelector returns label selector made out of subset of common labels: name, instance, component
// return type is apimachinery labels selector, which is used when constructing client calls
func (cl CommonLabels) AsClientSelector() k8slabels.Selector {
	return k8slabels.SelectorFromSet(cl.selectorLabels())
}

// AsClientSelectorForNodePool returns label selector made out of subset of common labels: name, instance, component
// return type is apimachinery labels selector, which is used when constructing client calls
func (cl CommonLabels) AsClientSelectorForNodePool() k8slabels.Selector {
	return k8slabels.SelectorFromSet(cl.nodePoolSelectorLabels())
}

// AsAPISelector returns label selector made out of subset of common labels: name, instance, component
// return type is metav1.LabelSelector type which is used in resource definition
func (cl CommonLabels) AsAPISelector() *metav1.LabelSelector {
	return metav1.SetAsLabelSelector(cl.selectorLabels())
}

// AsAPISelectorForNodePool returns label selector made out of subset of common labels: name, instance, component, nodepool.
// return type is metav1.LabelSelector type which is used in resource definition
// This selector selects all pods of a specific nodepool.
// To select all pods for the cluster, across nodepools, use AsAPISelector.
func (cl CommonLabels) AsAPISelectorForNodePool() *metav1.LabelSelector {
	return metav1.SetAsLabelSelector(cl.nodePoolSelectorLabels())
}

// AsSet returns common labels with types labels.Set
func (cl CommonLabels) AsSet() k8slabels.Set {
	var mapLabels map[string]string = cl
	return mapLabels
}

func (cl CommonLabels) selectorLabels() k8slabels.Set {
	return k8slabels.Set{
		NameKey:      cl[NameKey],
		InstanceKey:  cl[InstanceKey],
		ComponentKey: cl[ComponentKey],
	}
}

func (cl CommonLabels) nodePoolSelectorLabels() k8slabels.Set {
	return k8slabels.Set{
		NameKey:      cl[NameKey],
		InstanceKey:  cl[InstanceKey],
		ComponentKey: cl[ComponentKey],
		NodePoolKey:  cl[NodePoolKey],
	}
}

func (cl CommonLabels) WithNodePool(nodePool string) CommonLabels {
	// union(cl, nil) hands back a fresh copy of cl, so mutating the result
	// below can never corrupt cl itself or any other CommonLabels derived
	// from the same underlying map (see the regression test). NodePoolKey is
	// set unconditionally, rather than through union()'s "existing key wins"
	// rule, since cl may already carry a (possibly different) nodepool value.
	result := CommonLabels(union(cl, nil))
	result[NodePoolKey] = nodePool
	return result
}

// union returns a new map containing the union of mainLabels and newLabels;
// neither input is mutated. If a key is set in mainLabels, that value wins
// over newLabels.
func union(
	mainLabels map[string]string, newLabels map[string]string,
) map[string]string {
	merged := make(map[string]string, len(mainLabels)+len(newLabels))
	for k, v := range newLabels {
		merged[k] = v
	}
	for k, v := range mainLabels {
		merged[k] = v
	}

	return merged
}

func defaultClusterLabels(cluster *vectorizedv1alpha1.Cluster) map[string]string {
	labels := make(map[string]string)
	labels[NameKey] = nameKeyRedpandaVal
	labels[InstanceKey] = cluster.Name
	labels[ComponentKey] = "redpanda"
	labels[PartOfKey] = nameKeyRedpandaVal
	labels[ManagedByKey] = managedByOperatorVal

	return labels
}
