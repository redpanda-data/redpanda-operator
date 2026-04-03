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
	"context"
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
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

func (m *StretchClusterOwnershipResolver) ResolveOwnerReference(ctx context.Context, owner *StretchClusterWithPools, clusterName string, ctl *kube.Ctl) (*StretchClusterWithPools, error) {
	sc := &redpandav1alpha2.StretchCluster{}
	err := ctl.Get(ctx, client.ObjectKey{Name: owner.GetName(), Namespace: owner.GetNamespace()}, sc)
	if err != nil {
		return nil, fmt.Errorf("cannot get StretchCluster %s/%s : %w", owner.GetNamespace(), owner.GetName(), err)
	}
	if owner.GetUID() != sc.GetUID() {
		// this means that owner got assigned incorrectly to StretchCluster from a different k8s cluster.
		// We need to fix it, so every resource owner is a StretchCluster from the same cluster as the resource itself.
		newOwner := NewStretchClusterWithPools(sc.DeepCopy(), owner.clusters, owner.NodePools...)
		newOwner.Kind = "StretchCluster"
		newOwner.APIVersion = redpandav1alpha2.GroupVersion.String()
		return newOwner, nil
	}
	return owner, nil
}
