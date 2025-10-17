// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// RedpandaRoleBinding defines the CRD for binding principals to a Redpanda role.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rprb
// +kubebuilder:printcolumn:name="Role",type="string",JSONPath=`.spec.roleRef.name`
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description=""
type RedpandaRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the RoleBinding.
	Spec RedpandaRoleBindingSpec `json:"spec"`
	// Represents the current status of the RoleBinding.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RedpandaRoleBindingStatus `json:"status,omitempty"`
}

// RedpandaRoleBindingSpec defines the configuration of a RoleBinding.
type RedpandaRoleBindingSpec struct {
	// RoleRef references the Role this binding applies to.
	// The referenced Role must exist in the same namespace.
	// +required
	RoleRef RoleRef `json:"roleRef"`

	// Principals defines the list of users assigned to the referenced role.
	// At least one principal is required. A RoleBinding without principals serves no purpose.
	// Format: Type:Name (e.g., User:john, User:jane). If type is omitted, defaults to User.
	// These principals are merged with any principals from the Role's own principals field.
	// Only "User:" prefix or no prefix is currently supported.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1024
	// +kubebuilder:validation:items:Pattern=`^(User:.+|[^:]+)$`
	// +required
	Principals []string `json:"principals,omitempty"`
}

// RoleRef references a Role by name.
type RoleRef struct {
	// Name of the Role.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// RedpandaRoleBindingStatus defines the observed state of a RoleBinding
type RedpandaRoleBindingStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the RoleBinding.
	// Supported condition type:
	// - "Synced": Indicates whether the RoleBinding's principals have been validated
	//   and synced to the referenced Role. This will be False if the Role doesn't exist
	//   or if principals haven't been synced yet.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RedpandaRoleBindingList contains a list of RoleBinding objects.
// +kubebuilder:object:root=true
type RedpandaRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of RoleBinding resources.
	Items []RedpandaRoleBinding `json:"items"`
}

func (r *RedpandaRoleBindingList) GetItems() []*RedpandaRoleBinding {
	return functional.MapFn(ptr.To, r.Items)
}
