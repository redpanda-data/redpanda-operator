// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_deployment.go.tpl
package operator

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	// Injected bound service account token expiration which triggers monitoring of its time-bound feature.
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/ae53151cb4e6fbba8bb78a2ef0b48a7c32a0a067/pkg/serviceaccount/claims.go#L38-L39
	tokenExpirationSeconds = 60*60 + 7

	// ServiceAccountVolumeName is the prefix name that will be added to volumes that mount ServiceAccount secrets
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L52-L53
	ServiceAccountVolumeName = "kube-api-access"

	// DefaultAPITokenMountPath is the path that ServiceAccountToken secrets are automounted to.
	// The token file would then be accessible at /var/run/secrets/kubernetes.io/serviceaccount
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L55-L57
	//nolint: gosec
	DefaultAPITokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	webhookCertificatePath = "/tmp/k8s-webhook-server/serving-certs"
)

func Deployment(dot *helmette.Dot) *appsv1.Deployment {
	values := helmette.Unwrap[Values](dot.Values)

	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        Fullname(dot),
			Labels:      Labels(dot),
			Namespace:   dot.Release.Namespace,
			Annotations: values.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(values.ReplicaCount),
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(dot),
			},
			Strategy: values.Strategy,
			Template: StrategicMergePatch(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      values.PodTemplate.Metadata.Labels,
					Annotations: values.PodTemplate.Metadata.Annotations,
				},
				Spec: values.PodTemplate.Spec,
			},
				corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: values.PodAnnotations,
						Labels:      helmette.Merge(SelectorLabels(dot), values.PodLabels),
					},
					Spec: corev1.PodSpec{
						AutomountServiceAccountToken:  ptr.To(false),
						TerminationGracePeriodSeconds: ptr.To(int64(10)),
						ImagePullSecrets:              values.ImagePullSecrets,
						ServiceAccountName:            ServiceAccountName(dot),
						NodeSelector:                  values.NodeSelector,
						Tolerations:                   values.Tolerations,
						Volumes:                       operatorPodVolumes(dot),
						Containers:                    operatorContainers(dot, nil),
					},
				}),
		},
	}

	// Values.Affinity should be deprecated.
	if !helmette.Empty(values.Affinity) {
		dep.Spec.Template.Spec.Affinity = values.Affinity
	}

	return dep
}

func operatorContainers(dot *helmette.Dot, podTerminationGracePeriodSeconds *int64) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	return []corev1.Container{
		{
			Name:            "manager",
			Image:           containerImage(dot),
			ImagePullPolicy: values.Image.PullPolicy,
			Command:         []string{"/manager"},
			Args:            operatorArguments(dot),
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)},
			Ports: []corev1.ContainerPort{
				{
					Name:          "webhook-server",
					ContainerPort: 9443,
					Protocol:      corev1.ProtocolTCP,
				},
				{
					Name:          "https",
					ContainerPort: 8443,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			VolumeMounts:   operatorPodVolumesMounts(dot),
			LivenessProbe:  livenessProbe(dot, podTerminationGracePeriodSeconds),
			ReadinessProbe: readinessProbe(dot, podTerminationGracePeriodSeconds),
			Resources:      values.Resources,
		},
	}
}

func livenessProbe(dot *helmette.Dot, podTerminationGracePeriodSeconds *int64) *corev1.Probe {
	values := helmette.Unwrap[Values](dot.Values)

	if values.LivenessProbe != nil {
		return &corev1.Probe{
			InitialDelaySeconds:           helmette.Default(15, values.LivenessProbe.InitialDelaySeconds), // TODO what to do with this??
			PeriodSeconds:                 helmette.Default(20, values.LivenessProbe.PeriodSeconds),
			TimeoutSeconds:                values.LivenessProbe.TimeoutSeconds,
			SuccessThreshold:              values.LivenessProbe.SuccessThreshold,
			FailureThreshold:              values.LivenessProbe.FailureThreshold,
			TerminationGracePeriodSeconds: helmette.Default(podTerminationGracePeriodSeconds, values.LivenessProbe.TerminationGracePeriodSeconds),
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz/",
					Port: intstr.FromInt32(8081),
				},
			},
		}
	}
	return &corev1.Probe{
		InitialDelaySeconds: 15,
		PeriodSeconds:       20,
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz/",
				Port: intstr.FromInt32(8081),
			},
		},
	}
}

func readinessProbe(dot *helmette.Dot, podTerminationGracePeriodSeconds *int64) *corev1.Probe {
	values := helmette.Unwrap[Values](dot.Values)

	if values.LivenessProbe != nil {
		return &corev1.Probe{
			InitialDelaySeconds:           helmette.Default(5, values.ReadinessProbe.InitialDelaySeconds),
			PeriodSeconds:                 helmette.Default(10, values.ReadinessProbe.PeriodSeconds),
			TimeoutSeconds:                values.ReadinessProbe.TimeoutSeconds,
			SuccessThreshold:              values.ReadinessProbe.SuccessThreshold,
			FailureThreshold:              values.ReadinessProbe.FailureThreshold,
			TerminationGracePeriodSeconds: helmette.Default(podTerminationGracePeriodSeconds, values.ReadinessProbe.TerminationGracePeriodSeconds),
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromInt32(8081),
				},
			},
		}
	}

	return &corev1.Probe{
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/readyz",
				Port: intstr.FromInt32(8081),
			},
		},
	}
}

func containerTag(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)
	if !helmette.Empty(values.Image.Tag) {
		return *values.Image.Tag
	}
	return dot.Chart.AppVersion
}

func containerImage(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	tag := containerTag(dot)

	return fmt.Sprintf("%s:%s", values.Image.Repository, tag)
}

func isWebhookEnabled(dot *helmette.Dot) bool {
	values := helmette.Unwrap[Values](dot.Values)

	return values.Webhook.Enabled && values.Scope == Cluster
}

func operatorPodVolumes(dot *helmette.Dot) []corev1.Volume {
	values := helmette.Unwrap[Values](dot.Values)

	vol := []corev1.Volume{
		kubeTokenAPIVolume(ServiceAccountVolumeName),
	}

	if !isWebhookEnabled(dot) {
		return vol
	}

	vol = append(vol, corev1.Volume{
		Name: "cert",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				DefaultMode: ptr.To(int32(420)),
				SecretName:  values.WebhookSecretName,
			},
		},
	})

	return vol
}

// kubeTokenAPIVolume is a slightly changed variant of
// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L484-L524
// Upstream creates Projected Volume Source, but this function returns Volume with provided name.
// Also const are renamed.
func kubeTokenAPIVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				// explicitly set default value, see https://github.com/kubernetes/kubernetes/issues/104464
				DefaultMode: ptr.To(corev1.ProjectedVolumeSourceDefaultMode),
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: ptr.To(int64(tokenExpirationSeconds)),
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kube-root-ca.crt",
							},
							Items: []corev1.KeyToPath{
								{
									Key:  "ca.crt",
									Path: "ca.crt",
								},
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "namespace",
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func operatorPodVolumesMounts(dot *helmette.Dot) []corev1.VolumeMount {
	volMount := []corev1.VolumeMount{}

	mountName := ServiceAccountVolumeName
	for _, vol := range operatorPodVolumes(dot) {
		if strings.HasPrefix(ServiceAccountVolumeName+"-", vol.Name) {
			mountName = vol.Name
		}
	}

	volMount = append(volMount, corev1.VolumeMount{
		Name:      mountName,
		ReadOnly:  true,
		MountPath: DefaultAPITokenMountPath,
	})

	if !isWebhookEnabled(dot) {
		return volMount
	}

	volMount = append(volMount, corev1.VolumeMount{
		Name:      "cert",
		MountPath: webhookCertificatePath,
		ReadOnly:  true,
	})

	return volMount
}

func operatorArguments(dot *helmette.Dot) []string {
	values := helmette.Unwrap[Values](dot.Values)

	args := []string{
		"--health-probe-bind-address=:8081",
		"--metrics-bind-address=:8443",
		"--leader-elect",
		fmt.Sprintf("--webhook-enabled=%t", isWebhookEnabled(dot)),
	}

	if isWebhookEnabled(dot) {
		args = append(args,
			"--webhook-enabled=true",
			fmt.Sprintf("--webhook-cert-path=%s", webhookCertificatePath),
		)
	}

	if values.Scope == Namespace {
		args = append(args,
			fmt.Sprintf("--namespace=%s", dot.Release.Namespace),
			fmt.Sprintf("--log-level=%s", values.LogLevel),
		)
	}

	hasConfiguratorTag := false
	hasConfiguratorImage := false
	for _, flag := range values.AdditionalCmdFlags {
		if helmette.Contains("--configurator-tag", flag) {
			hasConfiguratorTag = true
		}
		if helmette.Contains("--configurator-base-image", flag) {
			hasConfiguratorImage = true
		}
	}

	// If --configurator-base-image and --configurator-tag haven't been
	// specified, set them to the image specified in this chart. This ensures
	// that the operator deploys the correct version of itself when it's
	// deploying itself for other purposes, like the sidecar, initcontainer, or
	// configurator.
	if !hasConfiguratorTag {
		args = append(args, fmt.Sprintf("--configurator-tag=%s", containerTag(dot)))
	}

	if !hasConfiguratorImage {
		args = append(args, fmt.Sprintf("--configurator-base-image=%s", values.Image.Repository))
	}

	return append(args, values.AdditionalCmdFlags...)
}
