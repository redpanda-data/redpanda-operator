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
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	testGroup   = "cluster.test.domain"
	testVersion = "v1"
)

var (
	groupVersion  = schema.GroupVersion{Group: testGroup, Version: testVersion}
	schemeBuilder = &scheme.Builder{GroupVersion: groupVersion}
	AddToScheme   = schemeBuilder.AddToScheme
)

func init() {
	schemeBuilder.Register(&MockCluster{}, &MockClusterList{})
}

type MockBuilder struct {
	*builder.Builder
	base    string
	watches []string
	owns    []string
}

func NewMockBuilder(manager ctrl.Manager) *MockBuilder {
	return &MockBuilder{
		Builder: ctrl.NewControllerManagedBy(manager),
	}
}

func (b *MockBuilder) Base() string {
	return b.base
}

func (b *MockBuilder) Owned() []string {
	return b.owns
}

func (b *MockBuilder) Watched() []string {
	return b.watches
}

func (b *MockBuilder) For(object client.Object, opts ...builder.ForOption) *builder.Builder {
	b.base = fmt.Sprintf("%T", object)
	return b.Builder.For(object, opts...)
}

func (b *MockBuilder) Owns(object client.Object, opts ...builder.OwnsOption) *builder.Builder {
	b.owns = append(b.owns, fmt.Sprintf("%T", object))
	return b.Builder.Owns(object, opts...)
}

func (b *MockBuilder) Watches(object client.Object, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *builder.Builder {
	b.watches = append(b.watches, fmt.Sprintf("%T", object))
	return b.Builder.Watches(object, eventHandler, opts...)
}

type MockCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Status            MockClusterStatus `json:"status,omitempty"`
}

func (in *MockCluster) DeepCopyInto(out *MockCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *MockCluster) DeepCopy() *MockCluster {
	if in == nil {
		return nil
	}
	out := new(MockCluster)
	in.DeepCopyInto(out)
	return out
}

func (in *MockCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

type MockClusterStatus struct{}

func (in *MockClusterStatus) DeepCopyInto(out *MockClusterStatus) {
	*out = *in
}

type MockClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MockCluster `json:"items"`
}

func (in *MockClusterList) DeepCopyInto(out *MockClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MockCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *MockClusterList) DeepCopy() *MockClusterList {
	if in == nil {
		return nil
	}
	out := new(MockClusterList)
	in.DeepCopyInto(out)
	return out
}

func (in *MockClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func mockClusterCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusters." + testGroup,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: testGroup,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    testVersion,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"status": {
									Type:                   "object",
									XPreserveUnknownFields: ptr.To(true),
								},
							},
						},
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "clusters",
				Singular: "cluster",
				ListKind: "MockClusterList",
				Kind:     "MockCluster",
			},
		},
	}
}

func installCRD(ctx context.Context, cl client.Client, crd *apiextensionsv1.CustomResourceDefinition) error {
	if err := cl.Create(ctx, crd); err != nil {
		return err
	}

	return wait.ExponentialBackoffWithContext(ctx, wait.Backoff{
		Duration: 1 * time.Second,
		Steps:    10,
		Cap:      10 * time.Second,
	}, func(ctx context.Context) (bool, error) {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(crd), crd); err != nil {
			return false, nil
		}
		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func InstallCRDs(ctx context.Context, cl client.Client) error {
	for _, crd := range []*apiextensionsv1.CustomResourceDefinition{
		mockClusterCRD(),
	} {
		if err := installCRD(ctx, cl, crd); err != nil {
			return err
		}
	}
	return nil
}

type MockOwnershipResolver struct {
	ownerMappings map[types.NamespacedName]map[string]string
}

var _ OwnershipResolver[MockCluster, *MockCluster] = (*MockOwnershipResolver)(nil)

func NewMockOwnershipResolver() *MockOwnershipResolver {
	return &MockOwnershipResolver{
		ownerMappings: make(map[types.NamespacedName]map[string]string),
	}
}

func (r *MockOwnershipResolver) AddOwner(cluster *MockCluster, labels map[string]string) {
	nn := client.ObjectKeyFromObject(cluster)

	r.ownerMappings[nn] = labels
}

func (r *MockOwnershipResolver) GetOwnerLabels(cluster *MockCluster) map[string]string {
	nn := client.ObjectKeyFromObject(cluster)

	if labels, ok := r.ownerMappings[nn]; ok {
		return labels
	}

	return nil
}

func (r *MockOwnershipResolver) AddLabels(cluster *MockCluster) map[string]string {
	return r.GetOwnerLabels(cluster)
}

func (r *MockOwnershipResolver) OwnerForObject(object client.Object) *types.NamespacedName {
	for owner, labels := range r.ownerMappings {
		objectLabels := object.GetLabels()

		matched := true
		for key, value := range labels {
			if objectValue, ok := objectLabels[key]; !ok || objectValue != value {
				matched = false
				break
			}
		}

		if matched {
			return &owner
		}
	}

	return nil
}

type MockSimpleResourceRenderer struct {
	watchedResources []client.Object
	resourceMappings map[types.NamespacedName][]client.Object
	errorMappings    map[types.NamespacedName]error
}

var _ SimpleResourceRenderer[MockCluster, *MockCluster] = (*MockSimpleResourceRenderer)(nil)

func NewMockSimpleResourceRenderer() *MockSimpleResourceRenderer {
	return &MockSimpleResourceRenderer{
		resourceMappings: make(map[types.NamespacedName][]client.Object),
		errorMappings:    make(map[types.NamespacedName]error),
	}
}

func (r *MockSimpleResourceRenderer) SetResources(cluster *MockCluster, resources []client.Object) {
	nn := client.ObjectKeyFromObject(cluster)
	r.resourceMappings[nn] = resources
}

func (r *MockSimpleResourceRenderer) SetError(cluster *MockCluster, err error) {
	nn := client.ObjectKeyFromObject(cluster)
	r.errorMappings[nn] = err
}

func (r *MockSimpleResourceRenderer) SetWatchedResources(resources []client.Object) {
	r.watchedResources = resources
}

func (r *MockSimpleResourceRenderer) Render(ctx context.Context, cluster *MockCluster, _ string) ([]client.Object, error) {
	nn := client.ObjectKeyFromObject(cluster)

	if err, ok := r.errorMappings[nn]; ok {
		return nil, err
	}

	if resources, ok := r.resourceMappings[nn]; ok {
		return resources, nil
	}

	return nil, nil
}

func (r *MockSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return r.watchedResources
}

type MockNodePoolRenderer struct {
	poolMappings  map[types.NamespacedName][]*MulticlusterStatefulSet
	errorMappings map[types.NamespacedName]error
}

var _ NodePoolRenderer[MockCluster, *MockCluster] = (*MockNodePoolRenderer)(nil)

func NewMockNodePoolRenderer() *MockNodePoolRenderer {
	return &MockNodePoolRenderer{
		poolMappings:  make(map[types.NamespacedName][]*MulticlusterStatefulSet),
		errorMappings: make(map[types.NamespacedName]error),
	}
}

func (r *MockNodePoolRenderer) SetPools(cluster *MockCluster, pools []*MulticlusterStatefulSet) {
	nn := client.ObjectKeyFromObject(cluster)
	r.poolMappings[nn] = pools
}

func (r *MockNodePoolRenderer) SetError(cluster *MockCluster, err error) {
	nn := client.ObjectKeyFromObject(cluster)
	r.errorMappings[nn] = err
}

func (r *MockNodePoolRenderer) Render(ctx context.Context, cluster *MockCluster, clusterName string) ([]*appsv1.StatefulSet, error) {
	nn := client.ObjectKeyFromObject(cluster)

	if err, ok := r.errorMappings[nn]; ok {
		return nil, err
	}

	filtered := []*appsv1.StatefulSet{}
	if pools, ok := r.poolMappings[nn]; ok {
		for _, pool := range pools {
			if pool.clusterName == clusterName {
				filtered = append(filtered, pool.StatefulSet.DeepCopy())
			}
		}
		return filtered, nil
	}

	return nil, nil
}

func (r *MockNodePoolRenderer) IsNodePool(object client.Object) bool {
	if !isNodePool(object) {
		return false
	}

	nn := client.ObjectKeyFromObject(object)

	for _, pools := range r.poolMappings {
		for _, pool := range pools {
			if nn == client.ObjectKeyFromObject(pool) {
				return true
			}
		}
	}

	return false
}

type MockClusterStatusUpdater struct{}

var _ ClusterStatusUpdater[MockCluster, *MockCluster] = (*MockClusterStatusUpdater)(nil)

func NewMockClusterStatusUpdater() *MockClusterStatusUpdater {
	return &MockClusterStatusUpdater{}
}

func (u *MockClusterStatusUpdater) Update(cluster *MockCluster, status *ClusterStatus) bool {
	return false
}

func MockResourceManagersSetup() (
	*MockOwnershipResolver,
	*MockClusterStatusUpdater,
	*MockNodePoolRenderer,
	*MockSimpleResourceRenderer,
	func(mgr ctrl.Manager) (
		OwnershipResolver[MockCluster, *MockCluster],
		ClusterStatusUpdater[MockCluster, *MockCluster],
		NodePoolRenderer[MockCluster, *MockCluster],
		SimpleResourceRenderer[MockCluster, *MockCluster],
	),
) {
	resolver, updater, nodeRenderer, resourceRenderer := NewMockOwnershipResolver(), NewMockClusterStatusUpdater(), NewMockNodePoolRenderer(), NewMockSimpleResourceRenderer()
	return resolver, updater, nodeRenderer, resourceRenderer, func(mgr ctrl.Manager) (OwnershipResolver[MockCluster, *MockCluster], ClusterStatusUpdater[MockCluster, *MockCluster], NodePoolRenderer[MockCluster, *MockCluster], SimpleResourceRenderer[MockCluster, *MockCluster]) {
		return resolver, updater, nodeRenderer, resourceRenderer
	}
}
