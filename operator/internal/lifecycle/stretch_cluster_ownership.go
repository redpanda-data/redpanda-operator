// Copyright 2026 Redpanda Data, Inc.
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

// StretchClusterOwnershipResolver it's a copy of V2OwnershipResolver
type StretchClusterOwnershipResolver struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
}

var _ OwnershipResolver[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchClusterOwnershipResolver)(nil)

// NewStretchClusterOwnershipResolver returns a StretchClusterOwnershipResolver.
func NewStretchClusterOwnershipResolver() *StretchClusterOwnershipResolver {
	return &StretchClusterOwnershipResolver{
		operatorLabel:  defaultOperatorLabel,
		ownerLabel:     defaultOwnerLabel,
		namespaceLabel: DefaultNamespaceLabel,
	}
}

// AddLabels returns the labels to add to all resources associated with a stretch cluster.
func (m *StretchClusterOwnershipResolver) AddLabels(cluster *StretchClusterWithPools) map[string]string {
	return map[string]string{
		m.namespaceLabel: cluster.GetNamespace(),
		m.ownerLabel:     cluster.GetName(),
		m.operatorLabel:  "v2",
	}
}

// GetOwnerLabels returns the labels that can identify a resource belonging
// to a given cluster.
func (m *StretchClusterOwnershipResolver) GetOwnerLabels(cluster *StretchClusterWithPools) map[string]string {
	return map[string]string{
		m.namespaceLabel: cluster.GetNamespace(),
		m.ownerLabel:     cluster.GetName(),
	}
}

// ownerFromLabels returns the stretch cluster based on a resource's labels.
func (m *StretchClusterOwnershipResolver) ownerFromLabels(labels map[string]string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: labels[m.namespaceLabel],
		Name:      labels[m.ownerLabel],
	}
}

// OwnerForObject maps an object to the v2 cluster that owns it.
func (m *StretchClusterOwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	if labels := object.GetLabels(); labels != nil {
		nn := m.ownerFromLabels(labels)
		if nn.Namespace != "" && nn.Name != "" {
			return &nn
		}
	}
	return nil
}
