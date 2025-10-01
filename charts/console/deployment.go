// Copyright 2025 Redpanda Data, Inc.
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
func ContainerPort(state *RenderState) int32 {
	listenPort := int32(8080)
	if state.Values.Service.TargetPort != nil {
		listenPort = *state.Values.Service.TargetPort
	}

	configListenPort := helmette.Dig(state.Values.Config, nil, "server", "listenPort")
	if asInt, ok := helmette.AsIntegral[int](configListenPort); ok {
		return int32(asInt)
	}

	return listenPort
}

func Deployment(state *RenderState) *appsv1.Deployment {
	if !state.Values.Deployment.Create {
		return nil
	}

	var replicas *int32
	if !state.Values.Autoscaling.Enabled {
		replicas = ptr.To(state.Values.ReplicaCount)
	}

	var initContainers []corev1.Container
	if !helmette.Empty(state.Values.InitContainers.ExtraInitContainers) {
		initContainers = helmette.UnmarshalYamlArray[corev1.Container](state.Template(*state.Values.InitContainers.ExtraInitContainers))
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "configs",
			MountPath: "/etc/console/configs",
			ReadOnly:  true,
		},
	}

	if state.Values.Secret.Create {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "secrets",
			MountPath: "/etc/console/secrets",
			ReadOnly:  true,
		})
	}

	for _, mount := range state.Values.SecretMounts {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.Path,
			SubPath:   ptr.Deref(mount.SubPath, ""),
		})
	}

	volumeMounts = append(volumeMounts, state.Values.ExtraVolumeMounts...)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.FullName(),
			Labels:      state.Labels(nil),
			Namespace:   state.Namespace,
			Annotations: state.Values.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: state.SelectorLabels(),
			},
			Strategy: state.Values.Strategy,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: helmette.Merge(map[string]string{
						"checksum/config": helmette.Sha256Sum(helmette.ToYaml(ConfigMap(state).Data)),
					}, state.Values.PodAnnotations),
					Labels: helmette.Merge(state.SelectorLabels(), state.Values.PodLabels),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:             state.Values.ImagePullSecrets,
					ServiceAccountName:           ServiceAccountName(state),
					AutomountServiceAccountToken: &state.Values.AutomountServiceAccountToken,
					SecurityContext:              &state.Values.PodSecurityContext,
					NodeSelector:                 state.Values.NodeSelector,
					Affinity:                     &state.Values.Affinity,
					TopologySpreadConstraints:    state.Values.TopologySpreadConstraints,
					PriorityClassName:            state.Values.PriorityClassName,
					Tolerations:                  state.Values.Tolerations,
					Volumes:                      consolePodVolumes(state),
					InitContainers:               initContainers,
					Containers: append([]corev1.Container{
						{
							Name:    ConsoleContainerName,
							Command: state.Values.Deployment.Command,
							Args: append([]string{
								"--config.filepath=/etc/console/configs/config.yaml",
							}, state.Values.Deployment.ExtraArgs...),
							SecurityContext: &state.Values.SecurityContext,
							Image:           containerImage(state),
							ImagePullPolicy: state.Values.Image.PullPolicy,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: ContainerPort(state),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: volumeMounts,
							LivenessProbe: &corev1.Probe{
								InitialDelaySeconds: state.Values.LivenessProbe.InitialDelaySeconds, // TODO what to do with this??
								PeriodSeconds:       state.Values.LivenessProbe.PeriodSeconds,
								TimeoutSeconds:      state.Values.LivenessProbe.TimeoutSeconds,
								SuccessThreshold:    state.Values.LivenessProbe.SuccessThreshold,
								FailureThreshold:    state.Values.LivenessProbe.FailureThreshold,
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/admin/health",
										Port: intstr.FromString("http"),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								InitialDelaySeconds: state.Values.ReadinessProbe.InitialDelaySeconds,
								PeriodSeconds:       state.Values.ReadinessProbe.PeriodSeconds,
								TimeoutSeconds:      state.Values.ReadinessProbe.TimeoutSeconds,
								SuccessThreshold:    state.Values.ReadinessProbe.SuccessThreshold,
								FailureThreshold:    state.Values.ReadinessProbe.FailureThreshold,
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/admin/health",
										Port: intstr.FromString("http"),
									},
								},
							},
							Resources: state.Values.Resources,
							Env:       consoleContainerEnv(state),
							EnvFrom:   state.Values.ExtraEnvFrom,
						},
					}, state.Values.ExtraContainers...),
				},
			},
		},
	}
}

// ConsoleImage
func containerImage(state *RenderState) string {
	// Use a default if tag is not set
	tag := state.Values.Image.Tag
	if tag == "" {
		tag = AppVersion
	}

	image := fmt.Sprintf("%s:%s", state.Values.Image.Repository, tag)

	if !helmette.Empty(state.Values.Image.Registry) {
		return fmt.Sprintf("%s/%s", state.Values.Image.Registry, image)
	}

	return image
}

type PossibleEnvVar struct {
	Value  any
	EnvVar corev1.EnvVar
}

func consoleContainerEnv(state *RenderState) []corev1.EnvVar {
	if !state.Values.Secret.Create {
		vars := state.Values.ExtraEnv

		if state.Values.LicenseSecretRef != nil && !helmette.Empty(state.Values.LicenseSecretRef.Name) {
			vars = append(state.Values.ExtraEnv, corev1.EnvVar{
				Name: "LICENSE",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.Values.LicenseSecretRef.Name,
						},
						Key: helmette.Default("enterprise-license", state.Values.LicenseSecretRef.Key),
					},
				},
			})
		}

		return vars
	}

	possibleVars := []PossibleEnvVar{
		{
			Value: state.Values.Secret.Kafka.SASLPassword,
			EnvVar: corev1.EnvVar{
				Name: "KAFKA_SASL_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "kafka-sasl-password",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Serde.ProtobufGitBasicAuthPassword,
			EnvVar: corev1.EnvVar{
				Name: "SERDE_PROTOBUF_GIT_BASICAUTH_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "serde-protobuf-git-basicauth-password",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Kafka.AWSMSKIAMSecretKey,
			EnvVar: corev1.EnvVar{
				Name: "KAFKA_SASL_AWSMSKIAM_SECRETKEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "kafka-sasl-aws-msk-iam-secret-key",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Kafka.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-ca",
			},
		},
		{
			Value: state.Values.Secret.Kafka.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-cert",
			},
		},
		{
			Value: state.Values.Secret.Kafka.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "KAFKA_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/kafka-tls-key",
			},
		},
		{
			Value: state.Values.Secret.SchemaRegistry.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-ca",
			},
		},
		{
			Value: state.Values.Secret.SchemaRegistry.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-cert",
			},
		},
		{
			Value: state.Values.Secret.SchemaRegistry.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "SCHEMAREGISTRY_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/schemaregistry-tls-key",
			},
		},
		{
			Value: state.Values.Secret.SchemaRegistry.Password,
			EnvVar: corev1.EnvVar{
				Name: "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "schema-registry-password",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.SchemaRegistry.BearerToken,
			EnvVar: corev1.EnvVar{
				Name: "SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "schema-registry-bearertoken",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Authentication.JWTSigningKey,
			EnvVar: corev1.EnvVar{
				Name: "AUTHENTICATION_JWTSIGNINGKEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "authentication-jwt-signingkey",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.License,
			EnvVar: corev1.EnvVar{
				Name: "LICENSE",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "license",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Redpanda.AdminAPI.Password,
			EnvVar: corev1.EnvVar{
				Name: "REDPANDA_ADMINAPI_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: state.FullName(),
						},
						Key: "redpanda-admin-api-password",
					},
				},
			},
		},
		{
			Value: state.Values.Secret.Redpanda.AdminAPI.TLSCA,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_CAFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-ca",
			},
		},
		{
			Value: state.Values.Secret.Redpanda.AdminAPI.TLSKey,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_KEYFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-key",
			},
		},
		{
			Value: state.Values.Secret.Redpanda.AdminAPI.TLSCert,
			EnvVar: corev1.EnvVar{
				Name:  "REDPANDA_ADMINAPI_TLS_CERTFILEPATH",
				Value: "/etc/console/secrets/redpanda-admin-api-tls-cert",
			},
		},
	}

	vars := state.Values.ExtraEnv
	for _, possible := range possibleVars {
		if !helmette.Empty(possible.Value) {
			vars = append(vars, possible.EnvVar)
		}
	}

	return vars
}

func consolePodVolumes(state *RenderState) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "configs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: state.FullName(),
					},
				},
			},
		},
	}

	if state.Values.Secret.Create {
		volumes = append(volumes, corev1.Volume{
			Name: "secrets",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: state.FullName(),
				},
			},
		})
	}

	for _, mount := range state.Values.SecretMounts {
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

	return append(volumes, state.Values.ExtraVolumes...)
}
