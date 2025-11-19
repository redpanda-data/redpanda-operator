// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_deployment.go.tpl
package multicluster

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Deployment(dot *helmette.Dot) *appsv1.Deployment {
	values := helmette.Unwrap[Values](dot.Values)

	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      Fullname(dot),
			Labels:    Labels(dot),
			Namespace: dot.Release.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(dot),
			},
			Template: StrategicMergePatch(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      values.PodTemplate.Metadata.Labels,
					Annotations: values.PodTemplate.Metadata.Annotations,
				},
				Spec: values.PodTemplate.Spec,
			},
				corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: SelectorLabels(dot),
					},
					Spec: corev1.PodSpec{
						ServiceAccountName: ServiceAccountName(dot),
						Volumes:            operatorPodVolumes(dot),
						Containers:         operatorContainers(dot),
					},
				}),
		},
	}
	return dep
}

func operatorPodVolumes(dot *helmette.Dot) []corev1.Volume {
	return []corev1.Volume{
		tlsVolume(dot),
	}
}

func operatorContainers(dot *helmette.Dot) []corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	return []corev1.Container{
		{
			Name:            "manager",
			Image:           containerImage(dot),
			Command:         []string{"/manager"},
			Args:            append([]string{"multicluster"}, operatorArguments(dot)...),
			SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)},
			Ports: []corev1.ContainerPort{
				{
					Name:          "https",
					ContainerPort: 9443,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			VolumeMounts: operatorPodVolumesMounts(dot),
			Resources:    values.Resources,
		},
	}
}

func operatorPodVolumesMounts(dot *helmette.Dot) []corev1.VolumeMount {
	return []corev1.VolumeMount{tlsVolumeMount(dot)}
}

func tlsVolumeMount(dot *helmette.Dot) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      fmt.Sprintf("%s-certificates", Fullname(dot)),
		ReadOnly:  true,
		MountPath: "/tls",
	}
}

func tlsVolume(dot *helmette.Dot) corev1.Volume {
	return corev1.Volume{
		Name: fmt.Sprintf("%s-certificates", Fullname(dot)),
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: fmt.Sprintf("%s-certificates", Fullname(dot)),
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

func operatorArguments(dot *helmette.Dot) []string {
	values := helmette.Unwrap[Values](dot.Values)

	defaults := map[string]string{
		"--name":                   values.Node,
		"--address":                "0.0.0.0:9443",
		"--ca-file":                "/tls/ca.crt",
		"--certificate-file":       "/tls/tls.crt",
		"--private-key-file":       "/tls/tls.key",
		"--kubernetes-api-address": values.KubernetesAPIExternalAddress,
		"--kubeconfig-namespace":   dot.Release.Namespace,
		"--kubeconfig-name":        Fullname(dot),
		"--log-level":              values.LogLevel,
	}

	var flags []string
	for key, value := range helmette.SortedMap(defaults) {
		if value == "" {
			flags = append(flags, key)
		} else {
			flags = append(flags, fmt.Sprintf("%s=%s", key, value))
		}
	}
	for _, peer := range values.Peers {
		flags = append(flags, fmt.Sprintf("--peer=%s://%s:9443", peer.Name, peer.Address))
	}

	return flags
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
