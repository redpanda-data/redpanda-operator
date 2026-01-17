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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// ShadowLink is a shadow link that exists in a cluster
type ShadowLink struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ShadowLinkSpec   `json:"spec,omitempty"`
	Status ShadowLinkStatus `json:"status,omitempty"`
}

// ShadowLinkSpec defines the desired state of a shadow link.
type ShadowLinkSpec struct {
	ClusterRef ClusterRef `json:"clusterRef"`
}

// ClusterRef is a reference to the cluster where this resource
// exists.
type ClusterRef struct {
	Name string `json:"name"`
}

// ShadowLinkStatus is the status for a shadow link.
type ShadowLinkStatus struct{}

// +kubebuilder:object:root=true

// ShadowLinkList is a list of ShadowLink objects.
type ShadowLinkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ShadowLink `json:"items"`
}

// GetItems returns the underlying ShadowLink items.
func (s *ShadowLinkList) GetItems() []ShadowLink {
	return s.Items
}
