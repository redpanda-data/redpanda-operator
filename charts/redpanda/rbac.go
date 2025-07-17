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

func Roles(dot *helmette.Dot) []*rbacv1.Role {
	values := helmette.Unwrap[Values](dot.Values)

	// path of static role definition -> Enabled
	mapping := map[string]bool{
		"files/sidecar.Role.yaml":          values.RBAC.Enabled && values.Statefulset.SideCars.Controllers.CreateRBAC,
		"files/pvcunbinder.Role.yaml":      values.Statefulset.SideCars.ShouldCreateRBAC() && values.Statefulset.SideCars.PVCUnbinderEnabled(),
		"files/decommission.Role.yaml":     values.Statefulset.SideCars.ShouldCreateRBAC() && values.Statefulset.SideCars.BrokerDecommissionerEnabled(),
		"files/rpk-debug-bundle.Role.yaml": values.RBAC.Enabled && values.RBAC.RPKDebugBundle,
	}

	var roles []*rbacv1.Role
	for file, enabled := range helmette.SortedMap(mapping) {
		if !enabled {
			continue
		}

		role := helmette.FromYaml[rbacv1.Role](dot.Files.Get(file))

		// Populated all chart values on the loaded static Role.
		role.ObjectMeta.Name = fmt.Sprintf("%s-%s", Fullname(dot), role.ObjectMeta.Name)
		role.ObjectMeta.Namespace = dot.Release.Namespace
		role.ObjectMeta.Labels = FullLabels(dot)
		role.ObjectMeta.Annotations = helmette.Merge(
			map[string]string{},
			values.ServiceAccount.Annotations, // For backwards compatibility
			values.RBAC.Annotations,
		)

		roles = append(roles, &role)
	}

	return roles
}

func ClusterRoles(dot *helmette.Dot) []*rbacv1.ClusterRole {
	values := helmette.Unwrap[Values](dot.Values)

	// path of static ClusterRole definition -> Enabled
	mapping := map[string]bool{
		"files/pvcunbinder.ClusterRole.yaml":    values.Statefulset.SideCars.ShouldCreateRBAC() && values.Statefulset.SideCars.PVCUnbinderEnabled(),
		"files/decommission.ClusterRole.yaml":   values.Statefulset.SideCars.ShouldCreateRBAC() && values.Statefulset.SideCars.BrokerDecommissionerEnabled(),
		"files/rack-awareness.ClusterRole.yaml": values.RBAC.Enabled && values.RackAwareness.Enabled,
	}

	var clusterRoles []*rbacv1.ClusterRole
	for file, enabled := range helmette.SortedMap(mapping) {
		if !enabled {
			continue
		}

		role := helmette.FromYaml[rbacv1.ClusterRole](dot.Files.Get(file))

		// Populated all chart values on the loaded static Role.
		// For ClusterScoped resources, we include the Namespace to permit
		// installing multiple releases with the same names into the same
		// cluster.
		role.ObjectMeta.Name = cleanForK8s(fmt.Sprintf("%s-%s-%s", Fullname(dot), dot.Release.Namespace, role.ObjectMeta.Name))
		role.ObjectMeta.Labels = FullLabels(dot)
		role.ObjectMeta.Annotations = helmette.Merge(
			map[string]string{},
			values.ServiceAccount.Annotations, // For backwards compatibility
			values.RBAC.Annotations,
		)

		clusterRoles = append(clusterRoles, &role)
	}

	return clusterRoles
}

func RoleBindings(dot *helmette.Dot) []*rbacv1.RoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	// Rather than worrying about syncing logic, we just generate the list of
	// bindings from the roles that we create.
	var roleBindings []*rbacv1.RoleBinding
	for _, role := range Roles(dot) {
		roleBindings = append(roleBindings, &rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      role.ObjectMeta.Name,
				Labels:    FullLabels(dot),
				Namespace: dot.Release.Namespace,
				Annotations: helmette.Merge(
					map[string]string{},
					values.ServiceAccount.Annotations, // For backwards compatibility
					values.RBAC.Annotations,
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
					Name:      ServiceAccountName(dot),
					Namespace: dot.Release.Namespace,
				},
			},
		})
	}

	return roleBindings
}

func ClusterRoleBindings(dot *helmette.Dot) []*rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	// Rather than worrying about syncing logic, we just generate the list of
	// bindings from the roles that we create.
	var crbs []*rbacv1.ClusterRoleBinding
	for _, clusterRole := range ClusterRoles(dot) {
		crbs = append(crbs, &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterRole.ObjectMeta.Name,
				Labels:    FullLabels(dot),
				Namespace: dot.Release.Namespace,
				Annotations: helmette.Merge(
					map[string]string{},
					values.ServiceAccount.Annotations, // For backwards compatibility
					values.RBAC.Annotations,
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
					Name:      ServiceAccountName(dot),
					Namespace: dot.Release.Namespace,
				},
			},
		})
	}

	return crbs
}
