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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// Group defines the CRD for managing ACLs for an OIDC group in Redpanda.
// Groups are external identities sourced from OIDC identity providers via JWT token
// claims. Unlike users, groups are not created in Redpanda — they exist externally.
// This CRD allows the operator to manage Kafka ACLs for a group principal.
// Group-based authorization must be enabled in Redpanda (enterprise feature).
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=grp
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:storageversion
type Group struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda group.
	Spec GroupSpec `json:"spec"`
	// Represents the current status of the Redpanda group.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status GroupStatus `json:"status,omitempty"`
}

var (
	_ ClusterReferencingObject = (*Group)(nil)
	_ AuthorizedObject         = (*Group)(nil)
)

// GetPrincipal constructs the principal of a Group for defining ACLs.
func (g *Group) GetPrincipal() string {
	return "Group:" + g.Name
}

func (g *Group) GetACLs() []ACLRule {
	if g.Spec.Authorization == nil {
		return nil
	}
	return g.Spec.Authorization.ACLs
}

func (g *Group) GetClusterSource() *ClusterSource {
	return g.Spec.ClusterSource
}

// GroupSpec defines the configuration of a Redpanda group.
type GroupSpec struct {
	// ClusterSource is a reference to the cluster where the group's ACLs should be managed.
	// It is used in constructing the client created to configure a cluster.
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.kafka: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.kafka)`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ClusterSource is immutable"
	// +required
	ClusterSource *ClusterSource `json:"cluster"`
	// Authorization rules defined for this group. The operator always manages ACLs
	// for the group principal. When omitted or empty, any existing ACLs for this
	// group are removed.
	Authorization *GroupAuthorizationSpec `json:"authorization,omitempty"`
}

// GroupAuthorizationSpec defines authorization rules for this group.
type GroupAuthorizationSpec struct {
	// List of ACL rules which should be applied to this group.
	// +kubebuilder:validation:MaxItems=1024
	// +kubebuilder:validation:XValidation:rule="self.all(acl, acl.resource.type != 'subject' && acl.resource.type != 'registry')",message="Schema Registry resource types (subject, registry) are not supported for Group ACLs"
	ACLs []ACLRule `json:"acls,omitempty"`
}

// GroupStatus defines the observed state of a Redpanda group.
type GroupStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda group.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GroupList contains a list of Redpanda group objects.
// +kubebuilder:object:root=true
type GroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda group resources.
	Items []Group `json:"items"`
}

func (g *GroupList) GetItems() []*Group {
	return functional.MapFn(ptr.To, g.Items)
}
