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
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// V1OwnershipResolver represents an ownership resolver for v2 clusters.
type V1OwnershipResolver struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
}

var _ OwnershipResolver[vectorizedv1alpha1.Cluster, *vectorizedv1alpha1.Cluster] = (*V1OwnershipResolver)(nil)

// NewV1OwnershipResolver returns a V1OwnershipResolver.
func NewV1OwnershipResolver() *V1OwnershipResolver {
	return &V1OwnershipResolver{
		operatorLabel:  defaultOperatorLabel,
		ownerLabel:     defaultOwnerLabel,
		namespaceLabel: defaultNamespaceLabel,
	}
}

func (v V1OwnershipResolver) GetOwnerLabels(cluster *vectorizedv1alpha1.Cluster) map[string]string {
	out := labels.ForCluster(cluster)
	out["migrate"] = "true"
	return out
}

func (v V1OwnershipResolver) AddLabels(cluster *vectorizedv1alpha1.Cluster) map[string]string {
	out := labels.ForCluster(cluster)
	out["migrate"] = "true"
	return out
}

func (v V1OwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	return nil
}
