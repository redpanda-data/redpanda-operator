// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_rbac.go.tpl
package redpanda

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Roles(state *RenderState) []*rbacv1.Role {
	// path of static role definition -> Enabled
	mapping := map[string]bool{
		"files/sidecar.Role.yaml":          state.Values.RBAC.Enabled && state.Values.Statefulset.SideCars.Controllers.CreateRBAC,
		"files/pvcunbinder.Role.yaml":      state.Values.Statefulset.SideCars.ShouldCreateRBAC() && state.Values.Statefulset.SideCars.PVCUnbinderEnabled(),
		"files/decommission.Role.yaml":     state.Values.Statefulset.SideCars.ShouldCreateRBAC() && state.Values.Statefulset.SideCars.BrokerDecommissionerEnabled(),
		"files/rpk-debug-bundle.Role.yaml": state.Values.RBAC.Enabled && state.Values.RBAC.RPKDebugBundle,
	}

	var roles []*rbacv1.Role
	for file, enabled := range helmette.SortedMap(mapping) {
		if !enabled {
			continue
		}

		role := helmette.FromYaml[rbacv1.Role](state.Dot.Files.Get(file))

		// Populated all chart values on the loaded static Role.
		role.ObjectMeta.Name = fmt.Sprintf("%s-%s", Fullname(state), role.ObjectMeta.Name)
		role.ObjectMeta.Namespace = state.Release.Namespace
		role.ObjectMeta.Labels = FullLabels(state)
		role.ObjectMeta.Annotations = helmette.Merge(
			map[string]string{},
			state.Values.ServiceAccount.Annotations, // For backwards compatibility
			state.Values.RBAC.Annotations,
		)

		roles = append(roles, &role)
	}

	return roles
}

func ClusterRoles(state *RenderState) []*rbacv1.ClusterRole {
	// path of static ClusterRole definition -> Enabled
	mapping := map[string]bool{
		"files/pvcunbinder.ClusterRole.yaml":    state.Values.Statefulset.SideCars.ShouldCreateRBAC() && state.Values.Statefulset.SideCars.PVCUnbinderEnabled(),
		"files/decommission.ClusterRole.yaml":   state.Values.Statefulset.SideCars.ShouldCreateRBAC() && state.Values.Statefulset.SideCars.BrokerDecommissionerEnabled(),
		"files/rack-awareness.ClusterRole.yaml": state.Values.RBAC.Enabled && state.Values.RackAwareness.Enabled,
	}

	var clusterRoles []*rbacv1.ClusterRole
	for file, enabled := range helmette.SortedMap(mapping) {
		if !enabled {
			continue
		}

		role := helmette.FromYaml[rbacv1.ClusterRole](state.Dot.Files.Get(file))

		// Populated all chart values on the loaded static Role.
		// For ClusterScoped resources, we include the Namespace to permit
		// installing multiple releases with the same names into the same
		// cluster.
		role.ObjectMeta.Name = cleanForK8s(fmt.Sprintf("%s-%s-%s", Fullname(state), state.Release.Namespace, role.ObjectMeta.Name))
		role.ObjectMeta.Labels = FullLabels(state)
		role.ObjectMeta.Annotations = helmette.Merge(
			map[string]string{},
			state.Values.ServiceAccount.Annotations, // For backwards compatibility
			state.Values.RBAC.Annotations,
		)

		clusterRoles = append(clusterRoles, &role)
	}

	return clusterRoles
}

func RoleBindings(state *RenderState) []*rbacv1.RoleBinding {
	// Rather than worrying about syncing logic, we just generate the list of
	// bindings from the roles that we create.
	var roleBindings []*rbacv1.RoleBinding
	for _, role := range Roles(state) {
		roleBindings = append(roleBindings, &rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      role.ObjectMeta.Name,
				Labels:    FullLabels(state),
				Namespace: state.Release.Namespace,
				Annotations: helmette.Merge(
					map[string]string{},
					state.Values.ServiceAccount.Annotations, // For backwards compatibility
					state.Values.RBAC.Annotations,
				),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      ServiceAccountName(state),
					Namespace: state.Release.Namespace,
				},
			},
		})
	}

	return roleBindings
}

func ClusterRoleBindings(state *RenderState) []*rbacv1.ClusterRoleBinding {
	// Rather than worrying about syncing logic, we just generate the list of
	// bindings from the roles that we create.
	var crbs []*rbacv1.ClusterRoleBinding
	for _, clusterRole := range ClusterRoles(state) {
		crbs = append(crbs, &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterRole.ObjectMeta.Name,
				Labels:    FullLabels(state),
				Namespace: state.Release.Namespace,
				Annotations: helmette.Merge(
					map[string]string{},
					state.Values.ServiceAccount.Annotations, // For backwards compatibility
					state.Values.RBAC.Annotations,
				),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.ObjectMeta.Name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      ServiceAccountName(state),
					Namespace: state.Release.Namespace,
				},
			},
		})
	}

	return crbs
}
