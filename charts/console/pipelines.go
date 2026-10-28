// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// PipelinesNamespace returns the namespace whose Pipeline resources Console
// manages.
func PipelinesNamespace(state *RenderState) string {
	return helmette.Default(state.Namespace, state.Values.Pipelines.Namespace)
}

// pipelinesViaServiceAccount reports whether Console reaches the Kubernetes
// API with its own ServiceAccount, which the chart then has to grant access.
func pipelinesViaServiceAccount(state *RenderState) bool {
	return state.Values.Pipelines.Enabled && !state.Values.Pipelines.ImpersonateUser
}

// pipelinesConfig returns `config` with `pipelines.kubernetes` set from
// `pipelines`. These keys win over the same keys in `config` so that Console
// always matches the RBAC the chart renders.
func pipelinesConfig(state *RenderState) map[string]any {
	if !state.Values.Pipelines.Enabled {
		return state.Values.Config
	}

	// Without a userRef, cluster-bound pipelines authenticate as the
	// cluster's bootstrap superuser.
	if state.Values.Pipelines.ClusterRef != "" && state.Values.Pipelines.UserRef == "" && !state.Values.Pipelines.AllowBootstrapUser {
		panic("pipelines.clusterRef requires pipelines.userRef: set pipelines.userRef to a User with ACLs scoped to Console's pipelines, or set pipelines.allowBootstrapUser: true for clusters without SASL")
	}

	// Pipeline Pods without a serviceAccountName run as the namespace's
	// default ServiceAccount, so they'd inherit the Role granted to it.
	if pipelinesViaServiceAccount(state) && ServiceAccountName(state) == "default" {
		panic("pipelines.enabled requires a dedicated ServiceAccount for Console: the chart grants Console's ServiceAccount access to Pipelines, and the namespace's default ServiceAccount is shared with every Pod that doesn't name one. Set serviceAccount.create: true or serviceAccount.name, or set pipelines.impersonateUser: true")
	}

	kubernetes := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "pipelines", "kubernetes").(map[string]any))
	kubernetes["enabled"] = true
	kubernetes["namespace"] = PipelinesNamespace(state)
	kubernetes["clusterRef"] = state.Values.Pipelines.ClusterRef
	kubernetes["userRef"] = state.Values.Pipelines.UserRef
	kubernetes["allowBootstrapUser"] = state.Values.Pipelines.AllowBootstrapUser
	kubernetes["impersonateUser"] = state.Values.Pipelines.ImpersonateUser
	kubernetes["allowUnauthenticated"] = state.Values.Pipelines.AllowUnauthenticated
	// Console refuses to start through its ServiceAccount if a dry-run
	// create of a Pipeline with spec.image succeeds, i.e. without the policy.
	kubernetes["skipAdmissionCheck"] = pipelinesViaServiceAccount(state) && !state.Values.Pipelines.AdmissionPolicy.Enabled

	pipelines := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "pipelines").(map[string]any))
	pipelines["kubernetes"] = kubernetes

	config := helmette.Merge(map[string]any{}, state.Values.Config)
	config["pipelines"] = pipelines

	return config
}

// PipelinesTypes returns the kinds that [Render] returns only when
// pipelines.enabled is true.
func PipelinesTypes() []kube.Object {
	return []kube.Object{
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&rbacv1.ClusterRole{},
		&admissionregistrationv1.ValidatingAdmissionPolicy{},
		&admissionregistrationv1.ValidatingAdmissionPolicyBinding{},
	}
}

// PipelinesRole grants Console's ServiceAccount access to the Pipelines in
// the pipelines namespace, unless Console impersonates its users.
func PipelinesRole(state *RenderState) *rbacv1.Role {
	if !pipelinesViaServiceAccount(state) {
		return nil
	}

	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pipelines", state.FullName()),
			Namespace: PipelinesNamespace(state),
			Labels:    state.Labels(nil),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cluster.redpanda.com"},
				Resources: []string{"pipelines"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
}

// PipelinesRoleBinding binds [PipelinesRole] to Console's ServiceAccount.
func PipelinesRoleBinding(state *RenderState) *rbacv1.RoleBinding {
	if !pipelinesViaServiceAccount(state) {
		return nil
	}

	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pipelines", state.FullName()),
			Namespace: PipelinesNamespace(state),
			Labels:    state.Labels(nil),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     fmt.Sprintf("%s-pipelines", state.FullName()),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName(state),
				Namespace: state.Namespace,
			},
		},
	}
}

// PipelineEditorClusterRole is for admins to bind to the groups whose members
// may manage pipelines when Console forwards their identity. No admission
// policy applies to them, so it amounts to creating Pods and reading Secrets
// in the namespace it's bound in.
func PipelineEditorClusterRole(state *RenderState) *rbacv1.ClusterRole {
	return pipelinesClusterRole(state, "pipeline-editor", []string{"get", "list", "watch", "create", "update", "patch", "delete"})
}

// PipelineViewerClusterRole is the read-only counterpart of
// [PipelineEditorClusterRole].
func PipelineViewerClusterRole(state *RenderState) *rbacv1.ClusterRole {
	return pipelinesClusterRole(state, "pipeline-viewer", []string{"get", "list", "watch"})
}

func pipelinesClusterRole(state *RenderState, suffix string, verbs []string) *rbacv1.ClusterRole {
	if !state.Values.Pipelines.Enabled || !state.Values.Pipelines.ImpersonateUser {
		return nil
	}

	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pipelinesClusterScopedName(state, suffix),
			Labels: state.Labels(nil),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cluster.redpanda.com"},
				Resources: []string{"pipelines"},
				Verbs:     verbs,
			},
		},
	}
}

// PipelinesAdmissionPolicy limits the Pipelines that Console's ServiceAccount
// may write to the fields Console sets, so a compromised Console can't run
// arbitrary Pods. Any spec field outside the allow-list, including ones added
// to the Pipeline CRD later, is rejected on create and update. Console can't
// edit a Pipeline an admin has extended, e.g. with valueSources.
func PipelinesAdmissionPolicy(state *RenderState) *admissionregistrationv1.ValidatingAdmissionPolicy {
	if !pipelinesViaServiceAccount(state) || !state.Values.Pipelines.AdmissionPolicy.Enabled {
		return nil
	}

	username := fmt.Sprintf("system:serviceaccount:%s:%s", state.Namespace, ServiceAccountName(state))

	return &admissionregistrationv1.ValidatingAdmissionPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingAdmissionPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pipelinesClusterScopedName(state, "pipelines"),
			Labels: state.Labels(nil),
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionregistrationv1.Fail),
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
								admissionregistrationv1.Update,
								admissionregistrationv1.Delete,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"cluster.redpanda.com"},
								APIVersions: []string{"*"},
								Resources:   []string{"pipelines"},
							},
						},
					},
				},
			},
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "console-service-account",
					Expression: fmt.Sprintf("request.userInfo.username == %q", username),
				},
			},
			Variables: []admissionregistrationv1.Variable{
				{
					// NB: dyn() keeps the policy type checker, which knows the
					// Pipeline schema, from warning that a typed object can't
					// be a comprehension range.
					Name:       "spec",
					Expression: "request.operation == 'DELETE' || !has(object.spec) ? {} : dyn(object.spec)",
				},
				{
					Name:       "finalizers",
					Expression: "request.operation == 'DELETE' || !has(object.metadata.finalizers) ? [] : dyn(object.metadata.finalizers)",
				},
				{
					Name:       "oldFinalizers",
					Expression: "request.operation != 'UPDATE' || !has(oldObject.metadata.finalizers) ? [] : dyn(oldObject.metadata.finalizers)",
				},
				{
					// Includes replicas and updateStrategy, which the CRD defaults
					// onto every write. TestPipelinesAdmissionPolicy checks that
					// every defaulted PipelineSpec field is listed here.
					Name:       "allowedSpecFields",
					Expression: "['configYaml', 'displayName', 'description', 'tags', 'resources', 'paused', 'replicas', 'updateStrategy', 'cluster', 'userRef']",
				},
				{
					Name:       "specFields",
					Expression: "variables.spec.filter(f, true)",
				},
				{
					Name:       "forbiddenSpecFields",
					Expression: "variables.specFields.filter(f, !(f in variables.allowedSpecFields))",
				},
				{
					Name:       "clusterRef",
					Expression: fmt.Sprintf("%q", state.Values.Pipelines.ClusterRef),
				},
				{
					Name:       "userRef",
					Expression: fmt.Sprintf("%q", state.Values.Pipelines.UserRef),
				},
				{
					Name:       "allowBootstrapUser",
					Expression: fmt.Sprintf("%t", state.Values.Pipelines.AllowBootstrapUser),
				},
			},
			Validations: []admissionregistrationv1.Validation{
				{
					Expression: "request.operation == 'DELETE' || (has(object.metadata.labels) && 'console.redpanda.com/managed-by' in object.metadata.labels && object.metadata.labels['console.redpanda.com/managed-by'] == 'console')",
					Message:    "Redpanda Console may only write Pipelines labeled console.redpanda.com/managed-by=console",
				},
				{
					Expression: "request.operation == 'CREATE' || (has(oldObject.metadata.labels) && 'console.redpanda.com/managed-by' in oldObject.metadata.labels && oldObject.metadata.labels['console.redpanda.com/managed-by'] == 'console')",
					Message:    "Redpanda Console may only update or delete Pipelines labeled console.redpanda.com/managed-by=console",
				},
				{
					Expression: "request.operation == 'DELETE' || !has(object.metadata.ownerReferences) || size(object.metadata.ownerReferences) == 0",
					Message:    "Redpanda Console may not set metadata.ownerReferences on Pipelines",
				},
				{
					// The pipeline controller manages its own finalizer as the
					// operator, which this policy doesn't match.
					Expression: "request.operation == 'DELETE' || variables.finalizers == variables.oldFinalizers",
					Message:    "Redpanda Console may not add or remove metadata.finalizers on Pipelines",
				},
				{
					Expression:        "size(variables.forbiddenSpecFields) == 0",
					MessageExpression: "'Redpanda Console may only set spec fields ' + variables.allowedSpecFields.join(', ') + '; found ' + variables.forbiddenSpecFields.join(', ')",
				},
				{
					Expression: "!('resources' in variables.specFields) || variables.spec.resources.all(k, (k == 'requests' || k == 'limits') && dyn(variables.spec.resources[k]).all(r, r == 'cpu' || r == 'memory'))",
					Message:    "Redpanda Console may only set cpu and memory in spec.resources.requests and spec.resources.limits",
				},
				{
					Expression:        "!('cluster' in variables.specFields) || (variables.clusterRef != '' && variables.spec.cluster.all(f, f == 'clusterRef') && has(variables.spec.cluster.clusterRef) && dyn(variables.spec.cluster.clusterRef).all(f, f == 'name') && variables.spec.cluster.clusterRef.name == variables.clusterRef)",
					MessageExpression: "variables.clusterRef == '' ? 'Redpanda Console may not set spec.cluster' : 'Redpanda Console may only set spec.cluster.clusterRef.name, to ' + variables.clusterRef",
				},
				{
					Expression:        "!('userRef' in variables.specFields) || (variables.userRef != '' && dyn(variables.spec.userRef).all(f, f == 'name') && has(variables.spec.userRef.name) && variables.spec.userRef.name == variables.userRef)",
					MessageExpression: "variables.userRef == '' ? 'Redpanda Console may not set spec.userRef' : 'Redpanda Console may only set spec.userRef.name, to ' + variables.userRef",
				},
				{
					// Without a userRef, the pipeline authenticates as the
					// cluster's bootstrap superuser.
					Expression:        "variables.allowBootstrapUser || !('cluster' in variables.specFields) || 'userRef' in variables.specFields",
					MessageExpression: "'Redpanda Console must set spec.userRef.name to ' + variables.userRef + ' on Pipelines that set spec.cluster'",
				},
			},
		},
	}
}

// PipelinesAdmissionPolicyBinding enforces [PipelinesAdmissionPolicy] on every
// namespace.
func PipelinesAdmissionPolicyBinding(state *RenderState) *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
	if !pipelinesViaServiceAccount(state) || !state.Values.Pipelines.AdmissionPolicy.Enabled {
		return nil
	}

	return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingAdmissionPolicyBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pipelinesClusterScopedName(state, "pipelines"),
			Labels: state.Labels(nil),
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        pipelinesClusterScopedName(state, "pipelines"),
			ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny},
		},
	}
}

// pipelinesClusterScopedName names a cluster-scoped object after a hash of the
// release's namespace and name, so no other release can render the same name.
// FullName is shortened to keep the name within 63 characters.
func pipelinesClusterScopedName(state *RenderState, suffix string) string {
	// NB: sha256 rather than FNV, which Helm templates can't compute.
	hash := helmette.Trunc(8, helmette.Sha256Sum(fmt.Sprintf("%s/%s", state.Namespace, state.ReleaseName)))
	tail := fmt.Sprintf("-%s-%s", hash, suffix)
	return helmette.TrimSuffix("-", helmette.Trunc(63-len(tail), state.FullName())) + tail
}
