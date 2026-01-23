// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:ignore=true
package console

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type RenderValues struct {
	ReplicaCount                 int32                             `json:"replicaCount"`
	NameOverride                 string                            `json:"nameOverride"`
	CommonLabels                 map[string]string                 `json:"commonLabels"`
	FullnameOverride             string                            `json:"fullnameOverride"`
	Image                        Image                             `json:"image"`
	ImagePullSecrets             []corev1.LocalObjectReference     `json:"imagePullSecrets"`
	AutomountServiceAccountToken bool                              `json:"automountServiceAccountToken"`
	ServiceAccount               ServiceAccountConfig              `json:"serviceAccount"`
	Annotations                  map[string]string                 `json:"annotations"`
	PodAnnotations               map[string]string                 `json:"podAnnotations"`
	PodLabels                    map[string]string                 `json:"podLabels"`
	PodSecurityContext           corev1.PodSecurityContext         `json:"podSecurityContext" partial:"builtin"`
	SecurityContext              corev1.SecurityContext            `json:"securityContext" partial:"builtin"`
	Service                      ServiceConfig                     `json:"service"`
	Ingress                      IngressConfig                     `json:"ingress"`
	Resources                    corev1.ResourceRequirements       `json:"resources"`
	Autoscaling                  AutoScaling                       `json:"autoscaling"`
	NodeSelector                 map[string]string                 `json:"nodeSelector"`
	Tolerations                  []corev1.Toleration               `json:"tolerations"`
	Affinity                     corev1.Affinity                   `json:"affinity"`
	TopologySpreadConstraints    []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints"`
	PriorityClassName            string                            `json:"priorityClassName"`
	// Config is a partial/fragment of console's configuration. There are two
	// possible sources of the types of depending on whether or not an
	// enterprise build is used. For simplicity, we opt to NOT types this
	// value.
	// Note that [PartialConfig] is the OSS version of console's config.
	Config              map[string]any            `json:"config"`
	ExtraEnv            []corev1.EnvVar           `json:"extraEnv"`
	ExtraEnvFrom        []corev1.EnvFromSource    `json:"extraEnvFrom"`
	ExtraVolumes        []corev1.Volume           `json:"extraVolumes"`
	ExtraVolumeMounts   []corev1.VolumeMount      `json:"extraVolumeMounts"`
	ExtraContainers     []corev1.Container        `json:"extraContainers"`
	ExtraContainerPorts []corev1.ContainerPort    `json:"extraContainerPorts"`
	InitContainers      InitContainers            `json:"initContainers"`
	SecretMounts        []SecretMount             `json:"secretMounts"`
	Secret              SecretConfig              `json:"secret"`
	LicenseSecretRef    *corev1.SecretKeySelector `json:"licenseSecretRef,omitempty"`
	LivenessProbe       corev1.Probe              `json:"livenessProbe" partial:"builtin"`
	ReadinessProbe      corev1.Probe              `json:"readinessProbe" partial:"builtin"`
	ConfigMap           Creatable                 `json:"configmap"`
	Deployment          DeploymentConfig          `json:"deployment"`
	Strategy            appsv1.DeploymentStrategy `json:"strategy"`
}

type DeploymentConfig struct {
	Create    bool     `json:"create"`
	Command   []string `json:"command,omitempty"`
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

type ServiceAccountConfig struct {
	Create                       bool              `json:"create"`
	AutomountServiceAccountToken bool              `json:"automountServiceAccountToken"`
	Annotations                  map[string]string `json:"annotations"`
	Name                         string            `json:"name"`
}

type ServiceConfig struct {
	Type        corev1.ServiceType `json:"type"`
	Port        int32              `json:"port"`
	NodePort    *int32             `json:"nodePort,omitempty"`
	TargetPort  *int32             `json:"targetPort,omitempty"`
	Annotations map[string]string  `json:"annotations"`
}

type IngressConfig struct {
	Enabled     bool                      `json:"enabled"`
	ClassName   *string                   `json:"className,omitempty"`
	Annotations map[string]string         `json:"annotations"`
	Hosts       []IngressHost             `json:"hosts"`
	TLS         []networkingv1.IngressTLS `json:"tls"`
}

type IngressHost struct {
	Host  string        `json:"host"`
	Paths []IngressPath `json:"paths"`
}

type IngressPath struct {
	Path     string                 `json:"path"`
	PathType *networkingv1.PathType `json:"pathType"`
}

type AutoScaling struct {
	Enabled                           bool   `json:"enabled"`
	MinReplicas                       int32  `json:"minReplicas"`
	MaxReplicas                       int32  `json:"maxReplicas"`
	TargetCPUUtilizationPercentage    *int32 `json:"targetCPUUtilizationPercentage"`
	TargetMemoryUtilizationPercentage *int32 `json:"targetMemoryUtilizationPercentage,omitempty"`
}

type InitContainers struct {
	ExtraInitContainers *string `json:"extraInitContainers"` // XXX Templated YAML
}

type SecretConfig struct {
	Create         bool                  `json:"create"`
	Kafka          KafkaSecrets          `json:"kafka"`
	Authentication AuthenticationSecrets `json:"authentication"`
	License        string                `json:"license"`
	Redpanda       RedpandaSecrets       `json:"redpanda"`
	Serde          SerdeSecrets          `json:"serde"`
	SchemaRegistry SchemaRegistrySecrets `json:"schemaRegistry"`
}

type SecretMount struct {
	Name        string  `json:"name"`
	SecretName  string  `json:"secretName"`
	Path        string  `json:"path"`
	SubPath     *string `json:"subPath,omitempty"`
	DefaultMode *int32  `json:"defaultMode"`
}

type KafkaSecrets struct {
	SASLPassword       *string `json:"saslPassword,omitempty"`
	AWSMSKIAMSecretKey *string `json:"awsMskIamSecretKey,omitempty"`
	TLSCA              *string `json:"tlsCa,omitempty"`
	TLSCert            *string `json:"tlsCert,omitempty"`
	TLSKey             *string `json:"tlsKey,omitempty"`
	TLSPassphrase      *string `json:"tlsPassphrase,omitempty"`
}

type SchemaRegistrySecrets struct {
	BearerToken *string `json:"bearerToken,omitempty"`
	Password    *string `json:"password,omitempty"`
	TLSCA       *string `json:"tlsCa,omitempty"`
	TLSCert     *string `json:"tlsCert,omitempty"`
	TLSKey      *string `json:"tlsKey,omitempty"`
}

type AuthenticationSecrets struct {
	JWTSigningKey string           `json:"jwtSigningKey"`
	OIDC          OIDCLoginSecrets `json:"oidc"`
}

type OIDCLoginSecrets struct {
	ClientSecret *string `json:"clientSecret,omitempty"`
}

type RedpandaSecrets struct {
	AdminAPI RedpandaAdminAPISecrets `json:"adminApi"`
}

type SerdeSecrets struct {
	ProtobufGitBasicAuthPassword *string `json:"protobufGitBasicAuthPassword,omitempty"`
}

type RedpandaAdminAPISecrets struct {
	Password *string `json:"password,omitempty"`
	TLSCA    *string `json:"tlsCa,omitempty"`
	TLSCert  *string `json:"tlsCert,omitempty"`
	TLSKey   *string `json:"tlsKey,omitempty"`
}

type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type Creatable struct {
	Create bool `json:"create"`
}

type Image struct {
	Registry   string            `json:"registry"`
	Repository string            `json:"repository"`
	PullPolicy corev1.PullPolicy `json:"pullPolicy"`
	Tag        string            `json:"tag"`
}
