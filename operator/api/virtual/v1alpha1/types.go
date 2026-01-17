// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ShadowLink is a shadow link that exists in a cluster
//
// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
// +genclient
type ShadowLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowLinkSpec   `json:"spec,omitempty"`
	Status ShadowLinkStatus `json:"status,omitempty"`
}

// GetCluster returns the name of the undelrying cluster.
func (s *ShadowLink) GetCluster() string {
	return s.Spec.ClusterRef.Name
}

// ShadowLinkSpec defines the desired state of a shadow link.
//
// +k8s:openapi-gen=true
type ShadowLinkSpec struct {
	ClusterRef ClusterRef `json:"clusterRef"`
}

// ClusterRef is a reference to the cluster where this resource
// exists.
//
// +k8s:openapi-gen=true
type ClusterRef struct {
	Name string `json:"name"`
}

// ShadowLinkStatus is the status for a shadow link.
//
// +k8s:openapi-gen=true
type ShadowLinkStatus struct{}

// ShadowLinkList is a list of ShadowLink objects.
//
// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
type ShadowLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShadowLink `json:"items"`
}

// GetItems returns the underlying ShadowLink items.
func (s *ShadowLinkList) GetItems() []ShadowLink {
	return s.Items
}

// SetItems sets the underlying ShadowLink items.
func (s *ShadowLinkList) SetItems(items []ShadowLink) {
	s.Items = items
}
