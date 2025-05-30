// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
//go:build !generate

// +gotohelm:ignore=true
//
// Code generated by genpartial DO NOT EDIT.
package operator

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type PartialValues struct {
	NameOverride       *string                       "json:\"nameOverride,omitempty\""
	FullnameOverride   *string                       "json:\"fullnameOverride,omitempty\""
	ReplicaCount       *int32                        "json:\"replicaCount,omitempty\""
	ClusterDomain      *string                       "json:\"clusterDomain,omitempty\""
	Image              *PartialImage                 "json:\"image,omitempty\""
	Config             *PartialConfig                "json:\"config,omitempty\""
	ImagePullSecrets   []corev1.LocalObjectReference "json:\"imagePullSecrets,omitempty\""
	LogLevel           *string                       "json:\"logLevel,omitempty\""
	RBAC               *PartialRBAC                  "json:\"rbac,omitempty\""
	Webhook            *PartialWebhook               "json:\"webhook,omitempty\""
	ServiceAccount     *PartialServiceAccountConfig  "json:\"serviceAccount,omitempty\""
	Resources          *corev1.ResourceRequirements  "json:\"resources,omitempty\""
	NodeSelector       map[string]string             "json:\"nodeSelector,omitempty\""
	Tolerations        []corev1.Toleration           "json:\"tolerations,omitempty\""
	Affinity           *corev1.Affinity              "json:\"affinity,omitempty\" jsonschema:\"deprecated\""
	Strategy           *appsv1.DeploymentStrategy    "json:\"strategy,omitempty\""
	Annotations        map[string]string             "json:\"annotations,omitempty\""
	PodAnnotations     map[string]string             "json:\"podAnnotations,omitempty\""
	PodLabels          map[string]string             "json:\"podLabels,omitempty\""
	AdditionalCmdFlags []string                      "json:\"additionalCmdFlags,omitempty\""
	CommonLabels       map[string]string             "json:\"commonLabels,omitempty\""
	Monitoring         *PartialMonitoringConfig      "json:\"monitoring,omitempty\""
	WebhookSecretName  *string                       "json:\"webhookSecretName,omitempty\""
	PodTemplate        *PartialPodTemplateSpec       "json:\"podTemplate,omitempty\""
	LivenessProbe      *corev1.Probe                 "json:\"livenessProbe,omitempty\""
	ReadinessProbe     *corev1.Probe                 "json:\"readinessProbe,omitempty\""
	Scope              *OperatorScope                "json:\"scope,omitempty\" jsonschema:\"required,pattern=^(Namespace|Cluster)$,description=Sets the scope of the Redpanda Operator.\""
	CRDs               *PartialCRDs                  "json:\"crds,omitempty\""
}

type PartialImage struct {
	Repository *string            "json:\"repository,omitempty\""
	PullPolicy *corev1.PullPolicy "json:\"pullPolicy,omitempty\" jsonschema:\"required,pattern=^(Always|Never|IfNotPresent)$,description=The Kubernetes Pod image pull policy.\""
	Tag        *string            "json:\"tag,omitempty\""
}

type PartialConfig struct {
	ApiVersion     *string                      "json:\"apiVersion,omitempty\""
	Kind           *string                      "json:\"kind,omitempty\""
	Health         *PartialHealthConfig         "json:\"health,omitempty\""
	Metrics        *PartialMetricsConfig        "json:\"metrics,omitempty\""
	Webhook        *PartialWebhookConfig        "json:\"webhook,omitempty\""
	LeaderElection *PartialLeaderElectionConfig "json:\"leaderElection,omitempty\""
}

type PartialRBAC struct {
	Create                        *bool "json:\"create,omitempty\""
	CreateAdditionalControllerCRs *bool "json:\"createAdditionalControllerCRs,omitempty\""
	CreateRPKBundleCRs            *bool "json:\"createRPKBundleCRs,omitempty\""
}

type PartialWebhook struct {
	Enabled *bool "json:\"enabled,omitempty\""
}

type PartialServiceAccountConfig struct {
	Annotations                  map[string]string "json:\"annotations,omitempty\""
	AutomountServiceAccountToken *bool             "json:\"automountServiceAccountToken,omitempty\""
	Create                       *bool             "json:\"create,omitempty\""
	Name                         *string           "json:\"name,omitempty\""
}

type PartialMonitoringConfig struct {
	Enabled *bool "json:\"enabled,omitempty\""
}

type PartialCRDs struct {
	Enabled      *bool "json:\"enabled,omitempty\""
	Experimental *bool "json:\"experimental,omitempty\""
}

type PartialPodTemplateSpec struct {
	Metadata *PartialMetadata "json:\"metadata,omitempty\""
	Spec     *corev1.PodSpec  "json:\"spec,omitempty\" jsonschema:\"required\""
}

type PartialHealthConfig struct {
	HealthProbeBindAddress *string "json:\"healthProbeBindAddress,omitempty\""
}

type PartialMetricsConfig struct {
	BindAddress *string "json:\"bindAddress,omitempty\""
}

type PartialWebhookConfig struct {
	Port *int "json:\"port,omitempty\""
}

type PartialLeaderElectionConfig struct {
	LeaderElect  *bool   "json:\"leaderElect,omitempty\""
	ResourceName *string "json:\"resourceName,omitempty\""
}

type PartialMetadata struct {
	Labels      map[string]string "json:\"labels,omitempty\""
	Annotations map[string]string "json:\"annotations,omitempty\""
}
