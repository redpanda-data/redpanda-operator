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
	Enabled   bool
	Name      string
	RuleFiles map[string]bool
}

func ClusterRoles(dot *helmette.Dot) []rbacv1.ClusterRole {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	bundles := []RBACBundle{
		{
			Name:    Fullname(dot),
			Enabled: true,
			RuleFiles: map[string]bool{
				"files/rbac/crd-installation.ClusterRole.yaml": values.CRDs.Enabled || values.CRDs.Experimental,
				"files/rbac/leader-election.ClusterRole.yaml":  true,
				"files/rbac/leader-election.Role.yaml":         true,
				"files/rbac/pvcunbinder.ClusterRole.yaml":      true,
				"files/rbac/pvcunbinder.Role.yaml":             true,
				"files/rbac/rack-awareness.ClusterRole.yaml":   true, // Rack awareness is a toggle on the CR, so we always need RBAC for it.
				"files/rbac/rpk-debug-bundle.Role.yaml":        true, // debug bundle permissions is a toggle on the CR, so we always need RBAC for it.
				"files/rbac/sidecar.Role.yaml":                 true, // Sidecar is a toggle on the CR, so we always need RBAC for it.
				"files/rbac/v1-manager.ClusterRole.yaml":       values.VectorizedControllers.Enabled,
				"files/rbac/v1-manager.Role.yaml":              values.VectorizedControllers.Enabled,
				"files/rbac/v2-manager.ClusterRole.yaml":       true,
			},
		},
		{
			Name:    cleanForK8sWithSuffix(Fullname(dot), "additional-controllers"),
			Enabled: values.RBAC.CreateAdditionalControllerCRs,
			RuleFiles: map[string]bool{
				"files/rbac/decommission.ClusterRole.yaml":     true,
				"files/rbac/decommission.Role.yaml":            true,
				"files/rbac/node-watcher.ClusterRole.yaml":     true, // Deprecated but not yet removed.
				"files/rbac/node-watcher.Role.yaml":            true, // Deprecated but not yet removed.
				"files/rbac/old-decommission.ClusterRole.yaml": true, // Deprecated but not yet removed.
				"files/rbac/old-decommission.Role.yaml":        true, // Deprecated but not yet removed.
				"files/rbac/pvcunbinder.ClusterRole.yaml":      true,
				"files/rbac/pvcunbinder.Role.yaml":             true,
			},
		},
	}

	clusterRoles := []rbacv1.ClusterRole{
		// Unused convenience ClusterRole that consumers may bind to for access to /metrics.
		{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        cleanForK8sWithSuffix(Fullname(dot)+"-"+dot.Release.Namespace, "metrics-reader"),
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

	for _, bundle := range bundles {
		if !bundle.Enabled {
			continue
		}

		var rules []rbacv1.PolicyRule
		for file, enabled := range helmette.SortedMap(bundle.RuleFiles) {
			if !enabled {
				continue
			}

			clusterRole := helmette.FromYaml[rbacv1.ClusterRole](dot.Files.Get(file))
			rules = append(rules, clusterRole.Rules...)
		}

		clusterRoles = append(clusterRoles, rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        bundle.Name + "-" + dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			Rules: rules,
		})
	}

	return clusterRoles
}

func ClusterRoleBindings(dot *helmette.Dot) []rbacv1.ClusterRoleBinding {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.RBAC.Create {
		return nil
	}

	// NB: We skip over making a binding for the metrics viewer role.
	var bindings []rbacv1.ClusterRoleBinding
	for _, role := range ClusterRoles(dot)[1:] {
		bindings = append(bindings, rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        role.ObjectMeta.Name,
				Labels:      Labels(dot),
				Annotations: values.Annotations,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
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
