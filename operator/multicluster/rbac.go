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

	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// roles returns all Roles for the given RenderState.
func roles(state *RenderState) []*rbacv1.Role {
	if !state.Spec().RBAC.IsEnabled() {
		return nil
	}

	var roles []*rbacv1.Role

	// Sidecar role: allows the sidecar to manage leases, get/list/watch pods and statefulsets.
	roles = append(roles, &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sidecar", state.fullname()),
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
	if ptr.Deref(state.Spec().RBAC.RPKDebugBundle, false) {
		roles = append(roles, &rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-rpk-debug-bundle", state.fullname()),
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

// clusterRoles returns all ClusterRoles for the given RenderState.
func clusterRoles(state *RenderState) []*rbacv1.ClusterRole {
	if !state.Spec().RBAC.IsEnabled() {
		return nil
	}

	var clusterRoles []*rbacv1.ClusterRole

	if state.Spec().RackAwareness.IsEnabled() {
		clusterRoles = append(clusterRoles, &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   tplutil.CleanForK8s(fmt.Sprintf("%s-%s-rack-awareness", state.fullname(), state.namespace)),
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

// roleBindings returns all RoleBindings for the given RenderState.
func roleBindings(state *RenderState) []*rbacv1.RoleBinding {
	var roleBindings []*rbacv1.RoleBinding
	for _, role := range roles(state) {
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
					Name:      state.Spec().GetServiceAccountName(state.fullname()),
					Namespace: state.namespace,
				},
			},
		})
	}
	return roleBindings
}

// clusterRoleBindings returns all ClusterRoleBindings for the given RenderState.
func clusterRoleBindings(state *RenderState) []*rbacv1.ClusterRoleBinding {
	var crbs []*rbacv1.ClusterRoleBinding
	for _, clusterRole := range clusterRoles(state) {
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
					Name:      state.Spec().GetServiceAccountName(state.fullname()),
					Namespace: state.namespace,
				},
			},
		})
	}
	return crbs
}
