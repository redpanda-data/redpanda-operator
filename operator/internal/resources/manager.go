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
	"context"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultFieldOwner     = client.FieldOwner("cluster.redpanda.com/operator")
	defaultOwnerLabel     = "cluster.redpanda.com/owner"
	defaultOperatorLabel  = "cluster.redpanda.com/operator"
	defaultNamespaceLabel = "cluster.redpanda.com/namespace"
)

type ClusterStatus struct {
	ObservedGeneration int64
	Replicas           int
	OutOfDateReplicas  int
	UpToDateReplicas   int
	DefunctReplicas    int
	HealthyReplicas    int
	RunningReplicas    int
}

type ResourceManager[T any, U Cluster[T]] interface {
	SetClusterStatus(cluster U, status ClusterStatus) bool
	NodePools(ctx context.Context, cluster U) ([]appsv1.StatefulSet, error)
	OwnedResources(ctx context.Context, cluster U) ([]client.Object, error)
	OwnedResourceTypes(cluster U) []client.Object
	OwnerLabels(cluster U) map[string]string
	GetOwner(object client.Object) (bool, types.NamespacedName)
	GetFieldOwner() client.FieldOwner
}

type ownershipInfo struct {
	operatorLabel  string
	ownerLabel     string
	namespaceLabel string
	fieldOwner     client.FieldOwner
}

type Option func(*ownershipInfo)

func WithFieldOwner(owner string) func(*ownershipInfo) {
	return func(info *ownershipInfo) {
		info.fieldOwner = client.FieldOwner(owner)
	}
}

func WithOperatorLabel(label string) func(*ownershipInfo) {
	return func(info *ownershipInfo) {
		info.operatorLabel = label
	}
}

func WithOwnerLabel(label string) func(*ownershipInfo) {
	return func(info *ownershipInfo) {
		info.ownerLabel = label
	}
}

func WithNamespaceLabel(label string) func(*ownershipInfo) {
	return func(info *ownershipInfo) {
		info.namespaceLabel = label
	}
}

type V2ResourceManager struct {
	client    client.Client
	ownership *ownershipInfo
}

func NewV2ResourceManager(mgr ctrl.Manager, options ...Option) *V2ResourceManager {
	manager := &V2ResourceManager{
		client: mgr.GetClient(),
		ownership: &ownershipInfo{
			fieldOwner:     defaultFieldOwner,
			operatorLabel:  defaultOperatorLabel,
			ownerLabel:     defaultOwnerLabel,
			namespaceLabel: defaultNamespaceLabel,
		},
	}

	for _, opt := range options {
		opt(manager.ownership)
	}

	return manager
}

func (m *V2ResourceManager) NodePools(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]appsv1.StatefulSet, error) {
	return nil, nil
}

func (m *V2ResourceManager) OwnedResources(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]client.Object, error) {
	return nil, nil
}

func (m *V2ResourceManager) OwnedResourceTypes(cluster *redpandav1alpha2.Redpanda) []client.Object {
	return nil
}

func (m *V2ResourceManager) OwnerLabels(cluster *redpandav1alpha2.Redpanda) map[string]string {
	// TODO: handle some backwards compatibility stuff
	return map[string]string{
		m.ownership.namespaceLabel: cluster.GetNamespace(),
		m.ownership.ownerLabel:     cluster.GetName(),
		m.ownership.operatorLabel:  "v2",
	}
}

func (m *V2ResourceManager) ownerFromLabels(labels map[string]string) types.NamespacedName {
	// TODO: handle some backwards compatibility stuff
	return types.NamespacedName{
		Namespace: labels[m.ownership.namespaceLabel],
		Name:      labels[m.ownership.ownerLabel],
	}
}

func (m *V2ResourceManager) ownedByV2(labels map[string]string) bool {
	// TODO: handle some backwards compatibility stuff
	return labels[m.ownership.operatorLabel] == "v2"
}

func (m *V2ResourceManager) GetOwner(object client.Object) (bool, types.NamespacedName) {
	if labels := object.GetLabels(); labels != nil && m.ownedByV2(labels) {
		nn := m.ownerFromLabels(labels)
		if nn.Namespace != "" && nn.Name != "" {
			return true, nn
		}
	}
	return false, types.NamespacedName{}
}

func (m *V2ResourceManager) SetClusterStatus(cluster *redpandav1alpha2.Redpanda, status ClusterStatus) bool {
	// TODO
	return true
}
