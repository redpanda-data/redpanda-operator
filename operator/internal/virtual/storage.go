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
	"context"
	"errors"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	virtualv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/virtual/v1alpha1"
)

func namespaceFromContext(ctx context.Context) string {
	namespace, ok := apirequest.NamespaceFrom(ctx)
	if !ok {
		return metav1.NamespaceDefault
	}
	return namespace
}

func toClient(o runtime.Object) client.Object {
	return o.(client.Object)
}

type Object[Obj any] interface {
	client.Object
	*Obj
}

func castObjectPointer[Obj any, PObj Object[Obj]](o Obj) PObj {
	var cast PObj = &o
	return cast
}

type ObjectList[Obj, List any] interface {
	client.ObjectList
	GetItems() []Obj
	*List
}

func castObjectListPointer[Obj, List any, PList ObjectList[Obj, List]](l List) PList {
	var cast PList = &l
	return cast
}

type VirtualStorage[Obj, List any, PObj Object[Obj], PList ObjectList[Obj, List]] struct {
	shortNames []string
	resource   schema.GroupResource
	ctl        *kube.Ctl
}

func NewVirtualStorage[Obj, List any, PObj Object[Obj], PList ObjectList[Obj, List]](shortNames []string, resource schema.GroupResource, ctl *kube.Ctl) *VirtualStorage[Obj, List, PObj, PList] {
	return &VirtualStorage[Obj, List, PObj, PList]{
		shortNames: shortNames,
		resource:   resource,
		ctl:        ctl,
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
	return nil, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Create(ctx context.Context, object runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	return nil, apierrors.NewMethodNotSupported(s.resource, client.ObjectKeyFromObject(toClient(object)).String())
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	return nil, true, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
}

func (s *VirtualStorage[Obj, List, PObj, PList]) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	return nil, false, apierrors.NewNotFound(s.resource, namespaceFromContext(ctx)+"/"+name)
}

func (s *VirtualStorage[Obj, List, PObj, PList]) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	return s.NewList(), nil
}
