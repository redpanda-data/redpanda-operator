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

// RedpandaRole defines the CRD for a Redpanda role.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rpr
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Managing ACLs",type="boolean",JSONPath=`.status.managedAcls`
// +kubebuilder:storageversion
type RedpandaRole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda role.
	Spec RoleSpec `json:"spec"`
	// Represents the current status of the Redpanda role.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RoleStatus `json:"status,omitempty"`
}

var (
	_ ClusterReferencingObject = (*RedpandaRole)(nil)
	_ AuthorizedObject         = (*RedpandaRole)(nil)
)

// GetPrincipal constructs the principal of a Role for defining ACLs.
func (r *RedpandaRole) GetPrincipal() string {
	return "RedpandaRole:" + r.Name
}

func (r *RedpandaRole) GetACLs() []ACLRule {
	if r.Spec.Authorization == nil {
		return nil
	}
	return r.Spec.Authorization.ACLs
}

func (r *RedpandaRole) GetClusterSource() *ClusterSource {
	return r.Spec.ClusterSource
}

func (r *RedpandaRole) ShouldManageACLs() bool {
	return r.Spec.Authorization != nil
}

func (r *RedpandaRole) HasManagedACLs() bool {
	return r.Status.ManagedACLs
}

func (r *RedpandaRole) ShouldManageRole() bool {
	// Always manage the role if it has a spec (similar to how users work)
	return true
}

func (r *RedpandaRole) HasManagedRole() bool {
	return r.Status.ManagedRole
}

func (r *RedpandaRole) ShouldManagePrincipals() bool {
	return len(r.Spec.Principals) > 0
}

func (r *RedpandaRole) HasManagedPrincipals() bool {
	return r.Status.ManagedPrincipals
}

// RoleSpec defines the configuration of a Redpanda role.
type RoleSpec struct {
	// ClusterSource is a reference to the cluster where the role should be created.
	// It is used in constructing the client created to configure a cluster.
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.admin: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.admin)`
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.kafka: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.kafka)`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ClusterSource is immutable"
	// +required
	ClusterSource *ClusterSource `json:"cluster"`
	// Principals defines the list of users assigned to this role.
	// Format: Type:Name (e.g., User:john, User:jane). If type is omitted, defaults to User.
	// +kubebuilder:validation:MaxItems=1024
	Principals []string `json:"principals,omitempty"`
	// Authorization rules defined for this role. If specified, the operator will manage ACLs for this role.
	// If omitted, ACLs should be managed separately using Redpanda's ACL management.
	Authorization *RoleAuthorizationSpec `json:"authorization,omitempty"`
}

// RoleAuthorizationSpec defines authorization rules for this role.
type RoleAuthorizationSpec struct {
	// List of ACL rules which should be applied to this role.
	// +kubebuilder:validation:MaxItems=1024
	ACLs []ACLRule `json:"acls,omitempty"`
}

// RoleStatus defines the observed state of a Redpanda role
type RoleStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda role.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ManagedACLs returns whether the role has managed ACLs that need
	// to be cleaned up.
	ManagedACLs bool `json:"managedAcls,omitempty"`
	// ManagedRole returns whether the role has been created in Redpanda and needs
	// to be cleaned up.
	ManagedRole bool `json:"managedRole,omitempty"`
	// ManagedPrincipals returns whether the role has managed principals (membership)
	// that are being reconciled by the operator.
	ManagedPrincipals bool `json:"managedPrincipals,omitempty"`
}

// RedpandaRoleList contains a list of Redpanda role objects.
// +kubebuilder:object:root=true
type RedpandaRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda role resources.
	Items []RedpandaRole `json:"items"`
}

func (r *RedpandaRoleList) GetItems() []*RedpandaRole {
	return functional.MapFn(ptr.To, r.Items)
}
