// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_rbac.go.tpl
package operator

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func ClusterRoles(dot *helmette.Dot) []rbacv1.ClusterRole {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	clusterRoles := []rbacv1.ClusterRole{
		// Unused convenience ClusterRole that consumers may bind to for access to /metrics.
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "metrics-reader"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:           []string{"get"},
					NonResourceURLs: []string{"/metrics"},
				},
			},
		},
	}

	if values.Scope == Cluster {
		return append(clusterRoles, []rbacv1.ClusterRole{
			{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRole",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        Fullname(dot),
					Labels:      Labels(dot),
					Annotations: values.Annotations,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"events", "secrets", "serviceaccounts", "services"},
					},
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"nodes"},
					},
					{
						Verbs:     []string{"delete", "get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumeclaims"},
					},
					{
						Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					{
						Verbs:     []string{"patch", "update"},
						APIGroups: []string{""},
						Resources: []string{"pods/finalizers", "pods/status"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"apps"},
						Resources: []string{"deployments", "statefulsets"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"cert-manager.io"},
						Resources: []string{"certificates", "clusterissuers", "issuers"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"networking.k8s.io"},
						Resources: []string{"ingresses"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"policy"},
						Resources: []string{"poddisruptionbudgets"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"rbac.authorization.k8s.io"},
						Resources: []string{"clusterroles", "clusterrolebindings"},
					},
					{
						Verbs:     []string{"delete", "get", "list", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"policy"},
						Resources: []string{"poddisruptionbudgets"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters", "consoles"},
					},
					{
						Verbs:     []string{"update", "patch"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters/finalizers", "consoles/finalizers"},
					},
					{
						Verbs:     []string{"get", "patch", "update"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters/status", "consoles/status"},
					},
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{"scheduling.k8s.io"},
						Resources: []string{"priorityclasses"},
					},
					// Permissions for the PVCUnbinder
					{
						Verbs:     []string{"get", "list", "patch", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumes"},
					},
					// Permissions for RBAC on /metrics
					{
						Verbs:     []string{"create"},
						APIGroups: []string{"authentication.k8s.io"},
						Resources: []string{"tokenreviews"},
					},
					{
						Verbs:     []string{"create"},
						APIGroups: []string{"authorization.k8s.io"},
						Resources: []string{"subjectaccessreviews"},
					},
				},
			},
		}...)
	}

	if values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs {
		clusterRoles = append(clusterRoles, []rbacv1.ClusterRole{
			{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRole",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
					Labels:      Labels(dot),
					Annotations: values.Annotations,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"nodes"},
					},
					{
						Verbs:     []string{"get", "list", "patch", "update", "watch", "delete"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumes"},
					},
					// Read-Only access to Secrets and Configmaps is required for the NodeWatcher
					// controller to work appropriately due to the usage of helm to retrieve values.
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"secrets", "configmaps"},
					},
					{
						Verbs:     []string{"get", "list", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumes"},
					},
					// HACK / REMOVE ME SOON: This false set of permissions is here to be in sync
					// with the redpanda chart. They are all superfluous.
					{
						Verbs:     []string{"create", "patch"},
						APIGroups: []string{""},
						Resources: []string{"events"},
					},
					{
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					{
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
						APIGroups: []string{"coordination.k8s.io"},
						Resources: []string{"leases"},
					},
					{
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
						APIGroups: []string{"apps"},
						Resources: []string{"statefulsets"},
					},
					{
						Verbs:     []string{"get", "list", "watch", "update", "patch", "delete"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumeclaims", "persistentvolumes"},
					},
				},
			},
		}...)
	}

	// HACK / REMOVE ME SOON: This false set of permissions is here to be in sync
	// with the redpanda chart. They are all superfluous.
	if values.RBAC.CreateRPKBundleCRs {
		clusterRoles = append(clusterRoles, rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list"},
					APIGroups: []string{""},
					Resources: []string{"nodes", "configmaps", "endpoints", "events", "limitranges", "persistentvolumeclaims", "pods", "pods/log", "replicationcontrollers", "resourcequotas", "serviceaccounts", "services"},
				},
			},
		})
	}

	return append(clusterRoles, rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        Fullname(dot),
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Rules: append(
			v2CRDRules(),
			rbacv1.PolicyRule{
				Verbs:     []string{"create", "get", "delete", "list", "patch", "update", "watch"},
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"clusterrolebindings", "clusterroles"},
			},
			// Permissions for RBAC on /metrics
			rbacv1.PolicyRule{
				Verbs:     []string{"create"},
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
			},
			rbacv1.PolicyRule{
				Verbs:     []string{"create"},
				APIGroups: []string{"authorization.k8s.io"},
				Resources: []string{"subjectaccessreviews"},
			},
		),
	})
}

func ClusterRoleBindings(dot *helmette.Dot) []rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	bindings := []rbacv1.ClusterRoleBinding{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
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
	}

	// HACK / REMOVE ME SOON: This false set of permissions is here to be in sync
	// with the redpanda chart. They are all superfluous.
	if values.RBAC.CreateRPKBundleCRs {
		bindings = append(bindings, rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
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

	if values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs {
		bindings = append(bindings, rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
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

	return bindings
}

func Roles(dot *helmette.Dot) []rbacv1.Role {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	roles := []rbacv1.Role{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "election-role"),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
				},
				{
					Verbs:     []string{"create", "patch"},
					APIGroups: []string{""},
					Resources: []string{"events"},
				},
				{
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					APIGroups: []string{"coordination.k8s.io"},
					Resources: []string{"leases"},
				},
			},
		},
	}

	if values.Scope == Namespace {
		roles = append(roles, rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"autoscaling"},
					Resources: []string{"horizontalpodautoscalers"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"pods"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
				},
				{
					Verbs:     []string{"list", "watch", "create", "delete", "get", "patch", "update"},
					APIGroups: []string{"apps"},
					Resources: []string{"statefulsets"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"batch"},
					Resources: []string{"jobs"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "patch", "update", "list", "watch"},
					APIGroups: []string{"cert-manager.io"},
					Resources: []string{"certificates"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "patch", "update", "list", "watch"},
					APIGroups: []string{"cert-manager.io"},
					Resources: []string{"issuers"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"coordination.k8s.io"},
					Resources: []string{"leases"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
				},
				{
					Verbs:     []string{"create", "patch"},
					APIGroups: []string{""},
					Resources: []string{"events"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"secrets"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"serviceaccounts"},
				},
				{
					Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"pods"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"services"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"monitoring.coreos.com"},
					Resources: []string{"servicemonitors", "podmonitors"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"networking.k8s.io"},
					Resources: []string{"ingresses"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"policy"},
					Resources: []string{"poddisruptionbudgets"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"rolebindings"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"rbac.authorization.k8s.io"},
					Resources: []string{"roles"},
				},
				// HACK / REMOVE ME SOON: This false set of permissions is here to be in sync
				// with the redpanda chart. They are all superfluous.
				{
					Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"persistentvolumeclaims"},
				},
				{
					Verbs:     []string{"patch", "update"},
					APIGroups: []string{"apps"},
					Resources: []string{"statefulsets/status"},
				},
			},
		})

		if values.RBAC.CreateAdditionalControllerCRs {
			roles = append(roles, rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        Fullname(dot) + "-additional-controllers",
					Namespace:   dot.Release.Namespace,
					Labels:      Labels(dot),
					Annotations: values.Annotations,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumeclaims"},
					},
					{
						Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumeclaims"},
					},
					{
						Verbs:     []string{"patch", "update"},
						APIGroups: []string{""},
						Resources: []string{"pods/status"},
					},
					{
						Verbs:     []string{"patch", "update"},
						APIGroups: []string{"apps"},
						Resources: []string{"statefulsets/status"},
					},
				},
			})
		}

		if values.RBAC.CreateRPKBundleCRs {
			roles = append(roles, rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
					Labels:      Labels(dot),
					Annotations: values.Annotations,
				},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"configmaps", "endpoints", "events", "limitranges", "persistentvolumeclaims", "pods", "pods/log", "replicationcontrollers", "resourcequotas", "serviceaccounts", "services"},
					},
				},
			})
		}

	} else if values.Scope == Cluster {
		roles = append(roles, rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"delete", "get", "list", "watch"},
					APIGroups: []string{""},
					Resources: []string{"persistentvolumeclaims", "pods"},
				},
			},
		})
	}

	return roles
}

func RoleBindings(dot *helmette.Dot) []rbacv1.RoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	binding := []rbacv1.RoleBinding{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "election-rolebinding"),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      ServiceAccountName(dot),
					Namespace: dot.Release.Namespace,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "election-role"),
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        Fullname(dot),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
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
	}

	if values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs {
		binding = append(binding, rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
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

	if values.Scope == Namespace && values.RBAC.CreateRPKBundleCRs {
		binding = append(binding, rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
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

	return binding
}

func v2CRDRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"redpandas"},
		},
		{
			Verbs:     []string{"update"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"redpandas/finalizers"},
		},
		{
			Verbs:     []string{"get", "patch", "update"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"redpandas/status"},
		},
		{
			Verbs:     []string{"get", "list", "patch", "update", "watch"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"schemas", "topics", "users"},
		},
		{
			Verbs:     []string{"update"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"schemas/finalizers", "topics/finalizers", "users/finalizers"},
		},
		{
			Verbs:     []string{"get", "patch", "update"},
			APIGroups: []string{"cluster.redpanda.com"},
			Resources: []string{"schemas/status", "topics/status", "users/status"},
		},
	}
}
