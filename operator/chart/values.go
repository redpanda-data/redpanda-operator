// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_values.go.tpl
package operator

import (
	_ "embed"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed values.yaml
	DefaultValuesYAML []byte

	//go:embed values.schema.json
	ValuesSchemaJSON []byte
)

type Peer struct {
	Name                 string            `json:"name,omitempty" jsonschema:"required"`
	Address              string            `json:"address,omitempty" jsonschema:"required"`
	AdditionalAnnotation map[string]string `json:"additionalAnnotation,omitempty"`
	SelectorOverwrite    map[string]string `json:"selectorOverwrite,omitempty"`
}

// MulticlusterService configures an optional peer-facing Service in
// front of the operator Deployment. The chart does not attempt to
// manage flat-network pod-IP endpoint syncing — that has a chicken-and-
// egg problem where operators must be reachable before they can learn
// peer addresses — so only Service-level exposure is supported here.
type MulticlusterService struct {
	// Enabled renders a per-operator Service alongside the Deployment.
	// Defaults to false so existing installs that provision their own
	// Service keep working unchanged.
	Enabled bool `json:"enabled"`
	// Type is the Kubernetes Service type. Only ClusterIP and
	// LoadBalancer are supported — ClusterIP for in-cluster mesh routing
	// (Cilium ClusterMesh, Submariner, Istio, MCS) and LoadBalancer for
	// cross-cloud deployments without a service mesh. Headless
	// (ClusterIP=None) is intentionally not offered: the operator runs
	// as a single-replica Deployment, so per-pod DNS is useless and
	// would actively misroute when multiple replicas exist.
	Type corev1.ServiceType `json:"type" jsonschema:"pattern=^(ClusterIP|LoadBalancer)$"`
	// Annotations are merged onto the generated Service metadata. Use
	// them to inject mesh-specific hints or cloud LB tuning — for
	// example, {"service.cilium.io/global":"true"} for Cilium ClusterMesh
	// or {"service.beta.kubernetes.io/aws-load-balancer-type":"nlb"}
	// for AWS NLB.
	Annotations map[string]string `json:"annotations"`
	// MCS additionally renders a Multi-Cluster Services ServiceExport
	// for the local Service and one ServiceImport per peer, so the
	// operator becomes reachable at
	// `<peer>.<namespace>.svc.clusterset.local` via a compliant MCS
	// controller (Submariner Lighthouse, GKE MCS, …).
	MCS bool `json:"mcs"`
}

type Multicluster struct {
	Enabled                      bool                `json:"enabled"`
	ServicePerOperatorDeployment bool                `json:"servicePerOperatorDeployment"`
	Service                      MulticlusterService `json:"service"`
	Name                         string              `json:"name"`
	KubernetesAPIExternalAddress string              `json:"apiServerExternalAddress"`
	Peers                        []Peer              `json:"peers"`
}

type Enterprise struct {
	LicenseSecretRef *corev1.SecretKeySelector `json:"licenseSecretRef,omitempty"`
}

type Values struct {
	Enterprise            *Enterprise                   `json:"enterprise,omitempty"`
	NameOverride          string                        `json:"nameOverride"`
	FullnameOverride      string                        `json:"fullnameOverride"`
	ReplicaCount          int32                         `json:"replicaCount"`
	ClusterDomain         string                        `json:"clusterDomain"`
	Image                 Image                         `json:"image"`
	Config                Config                        `json:"config"`
	ImagePullSecrets      []corev1.LocalObjectReference `json:"imagePullSecrets"`
	LogLevel              string                        `json:"logLevel"`
	RBAC                  RBAC                          `json:"rbac"`
	Webhook               Webhook                       `json:"webhook"`
	ServiceAccount        ServiceAccountConfig          `json:"serviceAccount"`
	Resources             corev1.ResourceRequirements   `json:"resources"`
	NodeSelector          map[string]string             `json:"nodeSelector"`
	Tolerations           []corev1.Toleration           `json:"tolerations"`
	Affinity              *corev1.Affinity              `json:"affinity" jsonschema:"deprecated"`
	Strategy              appsv1.DeploymentStrategy     `json:"strategy"`
	Annotations           map[string]string             `json:"annotations,omitempty"`
	PodAnnotations        map[string]string             `json:"podAnnotations"`
	PodLabels             map[string]string             `json:"podLabels"`
	AdditionalCmdFlags    []string                      `json:"additionalCmdFlags"`
	CommonLabels          map[string]string             `json:"commonLabels"`
	Monitoring            MonitoringConfig              `json:"monitoring"`
	WebhookSecretName     string                        `json:"webhookSecretName"`
	PodTemplate           *PodTemplateSpec              `json:"podTemplate,omitempty"`
	LivenessProbe         *corev1.Probe                 `json:"livenessProbe,omitempty"`
	ReadinessProbe        *corev1.Probe                 `json:"readinessProbe,omitempty"`
	CRDs                  CRDs                          `json:"crds"`
	VectorizedControllers VectorizedControllers         `json:"vectorizedControllers"`
	Multicluster          Multicluster                  `json:"multicluster"`
}

type VectorizedControllers struct {
	Enabled bool `json:"enabled"`
}

type CRDs struct {
	Enabled      bool `json:"enabled"`
	Experimental bool `json:"experimental"`
}

type PodTemplateSpec struct {
	Metadata Metadata       `json:"metadata,omitempty"`
	Spec     corev1.PodSpec `json:"spec,omitempty" jsonschema:"required"`
}

type Metadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type Image struct {
	Repository string            `json:"repository"`
	PullPolicy corev1.PullPolicy `json:"pullPolicy" jsonschema:"required,pattern=^(Always|Never|IfNotPresent)$,description=The Kubernetes Pod image pull policy."`
	Tag        *string           `json:"tag,omitempty"`
}

type KubeRBACProxyConfig struct {
	LogLevel int   `json:"logLevel"`
	Image    Image `json:"image"`
}

type Config struct {
	//nolint:stylecheck
	ApiVersion     string               `json:"apiVersion"`
	Kind           string               `json:"kind"`
	Health         HealthConfig         `json:"health"`
	Metrics        MetricsConfig        `json:"metrics"`
	Webhook        WebhookConfig        `json:"webhook"`
	LeaderElection LeaderElectionConfig `json:"leaderElection"`
}

type HealthConfig struct {
	HealthProbeBindAddress string `json:"healthProbeBindAddress"`
}

type MetricsConfig struct {
	BindAddress string `json:"bindAddress"`
}

type WebhookConfig struct {
	Port int `json:"port"`
}

type LeaderElectionConfig struct {
	LeaderElect  bool   `json:"leaderElect"`
	ResourceName string `json:"resourceName"`
}

type RBAC struct {
	Create                        bool `json:"create"`
	CreateAdditionalControllerCRs bool `json:"createAdditionalControllerCRs"`
}

type Webhook struct {
	Enabled bool `json:"enabled"`
}

type ServiceAccountConfig struct {
	Annotations                  map[string]string `json:"annotations,omitempty"`
	AutomountServiceAccountToken *bool             `json:"automountServiceAccountToken,omitempty"`
	Create                       bool              `json:"create"`
	Name                         *string           `json:"name,omitempty"`
}

type MonitoringConfig struct {
	Enabled bool `json:"enabled"`
}
