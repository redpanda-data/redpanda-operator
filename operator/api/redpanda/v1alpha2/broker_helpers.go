// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// GetV1Cluster fetches Cluster CR for given Broker. Returns an error if the
// ClusterRef does not point to a V1 Cluster.
func (b *Broker) GetV1Cluster(ctx context.Context, k8sClient client.Client) (*vectorizedv1alpha1.Cluster, error) {
	if !b.Spec.ClusterRef.IsV1() {
		return nil, fmt.Errorf("cluster reference is not a V1 Cluster")
	}
	v1Cluster := &vectorizedv1alpha1.Cluster{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      b.Spec.ClusterRef.Name,
		Namespace: b.Spec.ClusterRef.GetNamespace(b.Namespace),
	}, v1Cluster)
	if err != nil {
		return nil, err
	}
	return v1Cluster, nil
}

// GetV2Cluster fetches Redpanda CR for given Broker. Returns an error if the
// ClusterRef does not point to a V2 Redpanda.
func (b *Broker) GetV2Cluster(ctx context.Context, k8sClient client.Client) (*Redpanda, error) {
	if !b.Spec.ClusterRef.IsV2() {
		return nil, fmt.Errorf("cluster reference is not a V2 Redpanda")
	}
	v2Cluster := &Redpanda{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      b.Spec.ClusterRef.Name,
		Namespace: b.Spec.ClusterRef.GetNamespace(b.Namespace),
	}, v2Cluster)
	if err != nil {
		return nil, err
	}
	return v2Cluster, nil
}

// GetNodePool fetches NodePool CR for given Broker. Returns an error if the
// ClusterRef does not point to a NodePool.
func (b *Broker) GetNodePool(ctx context.Context, k8sClient client.Client) (*NodePool, error) {
	if !b.Spec.ClusterRef.IsNodePool() {
		return nil, fmt.Errorf("cluster reference is not a NodePool")
	}
	nodePool := &NodePool{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      b.Spec.ClusterRef.Name,
		Namespace: b.Spec.ClusterRef.GetNamespace(b.Namespace),
	}, nodePool)
	if err != nil {
		return nil, err
	}
	return nodePool, nil
}

func (b *Broker) SetV1ClusterRef(v1Cluster *vectorizedv1alpha1.Cluster) {
	b.Spec.ClusterRef = ClusterRef{
		Name:      v1Cluster.Name,
		Namespace: ptr.To(v1Cluster.Namespace),
		Kind:      ptr.To("Cluster"),
		Group:     ptr.To(vectorizedv1alpha1.GroupVersion.Group),
	}
}
