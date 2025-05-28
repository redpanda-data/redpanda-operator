// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// V2OwnershipResolver represents an ownership resolver for v2 clusters.
type V2OwnershipResolver struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
}

var _ OwnershipResolver[redpandav1alpha2.ClusterWithPools, *redpandav1alpha2.ClusterWithPools] = (*V2OwnershipResolver)(nil)

// NewV2OwnershipResolver returns a V2OwnershipResolver.
func NewV2OwnershipResolver() *V2OwnershipResolver {
	return &V2OwnershipResolver{
		operatorLabel:  defaultOperatorLabel,
		ownerLabel:     defaultOwnerLabel,
		namespaceLabel: defaultNamespaceLabel,
	}
}

// AddLabels returns the labels to add to all resources associated with a
// v2 cluster.
func (m *V2OwnershipResolver) AddLabels(cluster *redpandav1alpha2.ClusterWithPools) map[string]string {
	return map[string]string{
		m.namespaceLabel: cluster.GetNamespace(),
		m.ownerLabel:     cluster.GetName(),
		m.operatorLabel:  "v2",
		// we add these for backwards compatibility for the time being
		fluxNameLabel:      cluster.Name,
		fluxNamespaceLabel: cluster.Namespace,
	}
}

// GetOwnerLabels returns the labels that can identify a resource belonging
// to a given cluster.
func (m *V2OwnershipResolver) GetOwnerLabels(cluster *redpandav1alpha2.ClusterWithPools) map[string]string {
	return map[string]string{
		fluxNameLabel:      cluster.Name,
		fluxNamespaceLabel: cluster.Namespace,
	}
}

// ownerFromLabels returns the v2 cluster based on a resource's labels.
func (m *V2OwnershipResolver) ownerFromLabels(labels map[string]string) types.NamespacedName {
	owner := labels[m.ownerLabel]
	if owner == "" {
		// fallback to flux labels
		owner = labels[fluxNameLabel]
	}

	namespace := labels[m.namespaceLabel]
	if namespace == "" {
		// fallback to flux labels
		namespace = labels[fluxNamespaceLabel]
	}

	return types.NamespacedName{
		Namespace: namespace,
		Name:      owner,
	}
}

// OwnerForObject maps an object to the v2 cluster that owns it.
func (m *V2OwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	if labels := object.GetLabels(); labels != nil {
		nn := m.ownerFromLabels(labels)
		if nn.Namespace != "" && nn.Name != "" {
			return &nn
		}
	}
	return nil
}
