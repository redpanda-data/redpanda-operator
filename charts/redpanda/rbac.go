// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// maxNameLength is the Kubernetes name limit, which is also the DNS label
// limit.
const maxNameLength = 63

// RoleSet is the RBAC generator for a Redpanda deployment.
type RoleSet struct {
	// Prefix leads every object name.
	Prefix    string
	Namespace string

	Labels      map[string]string
	Annotations map[string]string

	// ServiceAccount is the name the bindings' subject points at. Resolved,
	// not derived: the chart falls back to "default", the operator to its
	// per-pool fullname.
	ServiceAccount string

	Roles        Roles
	ClusterRoles ClusterRoles
}

type Roles struct {
	Decommission   bool
	PVCUnbinder    bool
	RPKDebugBundle bool
	Sidecar        bool
}

type ClusterRoles struct {
	Decommission         bool
	MetricsReader        bool
	PVCUnbinder          bool
	RackAwareness        bool
	StretchRackAwareness bool
}

// roleToggles and clusterRoleToggles key the toggles by catalog name. This is
// the only place those names are spelled outside the catalog itself, so
// callers deal in fields; TestToggleFieldsCoverCatalog holds the two key sets
// equal.
func (r *RoleSet) roleToggles() map[string]bool {
	return map[string]bool{
		"decommission":     r.Roles.Decommission,
		"pvcunbinder":      r.Roles.PVCUnbinder,
		"rpk-debug-bundle": r.Roles.RPKDebugBundle,
		"sidecar":          r.Roles.Sidecar,
	}
}

func (r *RoleSet) clusterRoleToggles() map[string]bool {
	return map[string]bool{
		"decommission":           r.ClusterRoles.Decommission,
		"metrics-reader":         r.ClusterRoles.MetricsReader,
		"pvcunbinder":            r.ClusterRoles.PVCUnbinder,
		"rack-awareness":         r.ClusterRoles.RackAwareness,
		"stretch-rack-awareness": r.ClusterRoles.StretchRackAwareness,
	}
}

// RoleName is the namespaced name of a catalog entry.
func (r *RoleSet) RoleName(name string) string {
	return fmt.Sprintf("%s-%s", r.Prefix, name)
}

// ClusterRoleName is the cluster-scoped name of a catalog entry. The namespace
// is folded in so multiple releases with the same name can be installed into
// one cluster.
func (r *RoleSet) ClusterRoleName(name string) string {
	return cleanForK8s(fmt.Sprintf("%s-%s-%s", r.Prefix, r.Namespace, name))
}

// Render returns the Roles, ClusterRoles, RoleBindings, and
// ClusterRoleBindings required for a redpanda deployment.
// each group in catalog-key order.
func (r *RoleSet) Render() []kube.Object {
	var objs []kube.Object

	for _, obj := range r.renderRoles() {
		objs = append(objs, obj)
	}

	for _, obj := range r.renderClusterRoles() {
		objs = append(objs, obj)
	}

	for _, obj := range r.renderRoleBindings() {
		objs = append(objs, obj)
	}

	for _, obj := range r.renderClusterRoleBindings() {
		objs = append(objs, obj)
	}

	return objs
}

func (r *RoleSet) renderRoles() []*rbacv1.Role {
	catalog := roleRules()
	toggles := r.roleToggles()

	var roles []*rbacv1.Role
	for _, name := range slices.Sorted(maps.Keys(toggles)) {
		if !toggles[name] {
			continue
		}

		roles = append(roles, &rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        r.RoleName(name),
				Namespace:   r.Namespace,
				Labels:      r.Labels,
				Annotations: r.Annotations,
			},
			Rules: catalog[name],
		})
	}

	return roles
}

func (r *RoleSet) renderClusterRoles() []*rbacv1.ClusterRole {
	catalog := clusterRoleRules()
	toggles := r.clusterRoleToggles()

	var clusterRoles []*rbacv1.ClusterRole
	for _, name := range slices.Sorted(maps.Keys(toggles)) {
		if !toggles[name] {
			continue
		}

		clusterRoles = append(clusterRoles, &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        r.ClusterRoleName(name),
				Labels:      r.Labels,
				Annotations: r.Annotations,
			},
			Rules: catalog[name],
		})
	}

	return clusterRoles
}

// renderRoleBindings and renderClusterRoleBindings derive a binding per role
// emitted, so there's no second enablement gate to keep in sync.
func (r *RoleSet) renderRoleBindings() []*rbacv1.RoleBinding {
	var roleBindings []*rbacv1.RoleBinding
	for _, role := range r.renderRoles() {
		roleBindings = append(roleBindings, &rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        role.ObjectMeta.Name,
				Namespace:   r.Namespace,
				Labels:      r.Labels,
				Annotations: r.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      r.ServiceAccount,
					Namespace: r.Namespace,
				},
			},
		})
	}

	return roleBindings
}

func (r *RoleSet) renderClusterRoleBindings() []*rbacv1.ClusterRoleBinding {
	var crbs []*rbacv1.ClusterRoleBinding
	for _, clusterRole := range r.renderClusterRoles() {
		crbs = append(crbs, &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterRole.ObjectMeta.Name,
				Labels:      r.Labels,
				Annotations: r.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      r.ServiceAccount,
					Namespace: r.Namespace,
				},
			},
		})
	}

	return crbs
}

// ServiceAccount is the ServiceAccount a [RoleSet]'s bindings references.
func ServiceAccount(name, namespace string, labels, annotations map[string]string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}

// roleRules is the catalog of Roles a redpanda deployment MAY require. It's a
// hand coded version of what is generated from our controllers' markers.
func roleRules() map[string][]rbacv1.PolicyRule {
	return map[string][]rbacv1.PolicyRule{
		"decommission": {
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"delete", "get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
		"pvcunbinder": {
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims", "pods"},
				Verbs:     []string{"delete", "get", "list", "watch"},
			},
		},
		"rpk-debug-bundle": {
			{
				APIGroups: []string{""},
				Resources: []string{
					"configmaps",
					"endpoints",
					"events",
					"limitranges",
					"persistentvolumeclaims",
					"pods",
					"pods/log",
					"replicationcontrollers",
					"resourcequotas",
					"serviceaccounts",
					"services",
				},
				Verbs: []string{"get", "list"},
			},
		},
		"sidecar": {
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
		},
	}
}

// clusterRoleRules is the catalog of ClusterRoles a redpanda deployment MAY
// require. It's a hand coded version of what is generated from our
// controllers' markers.
func clusterRoleRules() map[string][]rbacv1.PolicyRule {
	return map[string][]rbacv1.PolicyRule{
		"decommission": {
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"patch"},
			},
		},
		"metrics-reader": {
			{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			},
		},
		"pvcunbinder": {
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"get", "list", "patch", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"list", "watch"},
			},
			{
				APIGroups: []string{"cluster.redpanda.com"},
				Resources: []string{"redpandas", "stretchclusters"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"events.k8s.io"},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
			{
				APIGroups: []string{"redpanda.vectorized.io"},
				Resources: []string{"clusters"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"storageclasses"},
				Verbs:     []string{"get"},
			},
		},
		"rack-awareness": {
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get"},
			},
		},
		"stretch-rack-awareness": {
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

// cleanForK8s truncates to the Kubernetes name limit and trims the trailing
// hyphen a cut can leave behind.
func cleanForK8s(in string) string {
	if len(in) > maxNameLength {
		in = in[:maxNameLength]
	}
	return strings.TrimSuffix(in, "-")
}
