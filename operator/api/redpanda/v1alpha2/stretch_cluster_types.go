// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// StretchCluster defines the CRD for StretchCluster configuration.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=stretchclusters
// +kubebuilder:resource:shortName=sc
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
type StretchCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec StretchClusterSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status StretchClusterStatus `json:"status,omitempty"`
}

type StretchClusterSpec struct{}

type StretchClusterStatus struct {
	// Conditions holds the conditions for the StretchCluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type StretchClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StretchCluster `json:"items"`
}
