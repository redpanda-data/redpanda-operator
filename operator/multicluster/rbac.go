// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// roles returns Roles across every local pool.
func roles(state *RenderState) []*rbacv1.Role {
	var out []*rbacv1.Role
	for _, pool := range state.inClusterPools {
		out = append(out, rolesForPool(state, pool)...)
	}
	return out
}

// rolesForPool returns Roles for a single local pool. RBAC config (enabled,
// RPKDebugBundle) is per-pool, so each pool gets its own Roles named
// <cluster>-<pool>-<purpose>.
func rolesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.Role {
	if !pool.Spec.RBAC.IsEnabled() {
		return nil
	}

	poolFullname := state.poolFullname(pool)
	var roles []*rbacv1.Role

	// Sidecar role: allows the sidecar to manage leases, get/list/watch pods and statefulsets.
	roles = append(roles, &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sidecar", poolFullname),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	})

	// RPK debug bundle role: allows rpk debug bundle to collect cluster diagnostics.
	if ptr.Deref(pool.Spec.RBAC.RPKDebugBundle, false) {
		roles = append(roles, &rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-rpk-debug-bundle", poolFullname),
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{
						"configmaps", "endpoints", "events", "limitranges",
						"persistentvolumeclaims", "pods", "pods/log",
						"replicationcontrollers", "resourcequotas",
						"serviceaccounts", "services",
					},
					Verbs: []string{"get", "list"},
				},
			},
		})
	}

	return roles
}

// clusterRoles returns ClusterRoles across every local pool.
func clusterRoles(state *RenderState) []*rbacv1.ClusterRole {
	var out []*rbacv1.ClusterRole
	for _, pool := range state.inClusterPools {
		out = append(out, clusterRolesForPool(state, pool)...)
	}
	return out
}

// clusterRolesForPool returns ClusterRoles for a single local pool. RBAC /
// RackAwareness toggles are per-pool. Cluster-scoped objects insert
// <namespace> to disambiguate StretchClusters with the same name in
// different namespaces.
func clusterRolesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.ClusterRole {
	if !pool.Spec.RBAC.IsEnabled() {
		return nil
	}

	poolFullname := state.poolFullname(pool)
	var clusterRoles []*rbacv1.ClusterRole

	// Self-metrics ClusterRole.
	clusterRoles = append(clusterRoles, &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   tplutil.CleanForK8s(fmt.Sprintf("%s-%s-metrics-reader", poolFullname, state.namespace)),
			Labels: state.commonLabels(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			},
		},
	})

	if pool.Spec.RackAwareness.IsEnabled() {
		clusterRoles = append(clusterRoles, &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   tplutil.CleanForK8s(fmt.Sprintf("%s-%s-rack-awareness", poolFullname, state.namespace)),
				Labels: state.commonLabels(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"nodes"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		})
	}

	return clusterRoles
}

// roleBindings returns RoleBindings across every local pool.
func roleBindings(state *RenderState) []*rbacv1.RoleBinding {
	var out []*rbacv1.RoleBinding
	for _, pool := range state.inClusterPools {
		out = append(out, roleBindingsForPool(state, pool)...)
	}
	return out
}

// roleBindingsForPool binds the pool's Roles to the pool's ServiceAccount.
func roleBindingsForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.RoleBinding {
	var roleBindings []*rbacv1.RoleBinding
	for _, role := range rolesForPool(state, pool) {
		roleBindings = append(roleBindings, &rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      role.ObjectMeta.Name,
				Labels:    state.commonLabels(),
				Namespace: state.namespace,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      pool.Spec.GetServiceAccountName(state.poolFullname(pool)),
					Namespace: state.namespace,
				},
			},
		})
	}
	return roleBindings
}

// clusterRoleBindings returns ClusterRoleBindings across every local pool.
func clusterRoleBindings(state *RenderState) []*rbacv1.ClusterRoleBinding {
	var out []*rbacv1.ClusterRoleBinding
	for _, pool := range state.inClusterPools {
		out = append(out, clusterRoleBindingsForPool(state, pool)...)
	}
	return out
}

// clusterRoleBindingsForPool binds the pool's ClusterRoles to the pool's
// ServiceAccount.
func clusterRoleBindingsForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.ClusterRoleBinding {
	var crbs []*rbacv1.ClusterRoleBinding
	for _, clusterRole := range clusterRolesForPool(state, pool) {
		crbs = append(crbs, &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   clusterRole.ObjectMeta.Name,
				Labels: state.commonLabels(),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      pool.Spec.GetServiceAccountName(state.poolFullname(pool)),
					Namespace: state.namespace,
				},
			},
		})
	}
	return crbs
}
