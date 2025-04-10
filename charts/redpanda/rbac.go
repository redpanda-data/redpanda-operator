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

func ClusterRoles(dot *helmette.Dot) []*rbacv1.ClusterRole {
	values := helmette.Unwrap[Values](dot.Values)

	var crs []*rbacv1.ClusterRole
	if cr := SidecarControllersClusterRole(dot); cr != nil {
		crs = append(crs, cr)
	}

	if !values.RBAC.Enabled {
		return crs
	}

	rpkBundleName := fmt.Sprintf("%s-rpk-bundle", Fullname(dot))

	crs = append(crs, []*rbacv1.ClusterRole{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Labels:      FullLabels(dot),
				Annotations: values.ServiceAccount.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"nodes"},
					Verbs:     []string{"get", "list"},
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        rpkBundleName,
				Labels:      FullLabels(dot),
				Annotations: values.ServiceAccount.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
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
		},
	}...)

	return crs
}

func ClusterRoleBindings(dot *helmette.Dot) []*rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	var crbs []*rbacv1.ClusterRoleBinding
	if crb := SidecarControllersClusterRoleBinding(dot); crb != nil {
		crbs = append(crbs, crb)
	}

	if !values.RBAC.Enabled {
		return crbs
	}

	rpkBundleName := fmt.Sprintf("%s-rpk-bundle", Fullname(dot))
	crbs = append(crbs, []*rbacv1.ClusterRoleBinding{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Labels:      FullLabels(dot),
				Annotations: values.ServiceAccount.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     Fullname(dot),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      ServiceAccountName(dot),
					Namespace: dot.Release.Namespace,
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        rpkBundleName,
				Labels:      FullLabels(dot),
				Annotations: values.ServiceAccount.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     rpkBundleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      ServiceAccountName(dot),
					Namespace: dot.Release.Namespace,
				},
			},
		},
	}...)

	return crbs
}

func SidecarControllersClusterRole(dot *helmette.Dot) *rbacv1.ClusterRole {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Statefulset.SideCars.ShouldCreateRBAC() {
		return nil
	}

	sidecarControllerName := fmt.Sprintf("%s-sidecar-controllers", Fullname(dot))
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        sidecarControllerName,
			Labels:      FullLabels(dot),
			Annotations: values.ServiceAccount.Annotations,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{""},
				Resources: []string{"pods"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
			},
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
			},
			{
				Verbs:     []string{"create", "patch"},
				APIGroups: []string{""},
				Resources: []string{"events"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
			},
		},
	}
}

func SidecarControllersClusterRoleBinding(dot *helmette.Dot) *rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Statefulset.SideCars.ShouldCreateRBAC() {
		return nil
	}

	sidecarControllerName := fmt.Sprintf("%s-sidecar-controllers", Fullname(dot))
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        sidecarControllerName,
			Labels:      FullLabels(dot),
			Annotations: values.ServiceAccount.Annotations,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     sidecarControllerName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(dot),
				Namespace: dot.Release.Namespace,
			},
		},
	}
}

func SidecarControllersRole(dot *helmette.Dot) *rbacv1.Role {
	values := helmette.Unwrap[Values](dot.Values)

	sidecarControllerName := fmt.Sprintf("%s-sidecar-controllers", Fullname(dot))
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        sidecarControllerName,
			Namespace:   dot.Release.Namespace,
			Labels:      FullLabels(dot),
			Annotations: values.ServiceAccount.Annotations,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
			},
			{
				Verbs:     []string{"create", "patch"},
				APIGroups: []string{""},
				Resources: []string{"events"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets/status"},
				Verbs:     []string{"patch", "update"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets", "pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
			},
		},
	}
}

func SidecarControllersRoleBinding(dot *helmette.Dot) *rbacv1.RoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	sidecarControllerName := fmt.Sprintf("%s-sidecar-controllers", Fullname(dot))
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        sidecarControllerName,
			Namespace:   dot.Release.Namespace,
			Labels:      FullLabels(dot),
			Annotations: values.ServiceAccount.Annotations,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     sidecarControllerName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(dot),
				Namespace: dot.Release.Namespace,
			},
		},
	}
}
