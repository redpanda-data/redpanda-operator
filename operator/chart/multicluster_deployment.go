// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_multicluster_deployment.go.tpl
package operator

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/chartutil"
)

func MulticlusterDeployment(dot *helmette.Dot) *appsv1.Deployment {
	values := helmette.Unwrap[Values](dot.Values)

	if values.Multicluster.Name == "" {
		panic("name must be specified in multicluster mode")
	}

	if values.Multicluster.KubernetesAPIExternalAddress == "" {
		panic("apiServerExternalAddress must be specified in multicluster mode")
	}

	if len(values.Multicluster.Peers) == 0 {
		panic("peers must be specified in multicluster mode")
	}

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
						Volumes:                       multiclusterOperatorPodVolumes(dot),
						Containers:                    multiclusterOperatorContainers(dot, nil),
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

func multiclusterOperatorContainers(dot *helmette.Dot, podTerminationGracePeriodSeconds *int64) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	return []corev1.Container{
		{
			Name:            "manager",
			Image:           containerImage(dot),
			ImagePullPolicy: values.Image.PullPolicy,
			Command:         []string{"/redpanda-operator"},
			Args:            multiclusterOperatorArguments(dot),
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
			VolumeMounts:   multiclusterOperatorPodVolumesMounts(dot),
			LivenessProbe:  livenessProbe(dot, podTerminationGracePeriodSeconds),
			ReadinessProbe: readinessProbe(dot, podTerminationGracePeriodSeconds),
			Resources:      values.Resources,
		},
	}
}

func multiclusterOperatorPodVolumes(dot *helmette.Dot) []corev1.Volume {
	values := helmette.Unwrap[Values](dot.Values)

	vol := []corev1.Volume{
		serviceAccountTokenVolume(),
		multiclusterTLSVolume(dot),
	}

	if !values.Webhook.Enabled {
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

func multiclusterOperatorPodVolumesMounts(dot *helmette.Dot) []corev1.VolumeMount {
	values := helmette.Unwrap[Values](dot.Values)

	volMount := []corev1.VolumeMount{
		serviceAccountTokenVolumeMount(),
		multiclusterTLSVolumeMount(dot),
	}

	if !values.Webhook.Enabled {
		return volMount
	}

	volMount = append(volMount, corev1.VolumeMount{
		Name:      "cert",
		MountPath: webhookCertificatePath,
		ReadOnly:  true,
	})

	return volMount
}

func multiclusterOperatorArguments(dot *helmette.Dot) []string {
	values := helmette.Unwrap[Values](dot.Values)

	defaults := map[string]string{
		"--health-probe-bind-address": ":8081",
		"--metrics-bind-address":      ":8443",
		"--log-level":                 values.LogLevel,
		"--name":                      values.Multicluster.Name,
		"--base-tag":                  containerTag(dot),
		"--base-image":                values.Image.Repository,
		"--raft-address":              "0.0.0.0:9443",
		"--ca-file":                   "/tls/ca.crt",
		"--certificate-file":          "/tls/tls.crt",
		"--private-key-file":          "/tls/tls.key",
		"--kubernetes-api-address":    values.Multicluster.KubernetesAPIExternalAddress,
		"--kubeconfig-namespace":      dot.Release.Namespace,
		"--kubeconfig-name":           Fullname(dot),
	}

	if values.Webhook.Enabled {
		defaults["--webhook-cert-path"] = webhookCertificatePath + "/tls.crt"
		defaults["--webhook-key-path"] = webhookCertificatePath + "/tls.key"
	}

	userProvided := chartutil.ParseFlags(values.AdditionalCmdFlags)

	flags := []string{"multicluster"}
	for key, value := range helmette.SortedMap(helmette.Merge(defaults, userProvided)) {
		if value == "" {
			flags = append(flags, key)
		} else {
			flags = append(flags, fmt.Sprintf("%s=%s", key, value))
		}
	}

	for _, peer := range values.Multicluster.Peers {
		flags = append(flags, fmt.Sprintf("--peer=%s://%s:9443", peer.Name, peer.Address))
	}

	return flags
}

func multiclusterTLSVolume(dot *helmette.Dot) corev1.Volume {
	return corev1.Volume{
		Name: fmt.Sprintf("%s-multicluster-certificates", Fullname(dot)),
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: fmt.Sprintf("%s-multicluster-certificates", Fullname(dot)),
				Items: []corev1.KeyToPath{{
					Key:  "tls.crt",
					Path: "tls.crt",
				}, {
					Key:  "tls.key",
					Path: "tls.key",
				}, {
					Key:  "ca.crt",
					Path: "ca.crt",
				}},
			},
		},
	}
}

func multiclusterTLSVolumeMount(dot *helmette.Dot) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      fmt.Sprintf("%s-multicluster-certificates", Fullname(dot)),
		ReadOnly:  true,
		MountPath: "/tls",
	}
}
