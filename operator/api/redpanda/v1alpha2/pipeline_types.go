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
	// PipelineConditionConfigValid indicates whether the pipeline
	// configuration passed lint validation.
	PipelineConditionConfigValid = "ConfigValid"
	// PipelineConditionClusterRef indicates whether the referenced
	// Redpanda cluster was resolved successfully.
	PipelineConditionClusterRef = "ClusterRef"
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
	// PipelineReasonConfigValid means the config passed lint validation.
	PipelineReasonConfigValid = "ConfigValid"
	// PipelineReasonConfigInvalid means the config failed lint validation.
	PipelineReasonConfigInvalid = "ConfigInvalid"
	// PipelineReasonClusterRefResolved means the clusterRef was resolved successfully.
	PipelineReasonClusterRefResolved = "ClusterRefResolved"
	// PipelineReasonClusterRefInvalid means the clusterRef could not be found or resolved.
	PipelineReasonClusterRefInvalid = "ClusterRefInvalid"
	// PipelineReasonUserResolved means the userRef was resolved successfully and
	// its password Secret was located.
	PipelineReasonUserResolved = "UserResolved"
	// PipelineReasonUserInvalid means the userRef could not be found or its
	// password Secret was missing.
	PipelineReasonUserInvalid = "UserInvalid"
	// PipelineReasonValueSourcesResolved means every entry in spec.valueSources
	// was bound successfully.
	PipelineReasonValueSourcesResolved = "ValueSourcesResolved"
	// PipelineReasonValueSourceInvalid means at least one entry in
	// spec.valueSources could not be resolved.
	PipelineReasonValueSourceInvalid = "ValueSourceInvalid"
)

// Pipeline status condition types added with the v2 spec.
const (
	// PipelineConditionUserRef indicates whether the referenced User CR was
	// resolved and had a usable password Secret.
	PipelineConditionUserRef = "UserRef"
	// PipelineConditionValueSourcesResolved indicates whether every
	// spec.valueSources entry resolved to a backing value.
	PipelineConditionValueSourcesResolved = "ValueSourcesResolved"
)

// PipelineSpec defines the desired state of a Redpanda Connect pipeline.
//
// +kubebuilder:validation:XValidation:message="userRef must be empty when cluster.staticConfiguration is set",rule="!has(self.cluster) || !has(self.cluster.staticConfiguration) || !has(self.userRef)"
// +kubebuilder:validation:XValidation:message="userRef cannot be set without cluster.clusterRef",rule="!has(self.userRef) || (has(self.cluster) && has(self.cluster.clusterRef))"
type PipelineSpec struct {
	// ConfigYAML is the user-supplied Redpanda Connect pipeline YAML.
	// Reference cluster-bound or sensitive values from .valueSources via
	// ${NAME} interpolation; the operator resolves them at render time.
	//
	// When .cluster is set, the operator inline-merges connection fields
	// (seed_brokers, tls, sasl) into any `input.redpanda` and
	// `output.redpanda` blocks in this YAML, derived from the resolved
	// cluster connection and .userRef. Users only need to write the
	// per-plugin fields (topic, key, consumer_group, etc.); brokers, TLS,
	// and SASL are filled in by the operator.
	//
	// User-side keys win on conflict — set a key explicitly (for example,
	// seed_brokers pointing at a different cluster) and the operator's
	// generated value is skipped for that key.
	//
	// The merge targets the `redpanda` input/output plugins specifically.
	// Any `redpanda_common` blocks the user authors are passed through
	// unchanged — the operator does not inject connection fields into
	// them.
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

	// ServiceAccountName is the ServiceAccount to bind to the pipeline pod.
	// When unset, the namespace's default ServiceAccount is used.
	//
	// Setting this is the recommended way to scope per-pipeline cloud-IAM
	// trust (e.g. IRSA on EKS, Workload Identity on GKE, Pod Identity on
	// AKS). Annotating the namespace's default SA works but grants every
	// pipeline in the namespace the same role — naming a Pipeline-specific
	// SA here keeps the trust boundary per-pipeline.
	//
	// The operator does NOT create the ServiceAccount; provision it
	// (along with the appropriate cloud-IAM annotations) out-of-band.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Paused stops the pipeline by scaling replicas to zero when set to true.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Resources defines the compute resource requirements for the pipeline pods.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ValueSources is a list of named values the pipeline YAML can reference
	// via ${NAME} interpolation. Each value is fetched at render time from
	// inline / ConfigMap / Secret / ExternalSecret and projected into the
	// pipeline pod as an environment variable. One named pull per entry —
	// avoids the bag-of-Secrets env-splat pattern.
	//
	// Example:
	//   spec:
	//     valueSources:
	//       - name: S3_SECRET_KEY
	//         source:
	//           secretKeyRef:
	//             name: s3-creds
	//             key: secret_access_key
	//     configYaml: |
	//       output:
	//         aws_s3:
	//           bucket: my-bucket
	//           credentials:
	//             secret: ${S3_SECRET_KEY}
	//
	// See: https://docs.redpanda.com/redpanda-connect/configuration/secrets/
	// +optional
	// +listType=map
	// +listMapKey=name
	ValueSources []NamedValueSource `json:"valueSources,omitempty"`

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

	// Budget configures a PodDisruptionBudget for the pipeline Deployment,
	// protecting pipeline pods from voluntary disruptions such as node drains
	// and cluster autoscaler evictions. When not set, no PDB is created.
	// +optional
	Budget *PipelineBudget `json:"budget,omitempty"`

	// ClusterSource declaratively binds the pipeline's redpanda input/output
	// to a Redpanda cluster. Mirrors the ClusterSource pattern used by the
	// User/Topic CRDs:
	//
	//   - clusterRef: point at an existing Redpanda CR by name. The operator
	//     resolves the internal broker addresses + TLS material automatically;
	//     the SASL identity is taken from .userRef.
	//   - staticConfiguration: hard-code brokers, TLS, and SASL. The password
	//     is a ValueSource so it can come from inline / Secret / ConfigMap /
	//     ExternalSecret.
	//
	// When unset, the pipeline runs against whatever brokers the user wires
	// inline in configYaml (e.g. an external Kafka, Confluent Cloud, etc.).
	// +optional
	ClusterSource *ClusterSource `json:"cluster,omitempty"`

	// UserRef binds the pipeline to a User CR. When set alongside
	// .cluster.clusterRef, the operator reads the referenced User's
	// password Secret + SASL mechanism and uses the User's metadata.name
	// as the SASL username, emitting REDPANDA_SASL_USERNAME / _PASSWORD /
	// _MECHANISM env vars in the pipeline pod and a `sasl:` block in the
	// auto-generated `redpanda` config.
	//
	// Set this when the cluster the pipeline talks to has SASL enabled.
	// On unauthenticated clusters (and in clusterRef-only modes that
	// only need broker discovery), leave it empty.
	//
	// CEL restrictions:
	//   - userRef must NOT be set alongside .cluster.staticConfiguration —
	//     the static path carries its own inline SASL config.
	//   - userRef must NOT be set without .cluster.clusterRef — there's no
	//     cluster context to authenticate against otherwise.
	//
	// The referenced User CR is expected to live in the same namespace as
	// the Pipeline and to declare ACLs scoped to the topics, schema
	// subjects, and consumer groups this pipeline reads/writes. The
	// operator does NOT auto-create or modify the User CR — ACL scoping
	// stays an explicit, auditable user-controlled action.
	// +optional
	UserRef *PipelineUserRef `json:"userRef,omitempty"`
}

// PipelineUserRef points at a User CR whose password Secret + SCRAM
// mechanism the pipeline will use to authenticate to Redpanda.
type PipelineUserRef struct {
	// Name of the User CR (in the same namespace as the Pipeline).
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// NamedValueSource binds a name to a value provider so the pipeline YAML
// can reference it via ${NAME} interpolation.
type NamedValueSource struct {
	// Name is the environment-variable name the pipeline YAML references.
	// Must match standard env-var characters: [A-Z_][A-Z0-9_]*.
	// +kubebuilder:validation:Pattern=`^[A-Z_][A-Z0-9_]*$`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Source is the value provider. Exactly one of inline / configMapKeyRef
	// / secretKeyRef / externalSecretRef must be set; the ValueSource
	// XValidation rules enforce this.
	Source ValueSource `json:"source"`
}

// PipelineBudget configures a PodDisruptionBudget for the pipeline.
type PipelineBudget struct {
	// MaxUnavailable defines the maximum number of pipeline pods that can be
	// unavailable during a voluntary disruption. Defaults to 1 if not set.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	MaxUnavailable int `json:"maxUnavailable"`
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
