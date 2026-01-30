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

type RBACBundle struct {
	Name string
	// NB: Subject is currently only used by ClusterRoles as we'll be moving to ClusterScope soon.
	Subject     string
	Enabled     bool
	RuleFiles   []string
	Annotations map[string]string
}

func clusterRoleBundles(dot *helmette.Dot) []RBACBundle {
	values := helmette.Unwrap[Values](dot.Values)

<<<<<<< HEAD
	if !values.RBAC.Create {
		return nil
	}

	return []RBACBundle{
=======
	bundles := []RBACBundle{
>>>>>>> f1112cbe (Add migration job to handle mismatched field managers (#1249))
		{
			Name:    Fullname(dot),
			Subject: ServiceAccountName(dot),
			Enabled: values.Scope == Cluster,
			RuleFiles: []string{
				"files/rbac/leader-election.ClusterRole.yaml",
				"files/rbac/pvcunbinder.ClusterRole.yaml",
				"files/rbac/rack-awareness.ClusterRole.yaml", // Rack awareness is a toggle on the CR, so we always need RBAC for it.
				"files/rbac/v1-manager.ClusterRole.yaml",
			},
		},
		{
			Name:    Fullname(dot),
			Subject: ServiceAccountName(dot),
			Enabled: values.Scope == Namespace,
			RuleFiles: []string{
				"files/rbac/leader-election.ClusterRole.yaml",
				"files/rbac/v2-manager.ClusterRole.yaml",
			},
		},
		{
			Name:    cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
			Subject: ServiceAccountName(dot),
			Enabled: values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs,
			RuleFiles: []string{
				"files/rbac/decommission.ClusterRole.yaml",
				"files/rbac/node-watcher.ClusterRole.yaml",     // Deprecated but not yet removed.
				"files/rbac/old-decommission.ClusterRole.yaml", // Deprecated but not yet removed.
				"files/rbac/pvcunbinder.ClusterRole.yaml",
			},
		},
		// ClusterRole for the CRD installation Job.
		{
			Name:    CRDJobServiceAccountName(dot),
			Enabled: values.CRDs.Enabled || values.CRDs.Experimental,
			Subject: CRDJobServiceAccountName(dot),
			Annotations: map[string]string{
				"helm.sh/hook":               "pre-install,pre-upgrade",
				"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
				"helm.sh/hook-weight":        "-10",
			},
			RuleFiles: []string{
				"files/rbac/crd-installation.ClusterRole.yaml",
			},
		},
	}

	// the migration job needs the same general RBAC policy as the operator itself
	bundles = append(bundles, RBACBundle{
		Name:    MigrationJobServiceAccountName(dot),
		Enabled: true,
		Subject: MigrationJobServiceAccountName(dot),
		Annotations: map[string]string{
			"helm.sh/hook":               "post-upgrade",
			"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
			"helm.sh/hook-weight":        "-10",
		},
		RuleFiles: bundles[0].RuleFiles,
	})

	return bundles
}

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

	for _, bundle := range clusterRoleBundles(dot) {
		if !bundle.Enabled {
			continue
		}

		var rules []rbacv1.PolicyRule
		for _, file := range bundle.RuleFiles {
			clusterRole := helmette.FromYaml[rbacv1.ClusterRole](dot.Files.Get(file))
			rules = append(rules, clusterRole.Rules...)
		}

		clusterRoles = append(clusterRoles, rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   bundle.Name + "-" + dot.Release.Namespace,
				Labels: Labels(dot),
				Annotations: helmette.Merge(
					helmette.Default(map[string]string{}, values.Annotations),
					helmette.Default(map[string]string{}, bundle.Annotations),
				),
			},
			Rules: rules,
		})
	}

	return clusterRoles
}

func Roles(dot *helmette.Dot) []rbacv1.Role {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	bundles := []RBACBundle{
		{
			Name:    cleanForK8sWithSuffix(Fullname(dot), "election-role"),
			Enabled: true,
			RuleFiles: []string{
				"files/rbac/leader-election.Role.yaml",
			},
		},
		{
			Name:    Fullname(dot),
			Enabled: values.Scope == Cluster,
			RuleFiles: []string{
				"files/rbac/pvcunbinder.Role.yaml",
			},
		},
		{
			Name:    Fullname(dot),
			Enabled: values.Scope == Namespace,
			RuleFiles: []string{
				"files/rbac/sidecar.Role.yaml", // Sidecar is a toggle on the CR, so we always need RBAC for it.
				"files/rbac/v2-manager.Role.yaml",
			},
		},
		{
			Name:    Fullname(dot) + "-additional-controllers",
			Enabled: values.Scope == Namespace && values.RBAC.CreateAdditionalControllerCRs,
			RuleFiles: []string{
				"files/rbac/decommission.Role.yaml",
				"files/rbac/node-watcher.Role.yaml",     // Deprecated but not yet removed.
				"files/rbac/old-decommission.Role.yaml", // Deprecated but not yet removed.
				"files/rbac/pvcunbinder.Role.yaml",
			},
		},
		{
			Name:    cleanForK8sWithSuffix(Fullname(dot), "rpk-bundle"),
			Enabled: values.RBAC.CreateRPKBundleCRs,
			RuleFiles: []string{
				"files/rbac/rpk-debug-bundle.Role.yaml",
			},
		},
	}

	var roles []rbacv1.Role
	for _, bundle := range bundles {
		if !bundle.Enabled {
			continue
		}

		var rules []rbacv1.PolicyRule
		for _, file := range bundle.RuleFiles {
			clusterRole := helmette.FromYaml[rbacv1.Role](dot.Files.Get(file))
			rules = append(rules, clusterRole.Rules...)
		}

		roles = append(roles, rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        bundle.Name,
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: rules,
		})
	}

	return roles
}

func ClusterRoleBindings(dot *helmette.Dot) []rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	// NB: We skip over making a binding for the metrics viewer role.
	var bindings []rbacv1.ClusterRoleBinding
	for _, bundle := range clusterRoleBundles(dot) {
		if !bundle.Enabled {
			continue
		}

		bindings = append(bindings, rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   bundle.Name + "-" + dot.Release.Namespace,
				Labels: Labels(dot),
				Annotations: helmette.Merge(
					helmette.Default(map[string]string{}, values.Annotations),
					helmette.Default(map[string]string{}, bundle.Annotations),
				),
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     bundle.Name + "-" + dot.Release.Namespace,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      bundle.Subject,
					Namespace: dot.Release.Namespace,
				},
			},
		})
	}

	return bindings
}

func RoleBindings(dot *helmette.Dot) []rbacv1.RoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	var bindings []rbacv1.RoleBinding
	for _, role := range Roles(dot) {
		bindings = append(bindings, rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        role.ObjectMeta.Name,
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
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

	return bindings
}
