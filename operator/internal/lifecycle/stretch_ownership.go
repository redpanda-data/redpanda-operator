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
)

// StretchOwnershipResolver represents an ownership resolver for stretch clusters.
type StretchOwnershipResolver struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
}

var _ OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchOwnershipResolver)(nil)

// NewV2OwnershipResolver returns a V2OwnershipResolver.
func NewStretchOwnershipResolver() *StretchOwnershipResolver {
	return &StretchOwnershipResolver{
		operatorLabel:  defaultOperatorLabel,
		ownerLabel:     defaultOwnerLabel,
		namespaceLabel: DefaultNamespaceLabel,
	}
}

// AddLabels returns the labels to add to all resources associated with a
// v2 cluster.
func (m *StretchOwnershipResolver) AddLabels(cluster *StretchClusterWithPools) map[string]string {
	return map[string]string{
		m.namespaceLabel: cluster.GetNamespace(),
		m.ownerLabel:     cluster.GetName(),
		m.operatorLabel:  "v2",
		// we add these for backwards compatibility for the time being
		fluxNameLabel:      cluster.GetName(),
		fluxNamespaceLabel: cluster.GetNamespace(),
	}
}

// GetOwnerLabels returns the labels that can identify a resource belonging
// to a given cluster.
func (m *StretchOwnershipResolver) GetOwnerLabels(cluster *StretchClusterWithPools) map[string]string {
	return map[string]string{
		fluxNameLabel:      cluster.GetName(),
		fluxNamespaceLabel: cluster.GetNamespace(),
	}
}

// ownerFromLabels returns the v2 cluster based on a resource's labels.
func (m *StretchOwnershipResolver) ownerFromLabels(labels map[string]string) types.NamespacedName {
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
func (m *StretchOwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	if labels := object.GetLabels(); labels != nil {
		nn := m.ownerFromLabels(labels)
		if nn.Namespace != "" && nn.Name != "" {
			return &nn
		}
	}
	return nil
}
