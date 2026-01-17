// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +versionName=v1alpha1
// +groupName=virtual.cluster.redpanda.com
// +k8s:conversion-gen=github.com/redpanda-data/redpanda-operator/operator/api/virtual
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "virtual.cluster.redpanda.com", Version: "v1alpha1"}

	// SchemeBuilder is the scheme builder with scheme init functions to run for this API package
	SchemeBuilder      runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = localSchemeBuilder.AddToScheme
)

func init() {
	localSchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &ShadowLink{}, &ShadowLinkList{})
		metav1.AddToGroupVersion(scheme, GroupVersion)
		return nil
	})
}
