// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_deployment.go.tpl
package console

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Console's HTTP server Port.
// The port is defined from the provided config but can be overridden
// by setting service.targetPort and if that is missing defaults to 8080.
func ContainerPort(dot *helmette.Dot) int32 {
	values := helmette.Unwrap[Values](dot.Values)

	listenPort := int32(8080)
	if values.Service.TargetPort != nil {
		listenPort = *values.Service.TargetPort
	}

	configListenPort := helmette.Dig(values.Config, nil, "server", "listenPort")
	if asInt, ok := helmette.AsIntegral[int](configListenPort); ok {
		return int32(asInt)
	}

	return listenPort
}

func Deployment(dot *helmette.Dot) *appsv1.Deployment {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Deployment.Create {
		return nil
	}

	var replicas *int32
	if !values.Autoscaling.Enabled {
		replicas = ptr.To(values.ReplicaCount)
	}

	var initContainers []corev1.Container
	if !helmette.Empty(values.InitContainers.ExtraInitContainers) {
		initContainers = helmette.UnmarshalYamlArray[corev1.Container](helmette.Tpl(dot, *values.InitContainers.ExtraInitContainers, dot))
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "configs",
			MountPath: "/etc/console/configs",
			ReadOnly:  true,
		},
	}

	if values.Secret.Create {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "secrets",
			MountPath: "/etc/console/secrets",
			ReadOnly:  true,
		})
	}

	for _, mount := range values.SecretMounts {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.Path,
			SubPath:   ptr.Deref(mount.SubPath, ""),
		})
	}

	volumeMounts = append(volumeMounts, values.ExtraVolumeMounts...)

	return &appsv1.Deployment{
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
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(dot),
			},
			Strategy: values.Strategy,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: helmette.Merge(map[string]string{
						"checksum/config": helmette.Sha256Sum(helmette.ToYaml(ConfigMap(dot).Data)),
					}, values.PodAnnotations),
					Labels: helmette.Merge(SelectorLabels(dot), values.PodLabels),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:             values.ImagePullSecrets,
					ServiceAccountName:           ServiceAccountName(dot),
					AutomountServiceAccountToken: &values.AutomountServiceAccountToken,
					SecurityContext:              &values.PodSecurityContext,
					NodeSelector:                 values.NodeSelector,
					Affinity:                     &values.Affinity,
					TopologySpreadConstraints:    values.TopologySpreadConstraints,
					PriorityClassName:            values.PriorityClassName,
					Tolerations:                  values.Tolerations,
					Volumes:                      consolePodVolumes(dot),
					InitContainers:               initContainers,
					Containers: append([]corev1.Container{
						{
							Name:    dot.Chart.Name,
							Command: values.Deployment.Command,
							Args: append([]string{
								"--config.filepath=/etc/console/configs/config.yaml",
							}, values.Deployment.ExtraArgs...),
							SecurityContext: &values.SecurityContext,
							Image:           containerImage(dot),
							ImagePullPolicy: values.Image.PullPolicy,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: ContainerPort(dot),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: volumeMounts,
							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: values.LivenessProbe.InitialDelaySeconds, // TODO what to do with this??
								PeriodSeconds:       values.LivenessProbe.PeriodSeconds,
								TimeoutSeconds:      values.LivenessProbe.TimeoutSeconds,
								SuccessThreshold:    values.LivenessProbe.SuccessThreshold,
								FailureThreshold:    values.LivenessProbe.FailureThreshold,
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/admin/health",
										Port: intstr.FromString("http"),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: values.ReadinessProbe.InitialDelaySeconds,
								PeriodSeconds:       values.ReadinessProbe.PeriodSeconds,
								TimeoutSeconds:      values.ReadinessProbe.TimeoutSeconds,
								SuccessThreshold:    values.ReadinessProbe.SuccessThreshold,
								FailureThreshold:    values.ReadinessProbe.FailureThreshold,
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/admin/health",
										Port: intstr.FromString("http"),
									},
								},
							},
							Resources: values.Resources,
							Env:       consoleContainerEnv(dot),
							EnvFrom:   values.ExtraEnvFrom,
						},
					}, values.ExtraContainers...),
				},
			},
		},
	}
}

// ConsoleImage
func containerImage(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)

	tag := dot.Chart.AppVersion
	if !helmette.Empty(values.Image.Tag) {
		tag = *values.Image.Tag
	}

	image := fmt.Sprintf("%s:%s", values.Image.Repository, tag)

	if !helmette.Empty(values.Image.Registry) {
		return fmt.Sprintf("%s/%s", values.Image.Registry, image)
	}

	return image
}

type PossibleEnvVar struct {
	Value  any
	EnvVar corev1.EnvVar
}

func consoleContainerEnv(dot *helmette.Dot) []corev1.EnvVar {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Secret.Create {
		vars := values.ExtraEnv

		if values.LicenseSecretRef != nil && !helmette.Empty(values.LicenseSecretRef.Name) {
			vars = append(values.ExtraEnv, corev1.EnvVar{
				Name: "LICENSE",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: values.LicenseSecretRef.Name,
						},
						Key: helmette.Default("enterprise-license", values.LicenseSecretRef.Key),
					},
				},
			})
		}

		return vars
	}

	possibleVars := []PossibleEnvVar{
		{
			Value: values.Secret.Kafka.SASLPassword,
			EnvVar: corev1.EnvVar{
				Name: "KAFKA_SASL_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "kafka-sasl-password",
					},
				},
			},
		},
		{
			Value: values.Secret.Serde.ProtobufGitBasicAuthPassword,
			EnvVar: corev1.EnvVar{
				Name: "SERDE_PROTOBUF_GIT_BASICAUTH_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "serde-protobuf-git-basicauth-password",
					},
				},
			},
		},
		{
			Value: values.Secret.Kafka.AWSMSKIAMSecretKey,
			EnvVar: corev1.EnvVar{
				Name: "KAFKA_SASL_AWSMSKIAM_SECRETKEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "kafka-sasl-aws-msk-iam-secret-key",
					},
				},
			},
		},
		{
			Value: values.Secret.Kafka.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-ca",
			},
		},
		{
			Value: values.Secret.Kafka.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-cert",
			},
		},
		{
			Value: values.Secret.Kafka.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-key",
			},
		},
		{
			Value: values.Secret.SchemaRegistry.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-ca",
			},
		},
		{
			Value: values.Secret.SchemaRegistry.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-cert",
			},
		},
		{
			Value: values.Secret.SchemaRegistry.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-key",
			},
		},
		{
			Value: values.Secret.SchemaRegistry.Password,
			EnvVar: corev1.EnvVar{
				Name: "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "schema-registry-password",
					},
				},
			},
		},
		{
			Value: values.Secret.SchemaRegistry.BearerToken,
			EnvVar: corev1.EnvVar{
				Name: "SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "schema-registry-bearertoken",
					},
				},
			},
		},
		{
			Value: values.Secret.Authentication.JWTSigningKey,
			EnvVar: corev1.EnvVar{
				Name: "AUTHENTICATION_JWTSIGNINGKEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "authentication-jwt-signingkey",
					},
				},
			},
		},
		{
			Value: values.Secret.License,
			EnvVar: corev1.EnvVar{
				Name: "LICENSE",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "license",
					},
				},
			},
		},
		{
			Value: values.Secret.Redpanda.AdminAPI.Password,
			EnvVar: corev1.EnvVar{
				Name: "REDPANDA_ADMINAPI_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: Fullname(dot),
						},
						Key: "redpanda-admin-api-password",
					},
				},
			},
		},
		{
			Value: values.Secret.Redpanda.AdminAPI.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-ca",
			},
		},
		{
			Value: values.Secret.Redpanda.AdminAPI.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-key",
			},
		},
		{
			Value: values.Secret.Redpanda.AdminAPI.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-cert",
			},
		},
	}

	vars := values.ExtraEnv
	for _, possible := range possibleVars {
		if !helmette.Empty(possible.Value) {
			vars = append(vars, possible.EnvVar)
		}
	}

	return vars
}

func consolePodVolumes(dot *helmette.Dot) []corev1.Volume {
	values := helmette.Unwrap[Values](dot.Values)

	volumes := []corev1.Volume{
		{
			Name: "configs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: Fullname(dot),
					},
				},
			},
		},
	}

	if values.Secret.Create {
		volumes = append(volumes, corev1.Volume{
			Name: "secrets",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: Fullname(dot),
				},
			},
		})
	}

	for _, mount := range values.SecretMounts {
		volumes = append(volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  mount.SecretName,
					DefaultMode: mount.DefaultMode,
				},
			},
		})
	}

	return append(volumes, values.ExtraVolumes...)
}
