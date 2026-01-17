// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package virtual

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	virtualv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/virtual/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/virtual/backends"
)

func handleBackendError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, backends.ErrNotImplemented) {
		return apierrors.NewBadRequest("endpoint not implemented")
	}
	return apierrors.NewInternalError(err)
}

func namespaceFromContext(ctx context.Context) string {
	namespace, ok := apirequest.NamespaceFrom(ctx)
	if !ok {
		return metav1.NamespaceDefault
	}
	return namespace
}

func filterClustersByNamespace(clusters []redpandav1alpha2.Redpanda) []*redpandav1alpha2.Redpanda {
	filtered := []*redpandav1alpha2.Redpanda{}
	byNamespace := map[string][]redpandav1alpha2.Redpanda{}
	for _, cluster := range clusters {
		byNamespace[cluster.Namespace] = append(byNamespace[cluster.Namespace], cluster)
	}
	for _, clusters := range byNamespace {
		if cluster, err := ensureOneClusterPerNamespace(clusters); err == nil && cluster != nil {
			filtered = append(filtered, cluster)
		}
	}

	slices.SortStableFunc(filtered, func(a, b *redpandav1alpha2.Redpanda) int {
		return cmp.Compare(a.Namespace, b.Namespace)
	})

	return filtered
}

func ensureOneClusterPerNamespace(clusters []redpandav1alpha2.Redpanda) (*redpandav1alpha2.Redpanda, error) {
	notDeleted := 0
	var activeCluster *redpandav1alpha2.Redpanda
	for _, cluster := range clusters {
		if cluster.GetDeletionTimestamp().IsZero() {
			notDeleted++
			if notDeleted > 1 {
				return nil, apierrors.NewBadRequest("only a single Redpanda cluster instance may exist per namespace")
			}
			activeCluster = ptr.To(cluster)
		}
	}
	return activeCluster, nil
}

func toClient(o runtime.Object) client.Object {
	return o.(client.Object)
}

type Object[Obj any] interface {
	client.Object
	GetCluster() string
	*Obj
}

func derefNonNilObjectPointer[Obj any, PObj Object[Obj]](o PObj) Obj {
	return *o
}

func castObjectPointer[Obj any, PObj Object[Obj]](o Obj) PObj {
	var cast PObj = &o
	return cast
}

type ObjectList[Obj, List any] interface {
	client.ObjectList
	GetItems() []Obj
	SetItems([]Obj)
	*List
}

func derefNonNilObjectListPointer[Obj, List any, PList ObjectList[Obj, List]](l PList) List {
	return *l
}

func castObjectListPointer[Obj, List any, PList ObjectList[Obj, List]](l List) PList {
	var cast PList = &l
	return cast
}

type VirtualStorage[Obj, List any, PObj Object[Obj], PList ObjectList[Obj, List]] struct {
	shortNames []string
	resource   schema.GroupResource
	ctl        *kube.Ctl
	backend    backends.Backend[PObj]
}

func NewVirtualStorage[Obj, List any, PObj Object[Obj], PList ObjectList[Obj, List]](shortNames []string, resource schema.GroupResource, backend backends.Backend[PObj], ctl *kube.Ctl) *VirtualStorage[Obj, List, PObj, PList] {
	return &VirtualStorage[Obj, List, PObj, PList]{
		shortNames: shortNames,
		resource:   resource,
		ctl:        ctl,
		backend:    backend,
	}
}

var (
	_ rest.Storage              = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.Getter               = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.Lister               = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.Scoper               = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.Creater              = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.Updater              = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.GracefulDeleter      = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.SingularNameProvider = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
	_ rest.ShortNamesProvider   = (*VirtualStorage[virtualv1alpha1.ShadowLink, virtualv1alpha1.ShadowLinkList, *virtualv1alpha1.ShadowLink, *virtualv1alpha1.ShadowLinkList])(nil)
)

func (s *VirtualStorage[Obj, List, PObj, PList]) Destroy() {}
func (s *VirtualStorage[Obj, List, PObj, PList]) NamespaceScoped() bool {
	return true
}

func (s *VirtualStorage[Obj, List, PObj, PList]) GetSingularName() string {
	return s.resource.Resource
}

func (s *VirtualStorage[Obj, List, PObj, PList]) ShortNames() []string {
	return s.shortNames
}

func (s *VirtualStorage[Obj, List, PObj, PList]) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string", Format: "name", Description: "Name of the Item"},
			{Name: "Namespace", Type: "string", Format: "namespace", Description: "Namespace of the Item"},
		},
	}

	switch v := object.(type) {
	case PList:
		for _, item := range v.GetItems() {
			o := castObjectPointer[Obj, PObj](item)
			row := metav1.TableRow{
				Object: runtime.RawExtension{Object: o},
				Cells:  []any{o.GetName(), o.GetNamespace()},
			}
			table.Rows = append(table.Rows, row)
		}
	case PObj:
		row := metav1.TableRow{
			Object: runtime.RawExtension{Object: v},
			Cells:  []any{v.GetName(), v.GetNamespace()},
		}
		table.Rows = append(table.Rows, row)
	default:
		return nil, errors.New("unknown type")
	}

	return table, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) New() runtime.Object {
	var o Obj
	return castObjectPointer[Obj, PObj](o)
}

func (s *VirtualStorage[Obj, List, PObj, PList]) NewList() runtime.Object {
	var l List
	return castObjectListPointer[Obj, List, PList](l)
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	namespace := namespaceFromContext(ctx)

	cluster, err := s.getActiveCluster(ctx, namespace)
	if err != nil {
		return nil, err
	}

	found, err := s.backend.Read(ctx, cluster, name)
	if err != nil {
		return nil, handleBackendError(err)
	}
	if found == nil {
		return nil, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
	}

	return found, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Create(ctx context.Context, runtimeobject runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	object := runtimeobject.(PObj)

	cluster, err := s.getActiveCluster(ctx, object.GetNamespace())
	if err != nil {
		return nil, err
	}

	found, err := s.backend.Read(ctx, cluster, object.GetName())
	if err != nil {
		return nil, handleBackendError(err)
	}
	if found != nil {
		return nil, apierrors.NewAlreadyExists(s.resource, client.ObjectKeyFromObject(object).String())
	}

	created, err := s.backend.Create(ctx, cluster, object.GetName(), object)
	if err != nil {
		return nil, handleBackendError(err)
	}
	return created, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	namespace := namespaceFromContext(ctx)

	cluster, err := s.getActiveCluster(ctx, namespace)
	if err != nil {
		return nil, false, err
	}

	found, err := s.backend.Read(ctx, cluster, name)
	if err != nil {
		return nil, false, handleBackendError(err)
	}
	if found == nil {
		return nil, true, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
	}

	if err := s.backend.Delete(ctx, cluster, name); err != nil {
		return nil, false, handleBackendError(err)
	}

	return found, true, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	namespace := namespaceFromContext(ctx)

	cluster, err := s.getActiveCluster(ctx, namespace)
	if err != nil {
		return nil, false, err
	}

	found, err := s.backend.Read(ctx, cluster, name)
	if err != nil {
		return nil, false, handleBackendError(err)
	}
	if found == nil {
		return nil, true, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
	}

	runtimeobject, err := objInfo.UpdatedObject(ctx, found)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	updated, err := s.backend.Update(ctx, cluster, name, runtimeobject.(PObj))
	if err != nil {
		return nil, false, handleBackendError(err)
	}

	return updated, false, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	namespace := namespaceFromContext(ctx)

	clusters, err := kube.List[redpandav1alpha2.RedpandaList](ctx, s.ctl, namespace)
	if err != nil {
		return nil, err
	}

	var list List
	plist := castObjectListPointer[Obj, List, PList](list)
	objects := []Obj{}

	for _, cluster := range filterClustersByNamespace(clusters.Items) {
		items, err := s.backend.List(ctx, cluster)
		if err != nil {
			// continue since we don't want a single cluster to kill the list
			continue
		}
		for _, item := range items {
			objects = append(objects, derefNonNilObjectPointer(item))
		}
	}

	plist.SetItems(objects)

	return plist, nil
}

func (s *VirtualStorage[Obj, List, PObj, PList]) getActiveCluster(ctx context.Context, namespace string) (*redpandav1alpha2.Redpanda, error) {
	clusters, err := kube.List[redpandav1alpha2.RedpandaList](ctx, s.ctl, namespace)
	if err != nil {
		return nil, err
	}

	return ensureOneClusterPerNamespace(clusters.Items)
}
