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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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
	Replicas          int
	OutOfDateReplicas int
	UpToDateReplicas  int
	DefunctReplicas   int
	HealthyReplicas   int
	RunningReplicas   int
	Quiesced          bool
}

type ResourceManager[T any, U Cluster[T]] interface {
	SetClusterStatus(cluster U, status ClusterStatus) bool
	NodePools(ctx context.Context, cluster U) ([]*appsv1.StatefulSet, error)
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
	kubeConfig clientcmdapi.Config
	client     client.Client
	ownership  *ownershipInfo
}

func NewV2ResourceManager(mgr ctrl.Manager, options ...Option) *V2ResourceManager {
	manager := &V2ResourceManager{
		kubeConfig: kube.RestToConfig(mgr.GetConfig()),
		client:     mgr.GetClient(),
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

func (m *V2ResourceManager) NodePools(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]*appsv1.StatefulSet, error) {
	objects, err := m.render(cluster)
	if err != nil {
		return nil, err
	}

	resources := []*appsv1.StatefulSet{}

	// filter out non-statefulsets
	for _, object := range objects {
		if object.GetObjectKind().GroupVersionKind().Kind == "StatefulSet" {
			resources = append(resources, object.(*appsv1.StatefulSet))
		}
	}

	return resources, nil
}

func (m *V2ResourceManager) OwnedResources(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]client.Object, error) {
	objects, err := m.render(cluster)
	if err != nil {
		return nil, err
	}

	resources := []client.Object{}

	// filter out the statefulsets
	for _, object := range objects {
		if object.GetObjectKind().GroupVersionKind().Kind != "StatefulSet" {
			resources = append(resources, object)
		}
	}

	return resources, nil
}

func (m *V2ResourceManager) render(cluster *redpandav1alpha2.Redpanda) ([]client.Object, error) {
	values := cluster.Spec.ClusterSpec.DeepCopy()

	return redpanda.Chart.Render(&m.kubeConfig, helmette.Release{
		Namespace: cluster.Namespace,
		Name:      cluster.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, values)
}

func (m *V2ResourceManager) OwnedResourceTypes(_ *redpandav1alpha2.Redpanda) []client.Object {
	return redpanda.Types()
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
	condition := metav1.Condition{
		Type:               "Quiesced",
		Status:             metav1.ConditionFalse,
		Reason:             "Quiesced",
		ObservedGeneration: cluster.GetGeneration(),
	}
	if status.Quiesced {
		condition.Status = metav1.ConditionTrue
	}
	cluster.Status.ObservedGeneration = cluster.Generation

	return apimeta.SetStatusCondition(&cluster.Status.Conditions, condition)
}

func (m *V2ResourceManager) GetFieldOwner() client.FieldOwner {
	return m.ownership.fieldOwner
}
