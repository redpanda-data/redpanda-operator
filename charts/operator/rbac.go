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
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "proxy-role"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
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
				Rules: append([]rbacv1.PolicyRule{
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"autoscaling"},
						Resources: []string{"horizontalpodautoscalers"},
					},
					{
						Verbs:     []string{"delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"persistentvolumes"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"apps"},
						Resources: []string{"statefulsets"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"cert-manager.io"},
						Resources: []string{"certificates", "issuers"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"cert-manager.io"},
						Resources: []string{"clusterissuers"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"events"},
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
						Verbs:     []string{"delete", "get", "list", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					{
						Verbs:     []string{"update"},
						APIGroups: []string{""},
						Resources: []string{"pods/finalizers"},
					},
					{
						Verbs:     []string{"patch", "update"},
						APIGroups: []string{""},
						Resources: []string{"pods/status"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"secrets"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"serviceaccounts"},
					},
					{
						Verbs:     []string{"create", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{""},
						Resources: []string{"services"},
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
						Resources: []string{"clusterrolebindings", "clusterroles"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters"},
					},
					{
						Verbs:     []string{"update"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters/finalizers"},
					},
					{
						Verbs:     []string{"get", "patch", "update"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"clusters/status"},
					},
					{
						Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"consoles"},
					},
					{
						Verbs:     []string{"update"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"consoles/finalizers"},
					},
					{
						Verbs:     []string{"get", "patch", "update"},
						APIGroups: []string{"redpanda.vectorized.io"},
						Resources: []string{"consoles/status"},
					},
				}, v2CRDRules()...),
			},
		}...)
	}

	if values.Scope == Namespace && values.RBAC.CreateRPKBundleCRs {
		clusterRoles = append(clusterRoles, []rbacv1.ClusterRole{
			{
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
						Verbs:     []string{"create", "get", "delete", "list", "patch", "update", "watch"},
						APIGroups: []string{"rbac.authorization.k8s.io"},
						Resources: []string{"clusterrolebindings", "clusterroles"},
					},
					{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{""},
						Resources: []string{"nodes", "configmaps", "endpoints", "events", "limitranges", "persistentvolumeclaims", "pods", "pods/log", "replicationcontrollers", "resourcequotas", "serviceaccounts", "services"},
					},
					{
						Verbs:     []string{"get", "list"},
						APIGroups: []string{"apiextensions.k8s.io"},
						Resources: []string{"customresourcedefinitions"},
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
				},
			},
		}...)
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
		Rules: append(v2CRDRules(), rbacv1.PolicyRule{
			Verbs:     []string{"create", "get", "delete", "list", "patch", "update", "watch"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"clusterrolebindings", "clusterroles"},
		}),
	})
}

func ClusterRoleBindings(dot *helmette.Dot) []rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	binding := []rbacv1.ClusterRoleBinding{
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "proxy-role"),
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     cleanForK8sWithSuffix(Fullname(dot), "proxy-role"),
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

	if values.Scope == Cluster {
		return append(binding, rbacv1.ClusterRoleBinding{
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
		})
	}

	if values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs {
		binding = append(binding, rbacv1.ClusterRoleBinding{
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

	if values.Scope == Namespace && values.RBAC.CreateRPKBundleCRs {
		binding = append(binding, rbacv1.ClusterRoleBinding{
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

	return append(binding, rbacv1.ClusterRoleBinding{
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
	})
}

func Roles(dot *helmette.Dot) []rbacv1.Role {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	role := []rbacv1.Role{
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
					APIGroups: []string{"coordination.k8s.io"},
					Resources: []string{"leases"},
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot), "pvc"),
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"list", "delete"},
					APIGroups: []string{""},
					Resources: []string{"persistentvolumeclaims"},
				},
			},
		},
	}

	if values.Scope == Namespace {
		role = append(role, rbacv1.Role{
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
					Resources: []string{"deployments"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"apps"},
					Resources: []string{"replicasets"},
				},
				{
					Verbs:     []string{"list", "watch", "create", "delete", "get", "patch", "update"},
					APIGroups: []string{"apps"},
					Resources: []string{"statefulsets"},
				},
				{
					Verbs:     []string{"patch", "update"},
					APIGroups: []string{"apps"},
					Resources: []string{"statefulsets/status"},
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
					Verbs:     []string{"patch", "update"},
					APIGroups: []string{""},
					Resources: []string{"pods/status"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{""},
					Resources: []string{"services"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"helm.toolkit.fluxcd.io"},
					Resources: []string{"helmreleases"},
				},
				{
					Verbs:     []string{"update"},
					APIGroups: []string{"helm.toolkit.fluxcd.io"},
					Resources: []string{"helmreleases/finalizers"},
				},
				{
					Verbs:     []string{"get", "patch", "update"},
					APIGroups: []string{"helm.toolkit.fluxcd.io"},
					Resources: []string{"helmreleases/status"},
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
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"buckets"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"gitrepositories"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"gitrepository"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"gitrepository/finalizers"},
				},
				{
					Verbs:     []string{"get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"gitrepository/status"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmcharts"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmcharts/finalizers"},
				},
				{
					Verbs:     []string{"get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmcharts/status"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmrepositories"},
				},
				{
					Verbs:     []string{"create", "delete", "get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmrepositories/finalizers"},
				},
				{
					Verbs:     []string{"get", "patch", "update"},
					APIGroups: []string{"source.toolkit.fluxcd.io"},
					Resources: []string{"helmrepositories/status"},
				},
			},
		})
	}

	return role
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
				Name:        cleanForK8sWithSuffix(Fullname(dot), "pvc"),
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
				Name:     cleanForK8sWithSuffix(Fullname(dot), "pvc"),
			},
		},
	}

	if values.Scope == Namespace {
		binding = append(binding, rbacv1.RoleBinding{
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
