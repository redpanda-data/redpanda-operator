// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

const (
	// PipelineDefaultImage is the default Redpanda Connect container image.
	PipelineDefaultImage = "docker.redpanda.com/redpandadata/connect:4.87.0"
)

// PipelinePhase describes the lifecycle phase of a Pipeline.
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Stopped;Unknown
type PipelinePhase string

const (
	// PipelinePhasePending indicates the pipeline has been accepted but
	// its Deployment has not yet been created.
	PipelinePhasePending PipelinePhase = "Pending"
	// PipelinePhaseProvisioning indicates the Deployment exists but not all
	// replicas are ready.
	PipelinePhaseProvisioning PipelinePhase = "Provisioning"
	// PipelinePhaseRunning indicates all desired replicas are ready and
	// processing data.
	PipelinePhaseRunning PipelinePhase = "Running"
	// PipelinePhaseStopped indicates the pipeline is paused (replicas scaled
	// to zero).
	PipelinePhaseStopped PipelinePhase = "Stopped"
	// PipelinePhaseUnknown is used when the controller cannot determine the
	// pipeline state.
	PipelinePhaseUnknown PipelinePhase = "Unknown"
)

// Pipeline condition types.
const (
	// PipelineConditionReady indicates whether the pipeline is fully
	// reconciled and running.
	PipelineConditionReady = "Ready"
)

// Pipeline condition reasons.
const (
	// PipelineReasonRunning means the pipeline is running with all replicas
	// available.
	PipelineReasonRunning = "Running"
	// PipelineReasonProvisioning means the Deployment is being rolled out.
	PipelineReasonProvisioning = "Provisioning"
	// PipelineReasonPaused means the pipeline is intentionally stopped.
	PipelineReasonPaused = "Paused"
	// PipelineReasonLicenseInvalid means the enterprise license check failed.
	PipelineReasonLicenseInvalid = "LicenseInvalid"
	// PipelineReasonFailed means a reconciliation step failed.
	PipelineReasonFailed = "Failed"
)

// PipelineSpec defines the desired state of a Redpanda Connect pipeline.
type PipelineSpec struct {
	// ConfigYAML is the Redpanda Connect pipeline configuration in YAML format.
	// This follows the standard Redpanda Connect configuration schema with
	// input, pipeline, and output sections.
	// +kubebuilder:validation:Required
	ConfigYAML string `json:"configYaml"`

	// DisplayName is a human-readable name for the pipeline.
	// Maps to the pipeline display name when migrating to Redpanda Cloud.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description is an optional description of what this pipeline does.
	// Maps to the pipeline description when migrating to Redpanda Cloud.
	// +optional
	Description string `json:"description,omitempty"`

	// Tags are key-value pairs for organizing and filtering pipelines.
	// Maps to pipeline tags when migrating to Redpanda Cloud.
	// +optional
	Tags map[string]string `json:"tags,omitempty"`

	// ConfigFiles defines additional configuration files to mount alongside
	// the main pipeline configuration. Each entry maps a filename to its content.
	// Files are mounted in the /config directory alongside connect.yaml.
	// The key "connect.yaml" is reserved and cannot be used.
	// Maps to pipeline config files when migrating to Redpanda Cloud.
	// +optional
	ConfigFiles map[string]string `json:"configFiles,omitempty"`

	// Replicas is the number of pipeline replicas to run.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Image is the container image for the Redpanda Connect deployment.
	// +optional
	Image *string `json:"image,omitempty"`

	// Paused stops the pipeline by scaling replicas to zero when set to true.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Resources defines the compute resource requirements for the pipeline pods.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Env specifies additional environment variables for the pipeline container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// SecretRef is a list of Kubernetes Secrets to expose as environment variables
	// in the pipeline container. All key-value pairs in each referenced Secret are
	// injected as environment variables, allowing the pipeline configuration to
	// reference them using Redpanda Connect's ${!env:VAR_NAME} or ${VAR_NAME}
	// interpolation syntax.
	//
	// For example, if a Secret "my-creds" contains key "KAFKA_PASSWORD", the pipeline
	// configYaml can reference it as:
	//   password: ${KAFKA_PASSWORD}
	//
	// See: https://docs.redpanda.com/redpanda-connect/configuration/secrets/
	// +optional
	SecretRef []corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// Annotations specifies additional annotations to apply to the pipeline pod
	// template. These are merged with any operator-level commonAnnotations, with
	// per-pipeline annotations taking precedence. Useful for integrations like
	// Datadog autodiscovery that rely on pod annotations.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Tolerations for the pipeline pods, allowing them to be scheduled on tainted nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// NodeSelector constrains pipeline pods to nodes with matching labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// TopologySpreadConstraints controls how pipeline pods are spread across
	// topology domains such as availability zones. When Zones is specified,
	// a default topology spread constraint is generated automatically.
	// Any constraints specified here are used in addition to (or instead of)
	// the auto-generated zone constraint.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Zones specifies the availability zones across which pipeline pods should
	// be spread. When set, the controller configures:
	//   - A node affinity to schedule pods only on nodes in these zones
	//   - A topology spread constraint to distribute pods evenly across zones
	// The zone label used is "topology.kubernetes.io/zone".
	// +optional
	Zones []string `json:"zones,omitempty"`

	// ClusterSource is a reference to the Redpanda cluster this pipeline connects to.
	// +optional
	ClusterSource *ClusterSource `json:"cluster,omitempty"`
}

// PipelineStatus defines the observed state of a Connect resource.
type PipelineStatus struct {
	// ObservedGeneration is the last observed generation of the Connect resource.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Connect resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase describes the current phase of the pipeline lifecycle.
	// +optional
	Phase PipelinePhase `json:"phase,omitempty"`

	// Replicas is the number of desired replicas.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready pipeline pods.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// Connect defines a Redpanda Connect pipeline managed by the operator.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=pipelines,shortName=rpcn
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion
type Pipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of the Connect pipeline.
	Spec PipelineSpec `json:"spec,omitempty"`

	// Status represents the current observed state of the Connect pipeline.
	Status PipelineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PipelineList contains a list of Connect resources.
type PipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Pipeline `json:"items"`
}

func (c *PipelineList) GetItems() []*Pipeline {
	return functional.MapFn(ptr.To, c.Items)
}

// GetClusterSource returns the cluster source reference if set.
func (c *Pipeline) GetClusterSource() *ClusterSource {
	return c.Spec.ClusterSource
}

// GetImage returns the configured image or the default.
func (c *Pipeline) GetImage() string {
	if c.Spec.Image != nil && *c.Spec.Image != "" {
		return *c.Spec.Image
	}
	return PipelineDefaultImage
}

// GetReplicas returns the effective replica count, respecting the paused state.
func (c *Pipeline) GetReplicas() int32 {
	if c.Spec.Paused {
		return 0
	}
	if c.Spec.Replicas != nil {
		return *c.Spec.Replicas
	}
	return 1
}
