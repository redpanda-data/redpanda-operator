// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"time"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	// InternalListenerName is name of internal listener
	InternalListenerName = "kafka"
	// ExternalListenerName is name of external listener
	ExternalListenerName = "kafka-external"
	// SchemaRegistryExternalListenerName is the default name of a schema registry listener
	SchemaRegistryExternalListenerName = "schema-registry"
	// PandaproxyExternalListenerName is the default name of an external pandaproxy listener
	PandaproxyExternalListenerName = "proxy-external"
)

const DefaultNodePoolName = "default"

// RedpandaResourceRequirements extends corev1.ResourceRequirements
// to allow specification of resources directly passed to Redpanda that
// are different to Requests or Limits.
type RedpandaResourceRequirements struct {
	corev1.ResourceRequirements `json:""`
	// Redpanda describes the amount of compute resources passed to redpanda.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Redpanda corev1.ResourceList `json:"redpanda,omitempty"`
}

// RedpandaCPU returns a copy of the rounded value for Redpanda CPU
//
// If it's not explicitly set, the Request.CPU is used. This allows
// overprovisioning the CPU, which is not recommended, but --smp can't be
// reduced on an update.
//
// The value returned is:
// * Is rounded up to an integer.
// * Is limited by 2Gi per core if requests.memory is set.
//
// Example:
//
//	in: minimum requirement per core, 2GB
//	in: Requests.Memory, 16GB
//	=> maxAllowedCores = 8
//	if requestedCores == 8, set smp = 8 (with 2GB per core)
//	if requestedCores == 4, set smp = 4 (with 4GB per core)
func (r *RedpandaResourceRequirements) RedpandaCPU() *resource.Quantity {
	q := r.Redpanda.Cpu()
	if q == nil || q.IsZero() {
		requestedMemory := r.Requests.Memory().Value()
		requestedCores := r.Requests.Cpu().Value()
		maxAllowedCores := requestedMemory / MinimumMemoryPerCore
		smp := maxAllowedCores
		if smp == 0 || requestedCores < smp {
			smp = requestedCores
		}
		q = resource.NewQuantity(smp, resource.BinarySI)
	}
	qd := q.DeepCopy()
	qd.RoundUp(0)
	return &qd
}

// RedpandaMemory returns a copy of the value for Redpanda Memory
//
// If it's not explicitly set, the Request.Memory is used.
func (r *RedpandaResourceRequirements) RedpandaMemory() *resource.Quantity {
	q := r.Redpanda.Memory()
	if q == nil || q.IsZero() {
		requestedMemory := r.Requests.Memory().Value()
		requestedMemory = int64(float64(requestedMemory) * RedpandaMemoryAllocationRatio)
		q = resource.NewQuantity(requestedMemory, resource.BinarySI)
	}
	qd := q.DeepCopy()
	return &qd
}

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// If specified, Redpanda Pod annotations
	Annotations map[string]string `json:"annotations,omitempty"`
	// Image is the fully qualified name of the Redpanda container
	Image string `json:"image,omitempty"`
	// Version is the Redpanda container tag
	Version string `json:"version,omitempty"`
	// Replicas determine how big the cluster will be.
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`
	// PriorityClassName is used to set the PodSpec.PriorityClassName of the redpanda Statefulset.
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// PodDisruptionBudget specifies whether PDB resource should be created for
	// the cluster and how should it be configured. By default this is enabled
	// and defaults to MaxUnavailable=1
	PodDisruptionBudget *PDBConfig `json:"podDisruptionBudget,omitempty"`
	// Resources used by redpanda process running in container. Beware that
	// there are multiple containers running in the redpanda pod and these can
	// be enabled/disabled and configured from the `sidecars` field. These
	// containers have separate resources settings and the amount of resources
	// assigned to these containers will be required on the cluster on top of
	// the resources defined here
	Resources RedpandaResourceRequirements `json:"resources,omitempty"`
	// Sidecars is list of sidecars run alongside redpanda container
	Sidecars Sidecars `json:"sidecars,omitempty"`
	// Configuration represent redpanda specific configuration
	Configuration RedpandaConfig `json:"configuration,omitempty"`
	// If specified, Redpanda Pod tolerations
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// If specified, Redpanda Pod node selectors. For reference please visit
	// https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Storage spec for cluster
	Storage StorageSpec `json:"storage,omitempty"`
	// Cloud storage configuration for cluster
	CloudStorage CloudStorageConfig `json:"cloudStorage,omitempty"`
	// List of superusers
	Superusers []Superuser `json:"superUsers,omitempty"`
	// SASL enablement flag
	// +optional
	// Deprecated: replaced by "kafkaEnableAuthorization"
	EnableSASL bool `json:"enableSasl,omitempty"`
	// Enable authorization for Kafka connections. Values are:
	//
	// - `nil`: Ignored. Authorization is enabled with `enable_sasl: true`
	//
	// - `true`: authorization is required
	//
	// - `false`: authorization is disabled;
	//
	// See also `enableSasl` and `configuration.kafkaApi[].authenticationMethod`
	KafkaEnableAuthorization *bool `json:"kafkaEnableAuthorization,omitempty"`
	// For configuration parameters not exposed, a map can be provided for string values.
	// Such values are passed transparently to Redpanda. The key format is "<subsystem>.field", e.g.,
	//
	// additionalConfiguration:
	//   redpanda.enable_idempotence: "true"
	//   redpanda.default_topic_partitions: "3"
	//   pandaproxy_client.produce_batch_size_bytes: "2097152"
	//
	// Notes:
	// 1. versioning is not supported for map keys
	// 2. key names not supported by Redpanda will lead to failure on start up
	// 3. updating this map requires a manual restart of the Redpanda pods. Please be aware of
	// sync period when one Redpanda POD is restarted
	// 4. cannot have keys that conflict with existing struct fields - it leads to panic
	//
	// By default if Replicas is 3 or more and redpanda.default_topic_partitions is not set
	// default webhook is setting redpanda.default_topic_partitions to 3.
	AdditionalConfiguration map[string]string `json:"additionalConfiguration,omitempty"`
	// Cluster configuration values may also be held here. A `keyName` entry in this
	// attribute will override corresponding `redpanda.keyName` entries in AdditionalConfiguration;
	// this is to permit the migration of those settings to this attribute.
	// The configuration may contain references to values extracted from k8s ConfigMaps or Secrets;
	// furthermore, we support the fetching of provider-specific secrets directly.
	ClusterConfiguration ClusterConfiguration `json:"clusterConfiguration,omitempty"`
	// DNSTrailingDotDisabled gives ability to turn off the fully-qualified
	// DNS name.
	// http://www.dns-sd.org/trailingdotsindomainnames.html
	DNSTrailingDotDisabled bool `json:"dnsTrailingDotDisabled,omitempty"`
	// RestartConfig allows to control the behavior of the cluster when restarting
	RestartConfig *RestartConfig `json:"restartConfig,omitempty"`

	// If key is not provided in the SecretRef, Secret data should have key "license"
	LicenseRef *SecretKeyRef `json:"licenseRef,omitempty"`

	// When InitialValidationForVolume is enabled the mounted Redpanda data folder
	// will be checked if:
	// - it is dir
	// - it has XFS file system
	// - it can create test file and delete it
	InitialValidationForVolume *bool `json:"initialValidationForVolume,omitempty"`

	// The name of the ServiceAccount to be used by the Redpanda pods
	ServiceAccount *string `json:"serviceAccount,omitempty"`

	// NodePools runs a StatefulSet for every NodePool. All these produce pods,
	// that join the cluster, but are steered independently (especially replica
	// count).
	NodePools []NodePoolSpec `json:"nodePools,omitempty"`
}

// ClusterConfiguration holds values (or references to values) that should be used
// to configure the cluster. Where the cluster schema defines a non-string type for a
// given key, the corresponding values here should be string-encoded (according to yaml
// rules)
type ClusterConfiguration map[string]ClusterConfigValue

// YAMLRepresentation holds a serialised form of a concrete value. We need this for
// a couple of reasons: firstly, "stringifying" numbers avoids loss of accuracy and
// rendering issues where intermediate values are represented as f64 values by
// external tooling. Secondly, the initial configuration of a bootstrap file has
// no running cluster - and therefore no online schema - available. Instead we use
// representations that can be inserted verbatim into a YAML document.
// Ideally, these will be JSON-encoded into a single line representation. They are
// decoded using YAML deserialisation (which has a little more flexibility around
// the representation of unambiguous string values).
type YAMLRepresentation string

// ClusterConfigValue represents a value of arbitrary type T. Values are string-encoded according to
// YAML rules in order to preserve numerical fidelity.
// Because these values must be embedded in a `.bootstrap.yaml` file - during the processing of
// which, the AdminAPI's schema is unavailable - we endeavour to use yaml-compatible representations
// throughout. The octet sequence of a representation will be inserted into a bootstrap template
// verbatim.
// TODO: this type should be lifted to a shared module rather than duplicated in the `redpanda` CRD definition.
// TODO: add CEL validation here.
type ClusterConfigValue struct {
	// If the value is directly known, its YAML-compatible representation can be embedded here.
	// Use the string representation of a serialised value in order to preserve accuracy.
	// Prefer JSON-encoding for values that have multi-line representations in YAML.
	// Example:
	// The string "foo" should be the five octets "\"foo\""
	// A true value should be the four octets "true".
	// The number -123456 should be a seven-octet sequence, "-123456".
	Repr *YAMLRepresentation `json:"repr,omitempty"`
	// If the value is supplied by a kubernetes object reference, coordinates are embedded here.
	// For target values, the string value fetched from the source will be treated as
	// a raw string (and appropriately quoted for use in the bootstrap file) unless `useRawValue` is set.
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Should the value be contained in a k8s secret rather than configmap, we can refer
	// to it here.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// Deprecated: replaced by "externalSecretRefSelector"; this field should be *REMOVED* ASAP since it's barely
	// made it into service.
	ExternalSecretRef *string `json:"externalSecretRef,omitempty"`
	// If the value is supplied by an external source, coordinates are embedded here.
	// Note: we interpret all fetched external secrets as raw string values by default
	// and yam-encode them prior to embedding. To disable that behaviour, set `useRawValue`.
	ExternalSecretRefSelector *ExternalSecretKeySelector `json:"externalSecretRefSelector,omitempty"`
	// Any referenced value (from kubernetes or external lookup) is typically considered to be a raw string;
	// by default it'll be quoted as a string after its lookup is resolved. To skip that behaviour,
	// and to consider the external value verbatim (ie, if it's already in an appropriate serialized form
	// for use in the bootstrap configuration), set useRawValue to true.
	// In particular, this value should be set to `true` if your external source contains a numeric value.
	UseRawValue bool `json:"useRawValue,omitempty"`
}

// ExternalSecretKeySelector selects a key of an external Secret.
// +structType=atomic
type ExternalSecretKeySelector struct {
	Name string `json:"name"`
	// Specify whether the Secret or its key must be defined
	// +optional
	Optional *bool `json:"optional,omitempty"`
}

// NodePoolSpec defines a NodePool. NodePools have their own:
// NodeSelector, so they can be scheduled on specific cloud provider Node Pools.
// Storage, as different NodePools may have different disk shapes.
// Replicas, so they can be controlled independently
// Resources, because this is tied strongly to the actual machine shape backing the NodePool.
type NodePoolSpec struct {
	// Name of the NodePool. Must be unique, and must not be "default".
	// +kubebuilder:validation:MinLength=3
	// +required
	Name string `json:"name"`
	// Replicas determine how big the node pool will be.
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`
	// If specified, Redpanda Pod tolerations
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// If specified, Redpanda Pod node selectors. For reference please visit
	// https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Storage spec for cluster
	// +required
	Storage StorageSpec `json:"storage"`
	// Resources used by redpanda process running in container. Beware that
	// there are multiple containers running in the redpanda pod and these can
	// be enabled/disabled and configured from the `sidecars` field. These
	// containers have separate resources settings and the amount of resources
	// assigned to these containers will be required on the cluster on top of
	// the resources defined here
	// +required
	Resources RedpandaResourceRequirements `json:"resources"`
	// Cache directory that will be mounted for Cloud Storage.
	// +required
	CloudCacheStorage StorageSpec `json:"cloudCacheStorage"`
	// Additional command line arguments that we pass to the redpanda binary
	// These are applied last and will override any other command line arguments that may be defined,
	// including the ones added when setting `DeveloperMode` to `true`.
	AdditionalCommandlineArguments map[string]string `json:"additionalCommandlineArguments,omitempty"`

	// HostIndexOffset is an additional offset on top of the host index - which
	// is practically the pod ordinal.
	// This makes it possible to have PrivateLink separate port ranges per NodePool.
	// +optional
	HostIndexOffset int `json:"hostIndexOffset"`
}

// RestartConfig contains strategies to configure how the cluster behaves when restarting, because of upgrades
// or other lifecycle events.
type RestartConfig struct {
	// DisableMaintenanceModeHooks deactivates the preStop and postStart hooks that force nodes to enter maintenance mode when stopping and exit maintenance mode when up again
	DisableMaintenanceModeHooks *bool `json:"disableMaintenanceModeHooks,omitempty"`

	// UnderReplicatedPartitionThreshold controls when rolling update will continue with
	// restarts. The procedure can be described as follows:
	//
	// 1. Rolling update checks if Pod specification needs to be replaced and deletes it
	// 2. Deleted Redpanda Pod is put into maintenance mode (postStart hook will disable
	//    maintenance mode when new Pod starts)
	// 3. Rolling update waits for Pod to be in Ready state
	// 4. Rolling update checks if cluster is in healthy state
	// 5. Rolling update checks if restarted Redpanda Pod admin API Ready endpoint returns HTTP 200 response
	// 6. Using UnderReplicatedPartitionThreshold each under replicated partition metric is compared with the threshold
	// 7. Rolling update moves to the next Redpanda pod
	//
	// The metric `vectorized_cluster_partition_under_replicated_replicas` is used in the comparison
	//
	// Mentioned metrics has the following help description:
	// `vectorized_cluster_partition_under_replicated_replicas` Number of under replicated replicas
	//
	// By default, the UnderReplicatedPartitionThreshold will be 0, which means all partitions needs to catch up without any lag.
	UnderReplicatedPartitionThreshold int `json:"underReplicatedPartitionThreshold,omitempty"`
}

// PDBConfig specifies how the PodDisruptionBudget should be created for the
// redpanda cluster. PDB will be created for the deployed cluster if Enabled is
// set to true.
type PDBConfig struct {
	// Enabled specifies whether PDB should be generated for the cluster. It defaults to true
	Enabled bool `json:"enabled,omitempty"`
	// An eviction is allowed if at least "minAvailable" pods selected by
	// "selector" will still be available after the eviction, i.e. even in the
	// absence of the evicted pod.  So for example you can prevent all voluntary
	// evictions by specifying "100%". This is a mutually exclusive setting with "maxUnavailable".
	// you can read more in https://kubernetes.io/docs/tasks/run-application/configure-pdb/
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`
	// An eviction is allowed if at most "maxUnavailable" pods selected by
	// "selector" are unavailable after the eviction, i.e. even in absence of
	// the evicted pod. For example, one can prevent all voluntary evictions
	// by specifying 0. This is a mutually exclusive setting with "minAvailable".
	// This property defaults to 1.
	// you can read more in https://kubernetes.io/docs/tasks/run-application/configure-pdb/
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// Sidecars is definition of sidecars running alongside redpanda process
type Sidecars struct {
	// RpkStatus is sidecar running rpk status collecting status information
	// from the running node
	RpkStatus *Sidecar `json:"rpkStatus,omitempty"`
}

// Sidecar is a container running alongside redpanda, there's couple of them
// added by default via defaulting webhook
type Sidecar struct {
	// Enabled if false, the sidecar won't be added to the pod running redpanda node
	Enabled bool `json:"enabled,omitempty"`
	// Resources are resource requirements and limits for the container running
	// this sidecar. For the default sidecars this is defaulted
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// Superuser has full access to the Redpanda cluster
type Superuser struct {
	Username string `json:"username"`
}

// CloudStorageConfig configures the Data Archiving feature in Redpanda
// https://vectorized.io/docs/data-archiving
type CloudStorageConfig struct {
	// Enables data archiving feature
	Enabled bool `json:"enabled"`
	// Cloud storage access key
	AccessKey string `json:"accessKey,omitempty"`
	// Reference to (Kubernetes) Secret containing the cloud storage secret key.
	// SecretKeyRef must contain the name and namespace of the Secret.
	// The Secret must contain a data entry of the form:
	// data[<SecretKeyRef.Name>] = <secret key>
	SecretKeyRef corev1.ObjectReference `json:"secretKeyRef,omitempty"`
	// Cloud storage region
	Region string `json:"region,omitempty"`
	// Cloud storage bucket
	Bucket string `json:"bucket,omitempty"`
	// Reconciliation period (default - 10s)
	ReconcilicationIntervalMs int `json:"reconciliationIntervalMs,omitempty"`
	// Number of simultaneous uploads per shard (default - 20)
	MaxConnections int `json:"maxConnections,omitempty"`
	// Disable TLS (can be used in tests)
	DisableTLS bool `json:"disableTLS,omitempty"`
	// Path to certificate that should be used to validate server certificate
	Trustfile string `json:"trustfile,omitempty"`
	// API endpoint for data storage
	APIEndpoint string `json:"apiEndpoint,omitempty"`
	// Used to override TLS port (443)
	APIEndpointPort int `json:"apiEndpointPort,omitempty"`
	// Cache directory that will be mounted for Redpanda
	CacheStorage *StorageSpec `json:"cacheStorage,omitempty"`
	// Determines how to load credentials for archival storage. Supported values
	// are config_file (default), aws_instance_metadata, sts, gcp_instance_metadata
	// (see the cloud_storage_credentials_source property at
	// https://docs.redpanda.com/docs/reference/cluster-properties/).
	// When using config_file then accessKey and secretKeyRef are mandatory.
	CredentialsSource CredentialsSource `json:"credentialsSource,omitempty"`
}

// CredentialsSource represents a mechanism for loading credentials for archival storage
type CredentialsSource string

const (
	// credentialsSourceConfigFile is the default options for credentials source
	CredentialsSourceConfigFile CredentialsSource = "config_file"
)

func (c CredentialsSource) IsDefault() bool {
	return c == "" || c == CredentialsSourceConfigFile
}

// StorageSpec defines the storage specification of the Cluster
type StorageSpec struct {
	// Storage capacity requested
	Capacity resource.Quantity `json:"capacity,omitempty"`
	// Storage class name - https://kubernetes.io/docs/concepts/storage/storage-classes/
	StorageClassName string `json:"storageClassName,omitempty"`
}

// ExternalConnectivityConfig adds listener that can be reached outside
// of a kubernetes cluster. The Service type NodePort will be used
// to create unique ports on each Kubernetes nodes. Those nodes
// need to be reachable from the client perspective. Setting up
// any additional resources in cloud or premise is the responsibility
// of the Redpanda operator user e.g. allow to reach the nodes by
// creating new rule in AWS security group.
// Inside the container the Configuration.KafkaAPI.Port + 1 will be
// used as a external listener. This port is tight to the autogenerated
// host port. The collision between Kafka external, Kafka internal,
// Admin, Pandaproxy, Schema Registry and RPC port is checked in the webhook.
// An optional endpointTemplate can be used to configure advertised addresses
// for Kafka API and Pandaproxy, while it is disallowed for other listeners.
type ExternalConnectivityConfig struct {
	// Enabled enables the external connectivity feature
	Enabled bool `json:"enabled,omitempty"`
	// Subdomain can be used to change the behavior of an advertised
	// KafkaAPI. Each broker advertises Kafka API as follows
	// ENDPOINT.SUBDOMAIN:EXTERNAL_KAFKA_API_PORT.
	// If Subdomain is empty then each broker advertises Kafka
	// API as PUBLIC_NODE_IP:EXTERNAL_KAFKA_API_PORT.
	// If TLS is enabled then this subdomain will be requested
	// as a subject alternative name.
	Subdomain string `json:"subdomain,omitempty"`
	// EndpointTemplate is a Golang template string that allows customizing each
	// broker advertised address.
	// Redpanda uses the format BROKER_ID.SUBDOMAIN:EXTERNAL_KAFKA_API_PORT by
	// default for advertised addresses. When an EndpointTemplate is
	// provided, then the BROKER_ID part is replaced with the endpoint
	// computed from the template.
	// The following variables are available to the template:
	// - Index: the Redpanda broker progressive number
	// - HostIP: the ip address of the Node, as reported in pod status
	//
	// Common template functions from Sprig (http://masterminds.github.io/sprig/)
	// are also available. The set of available functions is limited to hermetic
	// functions because template application needs to be deterministic.
	EndpointTemplate string `json:"endpointTemplate,omitempty"`
	// PortTemplate is a Golang template string that allows customizing each
	// broker advertised port, e.g. "32092 + {{ .Index }}".
	// If not provided, the port will be the same as the internal one defined
	// in the listener/endpoint's configuration such as KafkaAPI.Port.
	PortTemplate string `json:"portTemplate,omitempty"`
	// ExcludeFromService is an indication that tells the controller not to expose the node port
	// in a Kubernetes service. This is useful when the port is exposed by other means.
	ExcludeFromService bool `json:"excludeFromService,omitempty"`
	// The preferred address type to be assigned to the external
	// advertised addresses. The valid types are ExternalDNS,
	// ExternalIP, InternalDNS, InternalIP, and Hostname.
	// When the address of the preferred type is not found the advertised
	// addresses remains empty. The default preferred address type is
	// ExternalIP. This option only applies when Subdomain is empty.
	PreferredAddressType string `json:"preferredAddressType,omitempty"`
	// Configures a load balancer for bootstrapping
	Bootstrap *LoadBalancerConfig `json:"bootstrapLoadBalancer,omitempty"`
}

// PandaproxyExternalConnectivityConfig allows to customize pandaproxy specific
// external connectivity.
type PandaproxyExternalConnectivityConfig struct {
	ExternalConnectivityConfig `json:",inline"`

	// Configures a ingress resource
	Ingress *IngressConfig `json:"ingress,omitempty"`
}

// LoadBalancerConfig defines the load balancer specification
type LoadBalancerConfig struct {
	// If specified, sets the load balancer service annotations.
	// Example usage includes configuring the load balancer to
	// be an internal one through provider-specific annotations.
	Annotations map[string]string `json:"annotations,omitempty"`
	// The port used to communicate to the load balancer.
	Port int `json:"port,omitempty"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// If set, this represents the .metadata.generation that was observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Replicas show how many nodes have been created for the cluster
	// +optional
	Replicas int32 `json:"replicas"`
	// ReadyReplicas is the number of Pods belonging to the cluster that have a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// CurrentReplicas is the number of Pods that the controller currently wants to run for the cluster.
	// +optional
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`
	// Nodes of the provisioned redpanda nodes
	// +optional
	Nodes NodesList `json:"nodes,omitempty"`
	// Indicates cluster is upgrading.
	// +optional
	// Deprecated: replaced by "restarting"
	DeprecatedUpgrading bool `json:"upgrading"`
	// Indicates that a cluster is restarting due to an upgrade or a different reason
	// +optional
	Restarting bool `json:"restarting"`
	// Indicates that a node is currently being decommissioned from the cluster and provides its ordinal number
	// +optional
	DecommissioningNode *int32 `json:"decommissioningNode,omitempty"`
	// Current version of the cluster.
	// +optional
	Version string `json:"version"`
	// Current state of the cluster.
	// +optional
	Conditions []ClusterCondition `json:"conditions,omitempty"`
	// Every NodePool has its own status.
	NodePools map[string]NodePoolStatus `json:"nodePools,omitempty"`
}

// ClusterCondition contains details for the current conditions of the cluster
type ClusterCondition struct {
	// Type is the type of the condition
	Type ClusterConditionType `json:"type"`
	// Status is the status of the condition
	Status corev1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Unique, one-word, CamelCase reason for the condition's last transition
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human-readable message indicating details about last transition
	// +optional
	Message string `json:"message,omitempty"`
}

// ClusterConditionType is a valid value for ClusterCondition.Type
// +kubebuilder:validation:Enum=ClusterConfigured;OperatorQuiescent
type ClusterConditionType string

// These are valid conditions of the cluster.
const (
	// ClusterConfiguredConditionType indicates whether the Redpanda cluster configuration is in sync with the desired one
	ClusterConfiguredConditionType ClusterConditionType = "ClusterConfigured"
	// OperatorQuiescentConditionType indicates that the operator has no outstanding work to do, based on the observedGeneration.
	OperatorQuiescentConditionType ClusterConditionType = "OperatorQuiescent"
)

// GetCondition return the condition of the given type
func (s *ClusterStatus) GetCondition(
	cType ClusterConditionType,
) *ClusterCondition {
	for i := range s.Conditions {
		if s.Conditions[i].Type == cType {
			return &s.Conditions[i]
		}
	}
	return nil
}

// GetConditionStatus is a shortcut to directly get the status of a given condition
func (s *ClusterStatus) GetConditionStatus(
	cType ClusterConditionType,
) corev1.ConditionStatus {
	cond := s.GetCondition(cType)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

// SetCondition allows setting a condition of a given type.
// In case of change in any value other than the lastTransitionTime, the lastTransitionTime
// field will be set to the current timestamp. The return value indicates if a change has happened.
func (s *ClusterStatus) SetCondition(
	cType ClusterConditionType,
	status corev1.ConditionStatus,
	reason, message string,
) bool {
	return s.SetConditionUsingClock(cType, status, reason, message, time.Now)
}

// SetConditionUsingClock is similar to SetCondition, but allows to specify the function to get the system clock from.
func (s *ClusterStatus) SetConditionUsingClock(
	cType ClusterConditionType,
	status corev1.ConditionStatus,
	reason, message string,
	clock func() time.Time,
) bool {
	update := func(c *ClusterCondition) bool {
		changed := c.Status != status || c.Reason != reason || c.Message != message
		if changed {
			c.LastTransitionTime = metav1.NewTime(clock())
		}
		c.Type = cType
		c.Status = status
		c.Reason = reason
		c.Message = message
		return changed
	}
	// Try updating existing condition
	for i := range s.Conditions {
		if s.Conditions[i].Type == cType {
			return update(&s.Conditions[i])
		}
	}
	// Add a new one if missing
	newCond := ClusterCondition{}
	update(&newCond)
	s.Conditions = append(s.Conditions, newCond)
	return true
}

// These are valid reasons for ClusterConfigured
const (
	// ClusterConfiguredReasonUpdating indicates that the desired configuration is being applied to the running cluster
	ClusterConfiguredReasonUpdating = "Updating"
	// ClusterConfiguredReasonDrift indicates that the cluster drifted from the desired configuration and needs to be synced
	ClusterConfiguredReasonDrift = "Drift"
	// ClusterConfiguredReasonError signals an error when applying the configuration to the Redpanda cluster
	ClusterConfiguredReasonError = "Error"
)

// NodesList shows where client of Cluster custom resource can reach
// various listeners of Redpanda cluster
type NodesList struct {
	Internal           []string              `json:"internal,omitempty"`
	External           []string              `json:"external,omitempty"`
	ExternalBootstrap  *LoadBalancerStatus   `json:"externalBootstrap,omitempty"`
	ExternalAdmin      []string              `json:"externalAdmin,omitempty"`
	ExternalPandaproxy []string              `json:"externalPandaproxy,omitempty"`
	PandaproxyIngress  *string               `json:"pandaproxyIngress,omitempty"`
	SchemaRegistry     *SchemaRegistryStatus `json:"schemaRegistry,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

// RedpandaConfig is the definition of the main configuration
type RedpandaConfig struct {
	RPCServer      SocketAddress      `json:"rpcServer,omitempty"`
	KafkaAPI       []KafkaAPI         `json:"kafkaApi,omitempty"`
	AdminAPI       []AdminAPI         `json:"adminApi,omitempty"`
	PandaproxyAPI  []PandaproxyAPI    `json:"pandaproxyApi,omitempty"`
	SchemaRegistry *SchemaRegistryAPI `json:"schemaRegistry,omitempty"`

	// SchemaRegistryAPI intends to replace SchemaRegistry field.
	// It is a list of all Schema Registry listeners.
	SchemaRegistryAPI []SchemaRegistryAPI `json:"schemaRegistryApi,omitempty"`

	DeveloperMode bool `json:"developerMode,omitempty"`
	// Number of partitions in the internal group membership topic
	GroupTopicPartitions int `json:"groupTopicPartitions,omitempty"`
	// Enable auto-creation of topics. Reference https://kafka.apache.org/documentation/#brokerconfigs_auto.create.topics.enable
	AutoCreateTopics bool `json:"autoCreateTopics,omitempty"`
	// Additional command line arguments that we pass to the redpanda binary
	// These are applied last and will override any other command line arguments that may be defined,
	// including the ones added when setting `DeveloperMode` to `true`.
	AdditionalCommandlineArguments map[string]string `json:"additionalCommandlineArguments,omitempty"`
}

// AdminAPI configures listener for the Redpanda Admin API
type AdminAPI struct {
	Port int `json:"port,omitempty"`
	// External enables user to expose Redpanda
	// admin API outside of a Kubernetes cluster. For more
	// information please go to ExternalConnectivityConfig
	External ExternalConnectivityConfig `json:"external,omitempty"`
	// Configuration of TLS for Admin API
	TLS AdminAPITLS `json:"tls,omitempty"`
}

// KafkaAPI configures listener for the Kafka API
type KafkaAPI struct {
	Port int `json:"port,omitempty"`
	// Name is the name of this Kafka listener. It will be set to the default
	// value if not provided.
	//   "kafka" for an internal listener
	//   "kafka-external" for an external listener.
	Name string `json:"name,omitempty"`
	// External enables user to expose Redpanda
	// nodes outside of a Kubernetes cluster. For more
	// information please go to ExternalConnectivityConfig
	External ExternalConnectivityConfig `json:"external,omitempty"`
	// Configuration of TLS for Kafka API
	TLS KafkaAPITLS `json:"tls,omitempty"`
	// AuthenticationMethod can enable authentication method per Kafka
	// listener. Available options are: none, sasl, mtls_identity.
	// https://docs.redpanda.com/docs/security/authentication/
	AuthenticationMethod string `json:"authenticationMethod,omitempty"`
}

// PandaproxyAPI configures listener for the Pandaproxy API
type PandaproxyAPI struct {
	Port int `json:"port,omitempty"`
	// The name of Panda Proxy API endpoint.
	Name string `json:"name,omitempty"`
	// External enables user to expose Redpanda
	// nodes outside of a Kubernetes cluster. For more
	// information please go to ExternalConnectivityConfig
	External PandaproxyExternalConnectivityConfig `json:"external,omitempty"`
	// Configuration of TLS for Pandaproxy API
	TLS PandaproxyAPITLS `json:"tls,omitempty"`
	// AuthenticationMethod can enable authentication method per pandaproxy
	// listener. Available options are: none, http_basic.
	AuthenticationMethod string `json:"authenticationMethod,omitempty"`
}

// SchemaRegistryAPI configures the schema registry API
type SchemaRegistryAPI struct {
	// Port will set the schema registry listener port in Redpanda
	// configuration.
	// If not set the default will be 8081
	Port int `json:"port"`
	// The name of Schema Registry API endpoint.
	Name string `json:"name,omitempty"`
	// External enables user to expose Redpanda
	// nodes outside of a Kubernetes cluster. For more
	// information please go to ExternalConnectivityConfig
	External *SchemaRegistryExternalConnectivityConfig `json:"external,omitempty"`
	// TLS is the configuration for schema registry
	TLS *SchemaRegistryAPITLS `json:"tls,omitempty"`
	// AuthenticationMethod can enable authentication method per schema registry
	// listener. Available options are: none, http_basic.
	AuthenticationMethod string `json:"authenticationMethod,omitempty"`
}

// SchemaRegistryExternalConnectivityConfig defines the external connectivity
// options for schema registry.
type SchemaRegistryExternalConnectivityConfig struct {
	ExternalConnectivityConfig `json:",inline"`
	// Indicates that the node port for the service needs not to be generated.
	StaticNodePort bool `json:"staticNodePort,omitempty"`
	// Indicates the global endpoint that (together with subdomain), should be
	// advertised for schema registry.
	Endpoint string `json:"endpoint,omitempty"`
}

// SchemaRegistryStatus reports addresses where schema registry
// can be reached
type SchemaRegistryStatus struct {
	Internal string `json:"internal,omitempty"`
	// External address should be registered in DNS provider using
	// all public IP of a nodes that Redpanda is scheduled on.
	//
	// The External is empty when subdomain is not provided.
	External string `json:"external,omitempty"`
	// ExternalNodeIPs is only filled when the Schema Registry
	// external connectivity feature flag is enabled, but the subdomain is
	// empty. This gives user ability to register all addresses individually
	// in DNS provider of choice.
	ExternalNodeIPs []string `json:"externalNodeIPs,omitempty"`
}

// LoadBalancerStatus reports the load balancer status as generated
// by the load balancer core service
type LoadBalancerStatus struct {
	corev1.LoadBalancerStatus `json:""`
}

// NodePoolStatus describes the status of the NodePool.
type NodePoolStatus struct {
	CurrentReplicas int32 `json:"currentReplicas"`
	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas"`
	// Indicates that a nodePool's pods are restarting.
	// +optional
	Restarting bool `json:"restarting"`
}

// KafkaAPITLS configures TLS for redpanda Kafka API
//
// If Enabled is set to true, one-way TLS verification is enabled.
// In that case, a key pair ('tls.crt', 'tls.key') and CA certificate 'ca.crt'
// are generated and stored in a Secret with the same name and namespace as the
// Redpanda cluster. 'ca.crt', must be used by a client as a trustore when
// communicating with Redpanda.
//
// If RequireClientAuth is set to true, two-way TLS verification is enabled.
// In that case, a node and three client certificates are created.
// The node certificate is used by redpanda nodes.
//
// The three client certificates are the following: 1. operator client
// certificate is for internal use of this kubernetes operator 2. admin client
// certificate is meant to be used by your internal infrastructure, other than
// operator. It's possible that you might not need this client certificate in
// your setup. The client certificate can be retrieved from the Secret named
// '<redpanda-cluster-name>-admin-client'. 3. user client certificate is
// available for Redpanda users to call KafkaAPI. The client certificate can be
// retrieved from the Secret named '<redpanda-cluster-name>-user-client'.
//
// All TLS secrets are stored in the same namespace as the Redpanda cluster.
//
// Additionally all mentioned certificates beside PEM version will have JKS
// and PKCS#12 certificate. Both stores are protected with the password that
// is the same as the name of the Cluster custom resource.
type KafkaAPITLS struct {
	Enabled bool `json:"enabled,omitempty"`
	// References cert-manager Issuer or ClusterIssuer. When provided, this
	// issuer will be used to issue node certificates.
	// Typically you want to provide the issuer when a generated self-signed one
	// is not enough and you need to have a verifiable chain with a proper CA
	// certificate.
	IssuerRef *cmmetav1.ObjectReference `json:"issuerRef,omitempty"`
	// If provided, operator uses certificate in this secret instead of
	// issuing its own node certificate. The secret is expected to provide
	// the following keys: 'ca.crt', 'tls.key' and 'tls.crt'
	// If NodeSecretRef points to secret in different namespace, operator will
	// duplicate the secret to the same namespace as redpanda CRD to be able to
	// mount it to the nodes
	NodeSecretRef *corev1.ObjectReference `json:"nodeSecretRef,omitempty"`
	// If ClientCACertRef points to a secret containing the trusted CA certificates.
	// If provided and RequireClientAuth is true, the operator uses the certificate
	// in this secret instead of issuing client certificates. The secret is expected to provide
	// the following keys: 'ca.crt'.
	ClientCACertRef *corev1.TypedLocalObjectReference `json:"clientCACertRef,omitempty"`
	// Enables two-way verification on the server side. If enabled, all Kafka
	// API clients are required to have a valid client certificate.
	RequireClientAuth bool `json:"requireClientAuth,omitempty"`
}

// AdminAPITLS configures TLS for Redpanda Admin API
//
// If Enabled is set to true, one-way TLS verification is enabled.
// In that case, a key pair ('tls.crt', 'tls.key') and CA certificate 'ca.crt'
// are generated and stored in a Secret named '<redpanda-cluster-name>-admin-api-node
// and namespace as the Redpanda cluster. 'ca.crt' must be used by a client as a
// truststore when communicating with Redpanda.
//
// If RequireClientAuth is set to true, two-way TLS verification is enabled.
// In that case, a client certificate is generated, which can be retrieved from
// the Secret named '<redpanda-cluster-name>-admin-api-client'.
//
// All TLS secrets are stored in the same namespace as the Redpanda cluster.
//
// Additionally all mentioned certificates beside PEM version will have JKS
// and PKCS#12 certificate. Both stores are protected with the password that
// is the same as the name of the Cluster custom resource.
type AdminAPITLS struct {
	Enabled bool `json:"enabled,omitempty"`
	// References cert-manager Issuer or ClusterIssuer. When provided, this
	// issuer will be used to issue node certificates.
	// Typically you want to provide the issuer when a generated self-signed one
	// is not enough and you need to have a verifiable chain with a proper CA
	// certificate.
	IssuerRef *cmmetav1.ObjectReference `json:"issuerRef,omitempty"`
	// If provided, operator uses certificate in this secret instead of
	// issuing its own node certificate. The secret is expected to provide
	// the following keys: 'ca.crt', 'tls.key' and 'tls.crt'
	// If NodeSecretRef points to secret in different namespace, operator will
	// duplicate the secret to the same namespace as redpanda CRD to be able to
	// mount it to the nodes.
	NodeSecretRef *corev1.ObjectReference `json:"nodeSecretRef,omitempty"`
	// If ClientCACertRef points to a secret containing the trusted CA certificates.
	// If provided and RequireClientAuth is true, the operator uses the certificate
	// in this secret instead of issuing client certificates. The secret is expected to provide
	// the following keys: 'ca.crt'.
	ClientCACertRef   *corev1.TypedLocalObjectReference `json:"clientCACertRef,omitempty"`
	RequireClientAuth bool                              `json:"requireClientAuth,omitempty"`
}

// PandaproxyAPITLS configures the TLS of the Pandaproxy API
//
// If Enabled is set to true, one-way TLS verification is enabled.
// In that case, a key pair ('tls.crt', 'tls.key') and CA certificate 'ca.crt'
// are generated and stored in a Secret named '<redpanda-cluster-name>-proxy-api-node'
// and namespace as the Redpanda cluster. 'ca.crt' must be used by a client as a
// truststore when communicating with Redpanda.
//
// If RequireClientAuth is set to true, two-way TLS verification is enabled.
// If ClientCACertRef is provided, the operator will configure the Pandaproxy to
// use the CA cert it contains.
// Otherwise, a client certificate is generated, which can be retrieved from
// the Secret named '<redpanda-cluster-name>-proxy-api-client'.
//
// All TLS secrets are stored in the same namespace as the Redpanda cluster.
//
// Additionally all mentioned certificates beside PEM version will have JKS
// and PKCS#12 certificate. Both stores are protected with the password that
// is the same as the name of the Cluster custom resource.
type PandaproxyAPITLS struct {
	Enabled bool `json:"enabled,omitempty"`
	// References cert-manager Issuer or ClusterIssuer. When provided, this
	// issuer will be used to issue node certificates.
	// Typically you want to provide the issuer when a generated self-signed one
	// is not enough and you need to have a verifiable chain with a proper CA
	// certificate.
	IssuerRef *cmmetav1.ObjectReference `json:"issuerRef,omitempty"`
	// If provided, operator uses certificate in this secret instead of
	// issuing its own node certificate. The secret is expected to provide
	// the following keys: 'ca.crt', 'tls.key' and 'tls.crt'
	// If NodeSecretRef points to secret in different namespace, operator will
	// duplicate the secret to the same namespace as redpanda CRD to be able to
	// mount it to the nodes
	NodeSecretRef *corev1.ObjectReference `json:"nodeSecretRef,omitempty"`
	// If ClientCACertRef points to a secret containing the trusted CA certificates.
	// If provided and RequireClientAuth is true, the operator uses the certificate
	// in this secret instead of issuing client certificates. The secret is expected to provide
	// the following keys: 'ca.crt'.
	ClientCACertRef *corev1.TypedLocalObjectReference `json:"clientCACertRef,omitempty"`
	// Enables two-way verification on the server side. If enabled, all
	// Pandaproxy API clients are required to have a valid client certificate.
	RequireClientAuth bool `json:"requireClientAuth,omitempty"`
}

// SchemaRegistryAPITLS configures the TLS of the Pandaproxy API
//
// If Enabled is set to true, one-way TLS verification is enabled.
// In that case, a key pair ('tls.crt', 'tls.key') and CA certificate 'ca.crt'
// are generated and stored in a Secret named '<redpanda-cluster-name>-schema-registry-node'
// and namespace as the Redpanda cluster. 'ca.crt' must be used by a client as a
// truststore when communicating with Schema registry.
//
// If RequireClientAuth is set to true, two-way TLS verification is enabled.
// If ClientCACertRef is provided, the operator will configure the Schema Registry to
// use the CA cert it contains.
// Otherwise a client certificate is generated, which can be retrieved from
// the Secret named '<redpanda-cluster-name>-schema-registry-client'.
//
// All TLS secrets are stored in the same namespace as the Redpanda cluster.
//
// Additionally all mentioned certificates beside PEM version will have JKS
// and PKCS#12 certificate. Both stores are protected with the password that
// is the same as the name of the Cluster custom resource.
type SchemaRegistryAPITLS struct {
	Enabled bool `json:"enabled,omitempty"`
	// References cert-manager Issuer or ClusterIssuer. When provided, this
	// issuer will be used to issue node certificates.
	// Typically you want to provide the issuer when a generated self-signed one
	// is not enough and you need to have a verifiable chain with a proper CA
	// certificate.
	IssuerRef *cmmetav1.ObjectReference `json:"issuerRef,omitempty"`
	// If provided, operator uses certificate in this secret instead of
	// issuing its own node certificate. The secret is expected to provide
	// the following keys: 'ca.crt', 'tls.key' and 'tls.crt'
	// If NodeSecretRef points to secret in different namespace, operator will
	// duplicate the secret to the same namespace as redpanda CRD to be able to
	// mount it to the nodes
	NodeSecretRef *corev1.ObjectReference `json:"nodeSecretRef,omitempty"`
	// If ClientCACertRef points to a secret containing the trusted CA certificates.
	// If provided and RequireClientAuth is true, the operator uses the certificate
	// in this secret instead of issuing client certificates. The secret is expected to provide
	// the following keys: 'ca.crt'.
	ClientCACertRef *corev1.TypedLocalObjectReference `json:"clientCACertRef,omitempty"`
	// Enables two-way verification on the server side. If enabled, all SchemaRegistry
	// clients are required to have a valid client certificate.
	RequireClientAuth bool `json:"requireClientAuth,omitempty"`
}

// SocketAddress provide the way to configure the port
type SocketAddress struct {
	Port int `json:"port,omitempty"`
}

const (
	// MinimumMemoryPerCore the minimum amount of memory needed per core
	MinimumMemoryPerCore = 2 * gb
	// RedpandaMemoryAllocationRatio reserves 10% for the OS
	RedpandaMemoryAllocationRatio = 0.9
)

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}

// FullImageName returns image name including version
func (r *Cluster) FullImageName() string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s", r.Spec.Image, r.Spec.Version)
}

// KafkaAPIExternalListeners returns all the Kafka external listeners.
func (r *Cluster) KafkaAPIExternalListeners() []KafkaAPI {
	if r == nil {
		return nil
	}
	res := []KafkaAPI{}
	for i := range r.Spec.Configuration.KafkaAPI {
		el := r.Spec.Configuration.KafkaAPI[i]
		if el.Name == "" {
			el.Name = ExternalListenerName
		}
		if el.External.Enabled {
			res = append(res, el)
		}
	}
	return res
}

// ExternalListener returns the first external kafka listener.
// Deprecated: Prefer FirstExternalListener or KafkaAPIExternalListeners
func (r *Cluster) ExternalListener() *KafkaAPI {
	return r.FirstExternalListener()
}

// FirstExternalListener returns an external listener if found in configuration. Returns
// nil if no external listener is configured. Right now we support only one
// external listener which is enforced by webhook
func (r *Cluster) FirstExternalListener() *KafkaAPI {
	if r == nil {
		return nil
	}
	for i := range r.Spec.Configuration.KafkaAPI {
		el := &r.Spec.Configuration.KafkaAPI[i]
		if el.External.Enabled {
			return el
		}
	}
	return nil
}

// InternalListener returns internal listener.
func (r *Cluster) InternalListener() *KafkaAPI {
	if r == nil {
		return nil
	}
	for i := range r.Spec.Configuration.KafkaAPI {
		el := &r.Spec.Configuration.KafkaAPI[i]
		if !el.External.Enabled {
			return el
		}
	}
	return nil
}

// ListenerWithName contains listener definition with name. For now, we have
// only two names, internal and external
type ListenerWithName struct {
	Name string
	KafkaAPI
}

// KafkaTLSListeners returns kafka listeners that have tls enabled
func (r *Cluster) KafkaTLSListeners() []ListenerWithName {
	if r == nil {
		return nil
	}
	res := []ListenerWithName{}
	for i := range r.Spec.Configuration.KafkaAPI {
		el := &r.Spec.Configuration.KafkaAPI[i]
		if el.TLS.Enabled {
			name := InternalListenerName
			if el.External.Enabled {
				name = el.Name
				if name == "" {
					name = ExternalListenerName
				}
			}
			res = append(res, ListenerWithName{
				KafkaAPI: r.Spec.Configuration.KafkaAPI[i],
				Name:     name,
			})
		}
	}
	return res
}

// KafkaListener returns a KafkaAPI listener
// It returns internal listener if available
func (r *Cluster) KafkaListener() *KafkaAPI {
	if r == nil {
		return nil
	}
	if l := r.InternalListener(); l != nil {
		return l
	}
	return r.ExternalListener()
}

// AdminAPIInternal returns internal admin listener
func (r *Cluster) AdminAPIInternal() *AdminAPI {
	if r == nil {
		return nil
	}
	for _, el := range r.Spec.Configuration.AdminAPI {
		if !el.External.Enabled {
			return &el
		}
	}
	return nil
}

// AdminAPIExternal returns external admin listener
func (r *Cluster) AdminAPIExternal() *AdminAPI {
	if r == nil {
		return nil
	}
	for _, el := range r.Spec.Configuration.AdminAPI {
		if el.External.Enabled {
			return &el
		}
	}
	return nil
}

// AdminAPITLS returns admin api listener that has tls enabled or nil if there's
// none
func (r *Cluster) AdminAPITLS() *AdminAPI {
	if r == nil {
		return nil
	}
	for i, el := range r.Spec.Configuration.AdminAPI {
		if el.TLS.Enabled {
			return &r.Spec.Configuration.AdminAPI[i]
		}
	}
	return nil
}

// AdminAPIListener returns a AdminAPI listener
// It returns internal listener if available
func (r *Cluster) AdminAPIListener() *AdminAPI {
	if r == nil {
		return nil
	}
	if l := r.AdminAPIInternal(); l != nil {
		return l
	}
	return r.AdminAPIExternal()
}

// AdminAPIURLs returns a list of AdminAPI URLs.
func (r *Cluster) AdminAPIURLs() []string {
	if r == nil {
		return nil
	}
	aa := r.AdminAPIListener()
	if aa == nil {
		return []string{}
	}

	var (
		hosts = []string{}
		urls  = []string{}
	)
	if !aa.External.Enabled {
		for _, i := range r.Status.Nodes.Internal {
			port := fmt.Sprintf("%d", aa.Port)
			hosts = append(hosts, net.JoinHostPort(i, port))
		}
	} else {
		hosts = r.Status.Nodes.ExternalAdmin
	}

	for _, host := range hosts {
		u := url.URL{Scheme: aa.GetHTTPScheme(), Host: host}
		urls = append(urls, u.String())
	}
	return urls
}

// PandaproxyAPIInternal returns internal pandaproxy listener
func (r *Cluster) PandaproxyAPIInternal() *PandaproxyAPI {
	if r == nil {
		return nil
	}
	proxies := r.Spec.Configuration.PandaproxyAPI
	for i := range proxies {
		if !proxies[i].External.Enabled {
			return &proxies[i]
		}
	}
	return nil
}

// PandaproxyAPIExternalListeners returns all the external pandaproxy listeners
func (r *Cluster) PandaproxyAPIExternalListeners() []PandaproxyAPI {
	if r == nil {
		return nil
	}
	res := []PandaproxyAPI{}
	proxies := r.Spec.Configuration.PandaproxyAPI
	for i := range proxies {
		if proxies[i].External.Enabled {
			if proxies[i].Name == "" {
				proxies[i].Name = PandaproxyExternalListenerName
			}
			res = append(res, proxies[i])
		}
	}
	return res
}

// PandaproxyAPIExternal returns the first external pandaproxy listener
// Deprecated: Prefer FirstPandaproxyAPIExternal or PandaproxyAPIExternalListeners
func (r *Cluster) PandaproxyAPIExternal() *PandaproxyAPI {
	return r.FirstPandaproxyAPIExternal()
}

// FirstPandaproxyAPIExternal returns the first external pandaproxy listener in the list.
func (r *Cluster) FirstPandaproxyAPIExternal() *PandaproxyAPI {
	if r == nil {
		return nil
	}
	proxies := r.Spec.Configuration.PandaproxyAPI
	for i := range proxies {
		if proxies[i].External.Enabled {
			return &proxies[i]
		}
	}
	return nil
}

// PandaproxyAPITLS returns the first Pandaproxy listener that has TLS enabled.
// Deprecated: Prefer PandaproxyAPITLS or PandaproxyAPITLSs
func (r *Cluster) PandaproxyAPITLS() *PandaproxyAPI {
	return r.FirstPandaproxyAPITLS()
}

// PandaproxyAPITLS returns the first Pandaproxy listener that has TLS enabled.
// It returns nil if no TLS is configured.
func (r *Cluster) FirstPandaproxyAPITLS() *PandaproxyAPI {
	if r == nil {
		return nil
	}
	proxies := r.Spec.Configuration.PandaproxyAPI
	for i := range proxies {
		if proxies[i].TLS.Enabled {
			return &proxies[i]
		}
	}
	return nil
}

// PandaproxyAPITLSs returns all Pandaproxy listeners that have TLS enabled.
func (r *Cluster) PandaproxyAPITLSs() []PandaproxyAPI {
	if r == nil {
		return nil
	}
	res := []PandaproxyAPI{}
	proxies := r.Spec.Configuration.PandaproxyAPI
	for i := range proxies {
		if proxies[i].TLS.Enabled {
			if proxies[i].Name == "" {
				proxies[i].Name = PandaproxyExternalListenerName
			}
			res = append(res, proxies[i])
		}
	}
	return res
}

// SchemaRegistryAPIURL returns a SchemaRegistry URL string.
// It returns internal URL unless externally available with TLS.
func (r *Cluster) SchemaRegistryAPIURL() string {
	if r == nil {
		return ""
	}
	// Prefer to use internal URL
	// But if it is externally available with TLS, we cannot call internal URL without TLS and the TLS certs are signed for external URL
	host := r.Status.Nodes.SchemaRegistry.Internal
	if r.IsSchemaRegistryExternallyAvailable() && r.IsSchemaRegistryTLSEnabled() {
		host = r.Status.Nodes.SchemaRegistry.External
	}
	if sr := r.Spec.Configuration.SchemaRegistry; sr != nil {
		u := url.URL{Scheme: sr.GetHTTPScheme(), Host: host}
		return u.String()
	}
	return ""
}

// SchemaRegistryAPITLS returns a SchemaRegistry listener that has TLS enabled.
// It returns nil if no TLS is configured.
func (r *Cluster) SchemaRegistryAPITLS() *SchemaRegistryAPI {
	allSchemaRegistry := r.SchemaRegistryListeners()
	for i := range allSchemaRegistry {
		schemaRegistry := &allSchemaRegistry[i]
		if schemaRegistry.TLS != nil && schemaRegistry.TLS.Enabled {
			return schemaRegistry
		}
	}
	return nil
}

// SchemaRegistryListeners returns all schema registry listeners
func (r *Cluster) SchemaRegistryListeners() []SchemaRegistryAPI {
	if r == nil || r.Spec.Configuration.SchemaRegistry == nil {
		return nil
	}
	if r.Spec.Configuration.SchemaRegistry.Name == "" {
		r.Spec.Configuration.SchemaRegistry.Name = SchemaRegistryExternalListenerName
	}
	res := []SchemaRegistryAPI{*r.Spec.Configuration.SchemaRegistry}
	return append(res, r.Spec.Configuration.SchemaRegistryAPI...)
}

// IsSchemaRegistryExternallyAvailable returns true if schema registry
// is enabled with external connectivity
func (r *Cluster) IsSchemaRegistryExternallyAvailable() bool {
	allSchemaRegistry := r.SchemaRegistryListeners()
	for i := range allSchemaRegistry {
		if allSchemaRegistry[i].External != nil && allSchemaRegistry[i].External.Enabled {
			return true
		}
	}
	return false
}

// IsSchemaRegistryTLSEnabled returns true if schema registry
// is enabled with TLS
func (r *Cluster) IsSchemaRegistryTLSEnabled() bool {
	allSchemaRegistry := r.SchemaRegistryListeners()
	for i := range allSchemaRegistry {
		if allSchemaRegistry[i].TLS != nil && allSchemaRegistry[i].TLS.Enabled {
			return true
		}
	}
	return false
}

// IsSchemaRegistryMutualTLSEnabled returns true if schema registry
// is enabled with mutual TLS
func (r *Cluster) IsSchemaRegistryMutualTLSEnabled() bool {
	allSchemaRegistry := r.SchemaRegistryListeners()
	for i := range allSchemaRegistry {
		if allSchemaRegistry[i].TLS != nil && allSchemaRegistry[i].TLS.RequireClientAuth {
			return true
		}
	}
	return false
}

// IsSchemaRegistryAuthHTTPBasic returns true if schema registry authentication method
// is enabled with HTTP Basic
func (r *Cluster) IsSchemaRegistryAuthHTTPBasic() bool {
	allSchemaRegistry := r.SchemaRegistryListeners()
	for i := range allSchemaRegistry {
		if allSchemaRegistry[i].AuthenticationMethod == httpBasicAuthorizationMechanism {
			return true
		}
	}
	return false
}

// IsUsingMaintenanceModeHooks tells if the cluster is configured to use maintenance mode hooks on the pods.
// Maintenance mode feature needs to be enabled for this to be relevant.
func (r *Cluster) IsUsingMaintenanceModeHooks() bool {
	if r == nil {
		return true
	}
	// enabled unless explicitly stated
	if r.Spec.RestartConfig != nil && r.Spec.RestartConfig.DisableMaintenanceModeHooks != nil {
		return !*r.Spec.RestartConfig.DisableMaintenanceModeHooks
	}
	return true
}

// ClusterStatus

// IsRestarting tells if the cluster is restarting due to a change in configuration or an upgrade in progress
func (s *ClusterStatus) IsRestarting() bool {
	// Let's consider the old field for a transition period
	return s.Restarting || s.DeprecatedUpgrading
}

// SetRestarting sets the cluster as restarting
func (s *ClusterStatus) SetRestarting(restarting bool) {
	s.Restarting = restarting
	// keep deprecated upgrading field as some external tools may still rely on it
	s.DeprecatedUpgrading = restarting
}

func (r *Cluster) GetDesiredReplicas() int32 {
	nps := r.GetNodePoolsFromSpec()
	if r == nil || len(nps) == 0 {
		return 0
	}

	replicas := int32(0)

	var npNamesSeen []string

	for i := range nps {
		np := nps[i]
		replicas += ptr.Deref(np.Replicas, 0)
		npNamesSeen = append(npNamesSeen, np.Name)
	}

	// We may have nodepools deleted from spec - but they are not yet fully deleted (in scale-down).
	// Source these from the status instead.
	for npName, npStatus := range r.Status.NodePools {
		if !slices.Contains(npNamesSeen, npName) {
			replicas += npStatus.Replicas
		}
	}

	return replicas
}

// TLSConfig is a generic TLS configuration
type TLSConfig struct {
	Enabled           bool                      `json:"enabled,omitempty"`
	RequireClientAuth bool                      `json:"requireClientAuth,omitempty"`
	IssuerRef         *cmmetav1.ObjectReference `json:"issuerRef,omitempty"`
	NodeSecretRef     *corev1.ObjectReference   `json:"nodeSecretRef,omitempty"`

	ClientCACertRef *corev1.TypedLocalObjectReference `json:"clientCACertRef,omitempty"`
}

// Kafka API

// GetPort returns API port
//
//nolint:gocritic // TODO KafkaAPI is now 81 bytes, consider a pointer
func (k KafkaAPI) GetPort() int {
	return k.Port
}

// GetTLS returns API TLSConfig
//
//nolint:gocritic // TODO KafkaAPI is now 81 bytes, consider a pointer
func (k KafkaAPI) GetTLS() *TLSConfig {
	return &TLSConfig{
		Enabled:           k.TLS.Enabled,
		RequireClientAuth: k.TLS.RequireClientAuth,
		IssuerRef:         k.TLS.IssuerRef,
		NodeSecretRef:     k.TLS.NodeSecretRef,
		ClientCACertRef:   k.TLS.ClientCACertRef,
	}
}

// GetExternal returns API's ExternalConnectivityConfig
//
//nolint:gocritic // TODO KafkaAPI is now 81 bytes, consider a pointer
func (k KafkaAPI) GetExternal() *ExternalConnectivityConfig {
	return &k.External
}

// IsMutualTLSEnabled returns true if API requires client aut
//
//nolint:gocritic // TODO KafkaAPI is now 81 bytes, consider a pointer
func (k KafkaAPI) IsMutualTLSEnabled() bool {
	return k.TLS.Enabled && k.TLS.RequireClientAuth
}

// Admin API

// GetPort returns API port
func (a *AdminAPI) GetPort() int {
	return a.Port
}

// GetTLS returns API TLSConfig
func (a *AdminAPI) GetTLS() *TLSConfig {
	return &TLSConfig{
		Enabled:           a.TLS.Enabled,
		ClientCACertRef:   a.TLS.ClientCACertRef,
		RequireClientAuth: a.TLS.RequireClientAuth,
		IssuerRef:         a.TLS.IssuerRef,
		NodeSecretRef:     a.TLS.NodeSecretRef,
	}
}

// GetExternal returns API's ExternalConnectivityConfig
func (a *AdminAPI) GetExternal() *ExternalConnectivityConfig {
	return &a.External
}

// GetHTTPScheme returns API HTTP scheme
func (a *AdminAPI) GetHTTPScheme() string {
	scheme := "http" //nolint:goconst // no need to set as constant
	if a.TLS.Enabled {
		scheme = "https" //nolint:goconst // no need to set as constant
	}
	return scheme
}

// IsMutualTLSEnabled returns true if API requires client auth
func (a *AdminAPI) IsMutualTLSEnabled() bool {
	return a.TLS.Enabled && a.TLS.RequireClientAuth
}

// SchemaRegistry API

// GetPort returns API port
func (s SchemaRegistryAPI) GetPort() int {
	return s.Port
}

// GetHTTPScheme returns API HTTP scheme
func (s SchemaRegistryAPI) GetHTTPScheme() string {
	scheme := "http"
	if s.TLS != nil && s.TLS.Enabled {
		scheme = "https"
	}
	return scheme
}

// GetTLS returns API TLSConfig
func (s SchemaRegistryAPI) GetTLS() *TLSConfig {
	if s.TLS == nil {
		return defaultTLSConfig()
	}
	return &TLSConfig{
		Enabled:           s.TLS.Enabled,
		RequireClientAuth: s.TLS.RequireClientAuth,
		IssuerRef:         s.TLS.IssuerRef,
		NodeSecretRef:     s.TLS.NodeSecretRef,
		ClientCACertRef:   s.TLS.ClientCACertRef,
	}
}

// GetExternal returns API's ExternalConnectivityConfig
func (s SchemaRegistryAPI) GetExternal() *ExternalConnectivityConfig {
	if s.External != nil {
		return &s.External.ExternalConnectivityConfig
	}
	return nil
}

// IsExternallyAvailable returns true if schema registry is enabled with external connectivity
func (s SchemaRegistryAPI) IsExternallyAvailable() bool {
	return s.External != nil && s.External.Enabled
}

// PandaProxy API

// GetPort returns API port
//
//nolint:gocritic // struct will be still quite small
func (p PandaproxyAPI) GetPort() int {
	return p.Port
}

// GetTLS returns API TLSConfig
//
//nolint:gocritic // struct will be still quite small
func (p PandaproxyAPI) GetTLS() *TLSConfig {
	return &TLSConfig{
		Enabled:           p.TLS.Enabled,
		RequireClientAuth: p.TLS.RequireClientAuth,
		IssuerRef:         p.TLS.IssuerRef,
		NodeSecretRef:     p.TLS.NodeSecretRef,
		ClientCACertRef:   p.TLS.ClientCACertRef,
	}
}

// GetExternal returns API's ExternalConnectivityConfig
//
//nolint:gocritic // struct will be still quite small
func (p PandaproxyAPI) GetExternal() *ExternalConnectivityConfig {
	return &p.External.ExternalConnectivityConfig
}

func defaultTLSConfig() *TLSConfig {
	return &TLSConfig{
		Enabled:           false,
		RequireClientAuth: false,
		IssuerRef:         nil,
		NodeSecretRef:     nil,
		ClientCACertRef:   nil,
	}
}

// IsSASLOnInternalEnabled replaces single check if sasl is enabled with multiple
// check. The external kafka listener is excluded from the check as panda proxy,
// schema registry and console should use internal kafka listener even if we have
// external enabled.
func (r *Cluster) IsSASLOnInternalEnabled() bool {
	return r != nil && r.Spec.KafkaEnableAuthorization != nil && *r.Spec.KafkaEnableAuthorization ||
		r.Spec.EnableSASL
}

func (r *Cluster) GetDecommissionBrokerID() *int32 {
	return r.Status.DecommissioningNode
}

func (r *Cluster) SetDecommissionBrokerID(id *int32) {
	r.Status.DecommissioningNode = id
}

// getNodePoolsFromSpec returns the NodePools defined in the spec.
// This contains the default NodePool (driven by spec.replicas for example), and the
// NodePools driven by the nodePools field.
func (r *Cluster) GetNodePoolsFromSpec() []NodePoolSpec {
	out := make([]NodePoolSpec, 0)
	if r.Spec.Replicas != nil {
		defaultNodePool := NodePoolSpec{
			Name:                           DefaultNodePoolName,
			Replicas:                       r.Spec.Replicas,
			Tolerations:                    r.Spec.Tolerations,
			NodeSelector:                   r.Spec.NodeSelector,
			Storage:                        r.Spec.Storage,
			Resources:                      r.Spec.Resources,
			AdditionalCommandlineArguments: r.Spec.Configuration.AdditionalCommandlineArguments,
		}
		if r.Spec.CloudStorage.CacheStorage != nil {
			defaultNodePool.CloudCacheStorage = *r.Spec.CloudStorage.CacheStorage
		}
		out = append(out, defaultNodePool)
	}
	out = append(out, r.Spec.NodePools...)

	return out
}

func (r *Cluster) CalculateCurrentReplicas() int32 {
	var result int32
	for _, np := range r.Status.NodePools {
		result += np.CurrentReplicas
	}
	return result
}

type NodePoolSpecWithDeleted struct {
	NodePoolSpec
	Deleted bool
}
