// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha3

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func init() {
	SchemeBuilder.Register(&Redpanda{}, &RedpandaList{})
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:skipversion
type Redpanda struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RedpandaSpec   `json:"spec,omitempty"`
	Status RedpandaStatus `json:"status,omitempty"`
}

type RedpandaSpec struct{}

// RedpandaStatus defines the observed state of Redpanda
type RedpandaStatus struct {
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LicenseStatus contains information about the current state of any
	// installed license in the Redpanda cluster.
	// +optional
	LicenseStatus *RedpandaLicenseStatus `json:"license,omitempty"`

	// NodePools contains information about the node pools associated
	// with this cluster.
	// +optional
	NodePools []NodePoolStatus `json:"nodePools,omitempty"`

	// ConfigVersion contains the configuration version written in
	// Redpanda used for restarting broker nodes as necessary.
	// +optional
	ConfigVersion string `json:"configVersion,omitempty"`
}

type RedpandaLicenseStatus struct {
	Violation     bool     `json:"violation"`
	InUseFeatures []string `json:"inUseFeatures"`
	// +optional
	Expired *bool `json:"expired,omitempty"`
	// +optional
	Type *string `json:"type,omitempty"`
	// +optional
	Organization *string `json:"organization,omitempty"`
	// +optional
	Expiration *metav1.Time `json:"expiration,omitempty"`
}

// RedpandaList contains a list of Redpanda objects.
// +kubebuilder:object:root=true
// +kubebuilder:skipversion
type RedpandaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda resources.
	Items []Redpanda `json:"items"`
}
