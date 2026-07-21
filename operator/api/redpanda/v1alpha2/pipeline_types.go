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
	PipelineDefaultImage = "docker.redpanda.com/redpandadata/connect:4.100.0"
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
	// PipelineConditionLicense indicates whether the operator-level
	// enterprise license passed validation. A False status stops the
	// controller from syncing spec changes but never tears down a running
	// workload, so Ready can remain True while License is False.
	PipelineConditionLicense = "License"
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
	// PipelineReasonLicenseValid means the enterprise license check passed.
	PipelineReasonLicenseValid = "LicenseValid"
	// PipelineReasonFailed means a reconciliation step failed.
	PipelineReasonFailed = "Failed"
	// PipelineReasonNameConflict means a resource this Pipeline would create
	// (ConfigMap, Deployment, Secret, ...) already exists and is not owned by
	// this Pipeline. The controller refuses to adopt or overwrite it.
	PipelineReasonNameConflict = "NameConflict"
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
// +kubebuilder:validation:XValidation:message="cluster.clusterRef.namespace is not supported for pipelines; the referenced Redpanda must live in the Pipeline's namespace",rule="!has(self.cluster) || !has(self.cluster.clusterRef) || !has(self.cluster.clusterRef.namespace)"
// +kubebuilder:validation:XValidation:message="cluster.clusterRef.group must be cluster.redpanda.com (or unset) for pipelines",rule="!has(self.cluster) || !has(self.cluster.clusterRef) || !has(self.cluster.clusterRef.group) || self.cluster.clusterRef.group == 'cluster.redpanda.com'"
// +kubebuilder:validation:XValidation:message="cluster.clusterRef.kind must be Redpanda (or unset) for pipelines",rule="!has(self.cluster) || !has(self.cluster.clusterRef) || !has(self.cluster.clusterRef.kind) || self.cluster.clusterRef.kind == 'Redpanda'"
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
	//
	// This field backs the Pipeline's scale subresource, so `kubectl scale
	// pipeline/<name>`, HorizontalPodAutoscaler, and KEDA ScaledObjects all
	// read and write it through `/scale`. To autoscale a pipeline, point the
	// autoscaler at the Pipeline itself — NOT at its Deployment, whose
	// replica count the operator continuously resets to this field:
	//
	//   scaleTargetRef:
	//     apiVersion: cluster.redpanda.com/v1alpha2
	//     kind: Pipeline
	//     name: <pipeline-name>
	//
	// CPU and memory HPAs work out of the box: pipeline pods always carry
	// resource requests (operator defaults apply when .resources is unset).
	// To scale on the metrics Redpanda Connect itself emits (input_received,
	// output_sent, processor_latency_ns, ...) feed them to the autoscaler —
	// e.g. scrape the pods' `http` port at /metrics (the operator's
	// monitoring PodMonitor does this) into Prometheus, then use
	// prometheus-adapter for HPA custom metrics or a KEDA prometheus
	// trigger.
	//
	// When an autoscaler manages this field, omit it from applied manifests
	// (or have your GitOps tool ignore it) so config syncs don't undo the
	// autoscaler's writes.
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
	//
	// Paused wins over .replicas, including values an autoscaler writes
	// through the scale subresource: while paused the Deployment stays at
	// zero (the pipeline reports Stopped) even if HPA/KEDA keep updating
	// .replicas, and unpausing resumes at the current .replicas count.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Resources defines the compute resource requirements for the pipeline pods.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// ExtraInitContainers are additional init containers run to completion,
	// in order, before the pipeline's lint and connect containers start. Use
	// them for setup steps the pipeline depends on — fetching certificate
	// material, warming a cache, or waiting on an external dependency. They run
	// ahead of the operator's built-in `lint` init container, so anything they
	// write into a volume from .extraVolumes is visible to lint and to the
	// connect runtime (mount it into the pipeline via .extraVolumeMounts).
	//
	// This is a raw container passthrough with no operator-applied policy; the
	// pod's service account and security posture apply. It is distinct from a
	// long-lived plugin sidecar (which would be an init container with
	// restartPolicy: Always and is not expressed here).
	//
	// Example (with the backing volume and the mount that exposes the staged
	// file to the pipeline containers):
	//   spec:
	//     extraVolumes:
	//       - name: shared
	//         emptyDir: {}
	//     extraVolumeMounts:
	//       - name: shared
	//         mountPath: /shared
	//         readOnly: true
	//     extraInitContainers:
	//       - name: fetch-certs
	//         image: curlimages/curl:8.11.0
	//         command: ["sh", "-c", "curl -fsSL $CERT_URL -o /shared/ca.pem"]
	//         volumeMounts:
	//           - name: shared
	//             mountPath: /shared
	// +optional
	ExtraInitContainers []corev1.Container `json:"extraInitContainers,omitempty"`

	// ExtraVolumes are additional volumes added to the pipeline pod, typically
	// backing an .extraInitContainers staging step or an .extraVolumeMounts
	// entry. Raw passthrough: any Kubernetes volume source is accepted. The
	// names "config", "cluster-tls-ca", and "cluster-tls-client" are reserved
	// by the operator.
	// +optional
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// ExtraVolumeMounts are additional volume mounts applied to the built-in
	// lint init container and the connect container (both, so the linted view
	// of the filesystem matches the runtime's). Mounts for
	// .extraInitContainers are declared on those containers directly.
	// +optional
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`

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

	// NodeSelector constrains pipeline pods to nodes with matching labels — the
	// simplest way to pin a pipeline to a specific Kubernetes node pool (match
	// the node pool's label, e.g. eks.amazonaws.com/nodegroup or a custom
	// label). Combine with Tolerations if the node pool is tainted.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity sets pod affinity/anti-affinity for the pipeline pods. Use it for
	// node-pool scheduling that NodeSelector can't express — required-OR-of-pools
	// (multiple acceptable node pools), preferred (soft) node-pool placement, or
	// pod anti-affinity to spread a pipeline across nodes. It is merged with any
	// auto-generated zone affinity from Zones: the zone requirement is AND-ed
	// into the node affinity so both constraints apply.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

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
	//   - clusterRef: point at an existing Redpanda CR by name, in the same
	//     namespace as the Pipeline (cross-namespace references are rejected —
	//     the resolved TLS/SASL Secrets must be mountable by the pipeline
	//     pod). The operator resolves the internal broker addresses + TLS
	//     material automatically; the SASL identity is taken from .userRef.
	//     SECURITY: when .userRef is NOT set and the referenced cluster has
	//     SASL enabled, the pipeline authenticates as the cluster's bootstrap
	//     superuser — its credentials are projected into the pipeline pod's
	//     environment. Set .userRef so each pipeline runs with an ACL-scoped
	//     identity instead.
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

// PipelineStatus defines the observed state of a Pipeline.
type PipelineStatus struct {
	// ObservedGeneration is the last observed generation of the Pipeline.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Pipeline.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase describes the current phase of the pipeline lifecycle.
	// +optional
	Phase PipelinePhase `json:"phase,omitempty"`

	// Replicas is the number of pipeline pods observed on the underlying
	// Deployment. The scale subresource reports it as status.replicas, which
	// is how HPA and KEDA observe the pipeline's current scale.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the number of ready pipeline pods.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Selector is the label selector for this pipeline's pods, in string
	// form. The scale subresource reports it as status.selector, which is
	// how HPA and KEDA discover the pods backing this Pipeline when
	// computing per-pod (cpu/memory/custom) metrics.
	// +optional
	Selector string `json:"selector,omitempty"`
}

// Pipeline defines a Redpanda Connect pipeline managed by the operator.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
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

	// Spec defines the desired state of the Pipeline.
	Spec PipelineSpec `json:"spec,omitempty"`

	// Status represents the current observed state of the Pipeline.
	Status PipelineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PipelineList contains a list of Pipeline resources.
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

// GetImage returns the configured image, falling back to the binary-baked
// default. NOTE: this cannot see the operator-level --connect-default-image
// override, which takes precedence over PipelineDefaultImage at render time —
// callers that need the effective image must resolve that tier themselves
// (see the pipeline controller's resolveImage and the telemetry collector's
// resolvePipelineImage).
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
