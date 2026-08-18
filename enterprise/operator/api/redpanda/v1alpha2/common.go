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
	"k8s.io/utils/ptr"
)

// NOTE: the types in this file are forked from the OSS operator's
// redpanda/v1alpha2 package (common.go). Definitions, kubebuilder markers,
// json tags, and doc comments are copied verbatim so the generated CRD
// schemas remain byte-identical.

// ClusterRef represents a reference to a cluster that is being targeted.
type ClusterRef struct {
	// Group is used to override the object group that this reference points to.
	// If unspecified, defaults to "cluster.redpanda.com".
	// A bare API group only — controllers match it by string comparison, so a
	// "group/version" value (e.g. "cluster.redpanda.com/v1alpha2") would
	// silently match nothing and the referencing object would never bind.
	// +kubebuilder:validation:XValidation:message="group must be a bare API group without a version suffix (e.g. cluster.redpanda.com, not cluster.redpanda.com/v1alpha2)",rule="!self.contains('/')"
	Group *string `json:"group,omitempty"`
	// Kind is used to override the object kind that this reference points to.
	// If unspecified, defaults to "Redpanda".
	Kind *string `json:"kind,omitempty"`
	// Name specifies the name of the cluster being referenced.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace specifies the namespace of the cluster being referenced.
	// If unspecified, defaults to the namespace of the referencing object.
	// Setting this allows referencing a cluster that resides in a different
	// namespace, e.g. a ShadowLink whose source and shadow Redpanda clusters
	// live in separate namespaces on the same Kubernetes cluster.
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}

const (
	v2ClusterRefGroup     = "cluster.redpanda.com"
	v2ClusterRefKind      = "Redpanda"
	StretchClusterRefKind = "StretchCluster"
)

func (c *ClusterRef) GetGroup() string {
	return ptr.Deref(c.Group, v2ClusterRefGroup)
}

func (c *ClusterRef) GetKind() string {
	return ptr.Deref(c.Kind, v2ClusterRefKind)
}

func (c *ClusterRef) IsStretchCluster() bool {
	return c.GetGroup() == v2ClusterRefGroup && c.GetKind() == StretchClusterRefKind
}
