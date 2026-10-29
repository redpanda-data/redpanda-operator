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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	// pipelineAPIAudience must match the operator's pipelineapi.DefaultAudience.
	pipelineAPIAudience  = "redpanda-pipeline-api"
	pipelineAPIPort      = 8080
	pipelineAPIPortName  = "grpc"
	pipelineAPITokenDir  = "/var/run/secrets/redpanda.com/pipeline-api"
	pipelineAPITokenPath = "/var/run/secrets/redpanda.com/pipeline-api/token"
)

// PipelinesTypes returns the kinds that [Render] returns only when
// pipelines.enabled is true and that [Types] doesn't already list.
func PipelinesTypes() []kube.Object {
	return []kube.Object{
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&rbacv1.ClusterRoleBinding{},
	}
}

// PipelineAPIName names the pipeline API's namespaced objects.
func PipelineAPIName(state *RenderState) string {
	return cleanForK8s(fmt.Sprintf("%s-pipeline-api", state.FullName()))
}

// pipelineAPISelectorLabels select the pipeline API's Pods. They differ from
// [RenderState.SelectorLabels] so Console's Service and Deployment don't
// select them.
func pipelineAPISelectorLabels(state *RenderState) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      cleanForK8s(fmt.Sprintf("%s-pipeline-api", state.ChartName())),
		"app.kubernetes.io/instance":  state.ReleaseName,
		"app.kubernetes.io/component": "pipeline-api",
	}
}

// validatePipelines fails rendering for pipelines values that would leave the
// pipeline API unusable or reachable by Pods other than Console's.
func validatePipelines(state *RenderState) {
	if state.Values.Pipelines.Image.Tag == "" {
		panic("pipelines.image.tag is required: set it to the Redpanda Operator version that serves the pipeline API")
	}
	// Without a userRef, cluster-bound pipelines authenticate as the
	// cluster's bootstrap superuser.
	if state.Values.Pipelines.ClusterRef != "" && state.Values.Pipelines.UserRef == "" {
		panic("pipelines.clusterRef requires pipelines.userRef: set it to a User with ACLs scoped to Console's pipelines")
	}
	// The pipeline API accepts tokens of Console's ServiceAccount, which
	// every Pod that names no ServiceAccount shares if it's the default one.
	if ServiceAccountName(state) == "default" {
		panic("pipelines.enabled requires a dedicated ServiceAccount for Console: set serviceAccount.create: true or serviceAccount.name")
	}
}

// consoleConfig returns `config` with `cloud.redpandaConnect` pointed at the
// pipeline API. These keys win over the same keys in `config`.
func consoleConfig(state *RenderState) map[string]any {
	if !state.Values.Pipelines.Enabled {
		return state.Values.Config
	}

	redpandaConnect := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "cloud", "redpandaConnect").(map[string]any))
	redpandaConnect["enabled"] = true
	redpandaConnect["address"] = fmt.Sprintf("%s.%s.svc:%d", PipelineAPIName(state), state.Namespace, pipelineAPIPort)
	redpandaConnect["tokenFile"] = pipelineAPITokenPath

	cloud := helmette.Merge(map[string]any{}, helmette.Dig(state.Values.Config, map[string]any{}, "cloud").(map[string]any))
	cloud["redpandaConnect"] = redpandaConnect

	config := helmette.Merge(map[string]any{}, state.Values.Config)
	config["cloud"] = cloud

	return config
}

// pipelineAPITokenVolume projects a token of Console's ServiceAccount, bound
// to the pipeline API's audience, into the Console Pods.
func pipelineAPITokenVolume() corev1.Volume {
	return corev1.Volume{
		Name: "pipeline-api-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          pipelineAPIAudience,
							ExpirationSeconds: ptr.To(int64(3600)),
							Path:              "token",
						},
					},
				},
			},
		},
	}
}

func pipelineAPITokenVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "pipeline-api-token",
		MountPath: pipelineAPITokenDir,
		ReadOnly:  true,
	}
}

func PipelineAPIServiceAccount(state *RenderState) *corev1.ServiceAccount {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
	}
}

// PipelineAPIRole grants the pipeline API access to the Pipelines in Console's
// namespace.
func PipelineAPIRole(state *RenderState) *rbacv1.Role {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cluster.redpanda.com"},
				Resources: []string{"pipelines"},
				Verbs:     []string{"get", "list", "create", "update", "delete"},
			},
		},
	}
}

func PipelineAPIRoleBinding(state *RenderState) *rbacv1.RoleBinding {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     PipelineAPIName(state),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      PipelineAPIName(state),
				Namespace: state.Namespace,
			},
		},
	}
}

// PipelineAPIClusterRoleBinding lets the pipeline API authenticate Console
// with TokenReview.
func PipelineAPIClusterRoleBinding(state *RenderState) *rbacv1.ClusterRoleBinding {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   pipelinesClusterScopedName(state, "pipeline-api"),
			Labels: state.Labels(nil),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "system:auth-delegator",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      PipelineAPIName(state),
				Namespace: state.Namespace,
			},
		},
	}
}

// PipelineAPIDeployment runs the operator's pipeline API, which serves
// Console's pipeline calls from the Pipelines in Console's namespace.
func PipelineAPIDeployment(state *RenderState) *appsv1.Deployment {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	validatePipelines(state)

	args := []string{
		"pipeline-api",
		fmt.Sprintf("--namespace=%s", state.Namespace),
		fmt.Sprintf("--listen-address=:%d", pipelineAPIPort),
		fmt.Sprintf("--allowed-caller=system:serviceaccount:%s:%s", state.Namespace, ServiceAccountName(state)),
	}
	if state.Values.Pipelines.ClusterRef != "" {
		args = append(args, fmt.Sprintf("--cluster-ref=%s", state.Values.Pipelines.ClusterRef))
	}
	if state.Values.Pipelines.UserRef != "" {
		args = append(args, fmt.Sprintf("--user-ref=%s", state.Values.Pipelines.UserRef))
	}

	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
				Port: intstr.FromString(pipelineAPIPortName),
			},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: pipelineAPISelectorLabels(state),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: pipelineAPISelectorLabels(state),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           PipelineAPIName(state),
					AutomountServiceAccountToken: ptr.To(true),
					ImagePullSecrets:             state.Values.ImagePullSecrets,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "pipeline-api",
							Image:           fmt.Sprintf("%s:%s", state.Values.Pipelines.Image.Repository, state.Values.Pipelines.Image.Tag),
							ImagePullPolicy: state.Values.Pipelines.Image.PullPolicy,
							// The image's entrypoint runs the controller manager.
							Command: []string{"/redpanda-operator"},
							Args:    args,
							Ports: []corev1.ContainerPort{
								{
									Name:          pipelineAPIPortName,
									ContainerPort: pipelineAPIPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: probe,
							LivenessProbe:  probe,
							Resources:      state.Values.Pipelines.Resources,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func PipelineAPIService(state *RenderState) *corev1.Service {
	if !state.Values.Pipelines.Enabled {
		return nil
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PipelineAPIName(state),
			Namespace: state.Namespace,
			Labels:    state.Labels(nil),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: pipelineAPISelectorLabels(state),
			Ports: []corev1.ServicePort{
				{
					Name:        pipelineAPIPortName,
					Port:        pipelineAPIPort,
					TargetPort:  intstr.FromString(pipelineAPIPortName),
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: ptr.To("kubernetes.io/h2c"),
				},
			},
		},
	}
}

// pipelinesClusterScopedName names a cluster-scoped object after a hash of the
// release's namespace and name, so that same-named releases in different
// namespaces don't collide.
func pipelinesClusterScopedName(state *RenderState, suffix string) string {
	// NB: sha256 rather than FNV, which Helm templates can't compute.
	hash := helmette.Trunc(8, helmette.Sha256Sum(fmt.Sprintf("%s/%s", state.Namespace, state.ReleaseName)))
	tail := fmt.Sprintf("-%s-%s", hash, suffix)
	return fmt.Sprintf("%s%s", helmette.TrimSuffix("-", helmette.Trunc(63-len(tail), state.FullName())), tail)
}
