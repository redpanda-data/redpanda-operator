// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"github.com/cockroachdb/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func ListFor(scheme *runtime.Scheme, obj Object) (ObjectList, error) {
	gvk, err := GVKFor(scheme, obj)
	if err != nil {
		return nil, err
	}

	olist, err := scheme.New(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	list, ok := olist.(ObjectList)
	if !ok {
		return nil, errors.Newf("type is not ObjectList: %T", obj)
	}

	return list, nil
}

func GVKFor(scheme *runtime.Scheme, object Object) (schema.GroupVersionKind, error) {
	kinds, _, err := scheme.ObjectKinds(object)
	if err != nil {
		return schema.GroupVersionKind{}, errors.WithStack(err)
	}

	if len(kinds) == 0 {
		return schema.GroupVersionKind{}, errors.Newf("unable to determine object kind: %T", object)
	}

	return kinds[0], nil
}

func setGVK(scheme *runtime.Scheme, obj Object) error {
	gvk, err := GVKFor(scheme, obj)
	if err != nil {
		return err
	}

	obj.GetObjectKind().SetGroupVersionKind(gvk)

	return nil
}
