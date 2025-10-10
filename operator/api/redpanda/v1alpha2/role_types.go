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
	"context"
	"encoding/json"
	"strings"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// Role defines the CRD for a Redpanda role.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=roles
// +kubebuilder:resource:shortName=rpr
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Managing ACLs",type="boolean",JSONPath=`.status.managedAcls`
// +kubebuilder:storageversion
type Role struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda role.
	Spec RoleSpec `json:"spec"`
	// Represents the current status of the Redpanda role.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RoleStatus `json:"status,omitempty"`
}

var (
	_ ClusterReferencingObject = (*Role)(nil)
	_ AuthorizedObject         = (*Role)(nil)
)

// GetPrincipal constructs the principal of a Role for defining ACLs.
func (r *Role) GetPrincipal() string {
	return "RedpandaRole:" + r.Name
}

func (r *Role) GetACLs() []ACLRule {
	if r.Spec.Authorization == nil {
		return nil
	}
	return r.Spec.Authorization.ACLs
}

func (r *Role) GetClusterSource() *ClusterSource {
	return r.Spec.ClusterSource
}

func (r *Role) ShouldManageACLs() bool {
	return r.Spec.Authorization != nil
}

func (r *Role) HasManagedACLs() bool {
	return r.Status.ManagedACLs
}

func (r *Role) ShouldManageRole() bool {
	// Always manage the role if it has a spec (similar to how users work)
	return true
}

func (r *Role) HasManagedRole() bool {
	return r.Status.ManagedRole
}

// RoleSpec defines the configuration of a Redpanda role.
type RoleSpec struct {
	// ClusterSource is a reference to the cluster where the role should be created.
	// It is used in constructing the client created to configure a cluster.
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.admin: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.admin)`
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.kafka: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.kafka)`
	// +required
	ClusterSource *ClusterSource `json:"cluster"`
	// Principals defines the list of users assigned to this role inline.
	// Format: Type:Name (e.g., User:john, User:jane). If type is omitted, defaults to User.
	// Can be used together with PrincipalsFrom to combine inline and referenced principals.
	// +kubebuilder:validation:MaxItems=1024
	Principals []string `json:"principals,omitempty"`
	// PrincipalsFrom allows principals to be sourced from a ConfigMap.
	// The referenced ConfigMap must contain a key with a JSON array of principal strings,
	// or newline-separated principal strings.
	// Principals from this source are merged with inline Principals.
	// +optional
	PrincipalsFrom *PrincipalsSource `json:"principalsFrom,omitempty"`
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

// PrincipalsSource contains the source for principals.
type PrincipalsSource struct {
	// ConfigMapRef references a ConfigMap containing principals.
	// The ConfigMap data must contain a key with either:
	// - A JSON array of principal strings: ["User:alice", "User:bob"]
	// - Newline-separated principal strings
	// +required
	ConfigMapRef *corev1.ConfigMapKeySelector `json:"configMapRef"`
}

// FetchPrincipals fetches principals from the ConfigMap reference.
// It supports both JSON array format and newline-separated format.
// Returns an error if the ConfigMap or key is not found.
func (p *PrincipalsSource) FetchPrincipals(ctx context.Context, c client.Client, namespace string) ([]string, error) {
	if p == nil || p.ConfigMapRef == nil {
		return nil, nil
	}

	name := p.ConfigMapRef.LocalObjectReference.Name
	key := p.ConfigMapRef.Key
	if key == "" {
		key = "principals"
	}

	var cm corev1.ConfigMap
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get ConfigMap %s/%s", namespace, name)
	}

	data, ok := cm.Data[key]
	if !ok {
		return nil, errors.Newf("key %q not found in ConfigMap %s/%s", key, namespace, name)
	}

	// Try to parse as JSON array first
	var principals []string
	if err := json.Unmarshal([]byte(data), &principals); err == nil {
		return principals, nil
	}

	// Fall back to newline-separated format
	principals = []string{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			principals = append(principals, line)
		}
	}

	return principals, nil
}

// GetPrincipals returns all principals for this role, merging inline principals with
// principals from ConfigMap if configured. Duplicates are automatically removed.
func (r *RoleSpec) GetPrincipals(ctx context.Context, c client.Client, namespace string) ([]string, error) {
	// Use a map to track unique principals
	seen := make(map[string]struct{})
	var principals []string

	// Add inline principals
	for _, p := range r.Principals {
		if p != "" {
			if _, exists := seen[p]; !exists {
				seen[p] = struct{}{}
				principals = append(principals, p)
			}
		}
	}

	// Add ConfigMap principals
	if r.PrincipalsFrom != nil {
		fromConfigMap, err := r.PrincipalsFrom.FetchPrincipals(ctx, c, namespace)
		if err != nil {
			return nil, err
		}
		for _, p := range fromConfigMap {
			if p != "" {
				if _, exists := seen[p]; !exists {
					seen[p] = struct{}{}
					principals = append(principals, p)
				}
			}
		}
	}

	return principals, nil
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
}

// RoleList contains a list of Redpanda role objects.
// +kubebuilder:object:root=true
type RoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda role resources.
	Items []Role `json:"items"`
}

func (r *RoleList) GetItems() []*Role {
	return functional.MapFn(ptr.To, r.Items)
}
