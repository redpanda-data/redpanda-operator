// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package v1alpha2 defines the v1alpha2 schema for the Redpanda API. It is part of an evolving API architecture, representing an initial stage that may be subject to change based on user feedback and further development.
// +kubebuilder:object:generate=true
// +groupName=cluster.redpanda.com
package v1alpha2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "cluster.redpanda.com", Version: "v1alpha2"}

	// SchemeBuilder is used to add Go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
