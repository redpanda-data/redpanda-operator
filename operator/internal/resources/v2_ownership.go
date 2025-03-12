// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// V2OwnershipResolver represents an ownership resolver for v2 clusters.
type V2OwnershipResolver struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
}

var _ OwnershipResolver[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda] = (*V2OwnershipResolver)(nil)

// NewV2OwnershipResolver returns a V2OwnershipResolver.
func NewV2OwnershipResolver() *V2OwnershipResolver {
	return &V2OwnershipResolver{
		operatorLabel:  defaultOperatorLabel,
		ownerLabel:     defaultOwnerLabel,
		namespaceLabel: defaultNamespaceLabel,
	}
}

// GetOwnerLabels returns the labels for any all resources associated with a
// v2 cluster.
func (m *V2OwnershipResolver) GetOwnerLabels(cluster *redpandav1alpha2.Redpanda) map[string]string {
	// TODO: this should probably handle some backwards compatibility stuff
	// for v2 clusters that don't yet have these labels on their resources.
	return map[string]string{
		m.namespaceLabel: cluster.GetNamespace(),
		m.ownerLabel:     cluster.GetName(),
		m.operatorLabel:  "v2",
	}
}

// ownerFromLabels returns the v2 cluster based on a resource's labels.
func (m *V2OwnershipResolver) ownerFromLabels(labels map[string]string) types.NamespacedName {
	// TODO: this should probably handle some backwards compatibility stuff
	// for v2 clusters that don't yet have these labels on their resources.
	return types.NamespacedName{
		Namespace: labels[m.namespaceLabel],
		Name:      labels[m.ownerLabel],
	}
}

// ownedByV2 indicates that this resource is owned by a v2 cluster
func (m *V2OwnershipResolver) ownedByV2(labels map[string]string) bool {
	// TODO: this should probably handle some backwards compatibility stuff
	// for v2 clusters that don't yet have these labels on their resources.
	return labels[m.operatorLabel] == "v2"
}

// OwnerForObject maps an object to the v2 cluster that owns it.
func (m *V2OwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	// TODO: this should probably handle some backwards compatibility stuff
	// for v2 clusters that don't yet have these labels on their resources.
	if labels := object.GetLabels(); labels != nil && m.ownedByV2(labels) {
		nn := m.ownerFromLabels(labels)
		if nn.Namespace != "" && nn.Name != "" {
			return &nn
		}
	}
	return nil
}
