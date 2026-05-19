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

// roles returns Roles for every local NodePool that has RBAC enabled.
// RBAC config (enabled, RPKDebugBundle) is per-pool, so each pool gets its
// own Role objects and bindings.
func roles(state *RenderState) []*rbacv1.Role {
	var out []*rbacv1.Role
	for _, pool := range state.inClusterPools {
		out = append(out, rolesForPool(state, pool)...)
	}
	return out
}

func rolesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.Role {
	if !state.PoolSpec(pool).RBAC.IsEnabled() {
		return nil
	}

	poolFullname := state.poolFullname(pool)
	var out []*rbacv1.Role

	// Sidecar role: allows the sidecar to manage leases, get/list/watch pods and statefulsets.
	out = append(out, &rbacv1.Role{
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
	if ptr.Deref(state.PoolSpec(pool).RBAC.RPKDebugBundle, false) {
		out = append(out, &rbacv1.Role{
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

	return out
}

// clusterRoles returns ClusterRoles for every local NodePool that has RBAC enabled.
func clusterRoles(state *RenderState) []*rbacv1.ClusterRole {
	var out []*rbacv1.ClusterRole
	for _, pool := range state.inClusterPools {
		out = append(out, clusterRolesForPool(state, pool)...)
	}
	return out
}

func clusterRolesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*rbacv1.ClusterRole {
	if !state.PoolSpec(pool).RBAC.IsEnabled() {
		return nil
	}

	poolFullname := state.poolFullname(pool)
	var out []*rbacv1.ClusterRole

	// Self-metrics ClusterRole: grants nonResourceURLs:["/metrics"] get.
	// controller-runtime's metrics server enforces auth+authz on /metrics
	// by default, so anything authenticating as the operator SA — the
	// bundled ServiceMonitor scraping with the pod's projected token, and
	// `rpk k8s multicluster bundle` scraping with a TokenRequest-minted
	// SA token — needs this rule. clusterRoleBindings binds the matching
	// pool's SA to this role.
	out = append(out, &rbacv1.ClusterRole{
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

	if state.Spec().RackAwareness.IsEnabled() {
		out = append(out, &rbacv1.ClusterRole{
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

	return out
}

// roleBindings returns RoleBindings binding each pool's Roles to that pool's
// ServiceAccount.
func roleBindings(state *RenderState) []*rbacv1.RoleBinding {
	var out []*rbacv1.RoleBinding
	for _, pool := range state.inClusterPools {
		saName := state.PoolSpec(pool).GetServiceAccountName(state.poolFullname(pool))
		for _, role := range rolesForPool(state, pool) {
			out = append(out, &rbacv1.RoleBinding{
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
						Name:      saName,
						Namespace: state.namespace,
					},
				},
			})
		}
	}
	return out
}

// clusterRoleBindings binds each pool's ClusterRoles to that pool's
// ServiceAccount.
func clusterRoleBindings(state *RenderState) []*rbacv1.ClusterRoleBinding {
	var out []*rbacv1.ClusterRoleBinding
	for _, pool := range state.inClusterPools {
		saName := state.PoolSpec(pool).GetServiceAccountName(state.poolFullname(pool))
		for _, cr := range clusterRolesForPool(state, pool) {
			out = append(out, &rbacv1.ClusterRoleBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRoleBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:   cr.ObjectMeta.Name,
					Labels: state.commonLabels(),
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     cr.ObjectMeta.Name,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      saName,
						Namespace: state.namespace,
					},
				},
			})
		}
	}
	return out
}
