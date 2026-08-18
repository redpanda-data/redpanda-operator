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
	"encoding/json"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
)

// NOTE: the types in this file are forked from the OSS operator's
// redpanda/v1alpha2 package (redpanda_clusterspec_types.go,
// node_pool_types.go, redpanda_types.go). Definitions, kubebuilder markers,
// json tags, and doc comments are copied verbatim so the generated CRD
// schemas remain byte-identical.

// RedpandaImage configures the Redpanda container image settings in the Helm values.
type RedpandaImage struct {
	// Specifies the image repository to pull from.
	Repository *string `json:"repository,omitempty"`
	// Specifies the image tag.
	Tag *string `json:"tag,omitempty"`
	// Specifies the strategy used for pulling images from the repository. For available values, see https://kubernetes.io/docs/concepts/containers/images/#image-pull-policy.
	PullPolicy *string `json:"pullPolicy,omitempty"`
}

// RackAwareness configures rack awareness in the Helm values. See https://docs.redpanda.com/current/manage/kubernetes/kubernetes-rack-awareness/.
type RackAwareness struct {
	// Specifies whether rack awareness is enabled. When enabled, Kubernetes failure zones are treated as racks. Redpanda maps each rack to a failure zone and places partition replicas across them. Requires `rbac.enabled` set to `true`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies the key in Node labels or annotations to use to denote failure zones.
	NodeAnnotation *string `json:"nodeAnnotation,omitempty"`
}

// Auth configures authentication in the Helm values. See https://docs.redpanda.com/current/manage/kubernetes/security/authentication/sasl-kubernetes/.
type Auth struct {
	// Configures SASL authentication in the Helm values.
	SASL *SASL `json:"sasl,omitempty"`
}

// SASL configures SASL authentication in the Helm values.
type SASL struct {
	// Enables SASL authentication. If you enable SASL authentication, you must provide a Secret name in `secretRef`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies the default authentication mechanism to use for superusers. Options are `SCRAM-SHA-256` and `SCRAM-SHA-512`.
	// +kubebuilder:validation:Enum=SCRAM-SHA-256;SCRAM-SHA-512
	Mechanism *string `json:"mechanism,omitempty"`
	// If `users` is empty, `secretRef` specifies the name of the Secret that contains your superuser credentials in the format <username>:<password>:<optional-authentication-mechanism>. Otherwise, `secretRef` specifies the name of the Secret that the chart creates to store the credentials in `users`.
	SecretRef *string `json:"secretRef,omitempty"`
	// Specifies a list of superuser credentials.
	Users []UsersItems `json:"users,omitempty"`
	// Specifies configuration about the bootstrap user.
	BootstrapUser *BootstrapUser `json:"bootstrapUser,omitempty"`
}

// BootstrapUser configures the user used to bootstrap Redpanda when SASL is enabled.
type BootstrapUser struct {
	// Name specifies the name of the bootstrap user created for the cluster, if unspecified
	// defaults to "kubernetes-controller".
	// NOTE: for the StretchCluster kind this field is currently ignored; the bootstrap
	// username is fixed to "kubernetes-controller". Only secretKeyRef is honored there.
	Name *string `json:"name,omitempty"`
	// Specifies the location where the generated password will be written or a pre-existing
	// password will be read from. For the StretchCluster kind the referenced key is required
	// (there is no default key at the CRD level).
	//
	// For the StretchCluster kind the referenced Secret is read and replicated by the
	// operator to every member cluster in the same namespace, so whoever can set this field
	// is trusted to name a Secret whose contents may be copied across all member clusters of
	// the mesh. The operator never overwrites an existing Secret of that name on a member
	// cluster.
	//
	// IMPORTANT: for the StretchCluster kind the bootstrap password is consumed only when the
	// cluster is first bootstrapped and cannot be rotated afterwards by editing this Secret,
	// changing secretKeyRef to a different location, or removing secretKeyRef. Redpanda keeps
	// the SCRAM credential it was bootstrapped with, so changing the source after bootstrap
	// makes the operator and brokers authenticate with a credential the cluster never learned.
	// Rotate the bootstrap user out of band (via the admin API) instead.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// Specifies the authentication mechanism to use for the bootstrap user. Options are `SCRAM-SHA-256` and `SCRAM-SHA-512`.
	// NOTE: for the StretchCluster kind this field is currently ignored; the mechanism is
	// taken from sasl.mechanism.
	// +kubebuilder:validation:Enum=SCRAM-SHA-256;SCRAM-SHA-512
	Mechanism *string `json:"mechanism,omitempty"`
}

// UsersItems configures a list of superusers in the Helm values.
type UsersItems struct {
	// Specifies the authentication mechanism to use for superusers. Overrides the default in `SASL`. Options are `SCRAM-SHA-256` and `SCRAM-SHA-512`.
	// +kubebuilder:validation:Enum=SCRAM-SHA-256;SCRAM-SHA-512
	Mechanism *string `json:"mechanism,omitempty"`
	// Specifies the name of the superuser.
	Name *string `json:"name,omitempty"`
	// Specifies the superuser password.
	Password *string `json:"password,omitempty"`
}

// TLS configures TLS in the Helm values. See https://docs.redpanda.com/current/manage/kubernetes/security/tls/.
type TLS struct {
	// Lists all available certificates in the cluster. You can reference a specific certificate’s name in each listener’s `listeners.<listener name>.tls.cert` setting.
	Certs map[string]*Certificate `json:"certs,omitempty"`
	// Enables TLS globally for all listeners. Each listener must include a certificate name in its `<listener>.tls` object. To allow you to enable TLS for individual listeners, certificates are always loaded, even if TLS is disabled.
	Enabled *bool `json:"enabled,omitempty"`
}

// Certificate configures TLS certificates.
type Certificate struct {
	// Specify the name of an existing Issuer or ClusterIssuer resource to use to generate certificates. Requires cert-manager. See https://cert-manager.io/v1.1-docs.
	IssuerRef *IssuerRef `json:"issuerRef,omitempty"`
	// Specify the name of an existing Secret resource that contains your TLS certificate.
	SecretRef *SecretRef `json:"secretRef,omitempty"`
	// Specify the name of an existing Secret resource that contains your client TLS certificate.
	ClientSecretRef *SecretRef `json:"clientSecretRef,omitempty"`
	// Specifies the validity duration of certificates generated with `issuerRef`.
	Duration *metav1.Duration `json:"duration,omitempty"`
	// Specifies whether to include the `ca.crt` file in the trust stores of all listeners. Set to `true` only for certificates that are not authenticated using public certificate authorities (CAs).
	CAEnabled *bool `json:"caEnabled,omitempty"`
	// Specifies you wish to have Kubernetes internal dns names (IE the headless service of the redpanda StatefulSet) included in `dnsNames` of the  certificate even, when supplying an issuer.
	ApplyInternalDNSNames *bool `json:"applyInternalDNSNames,omitempty"`

	Enabled *bool `json:"enabled,omitempty"`
}

// IssuerRef configures the Issuer or ClusterIssuer resource to use to generate certificates. Requires cert-manager. See https://cert-manager.io/v1.1-docs.
type IssuerRef struct {
	// Specifies the name of the resource.
	Name *string `json:"name,omitempty"`
	// Specifies the kind of resource. One of `Issuer` or `ClusterIssuer`.
	Kind  *string `json:"kind,omitempty"`
	Group *string `json:"group,omitempty"`
}

// SecretRef configures the Secret resource that contains existing TLS certificates.
type SecretRef struct {
	// Specifies the name of the Secret resource.
	Name *string `json:"name,omitempty"`
}

// TrustStore is a mapping from a value on either a Secret or ConfigMap to the
// `truststore_path` field of a listener.
// +kubebuilder:validation:MaxProperties=1
// +kubebuilder:validation:MinProperties=1
type TrustStore struct {
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef,omitempty"`
}

// ExternalService allows you to enable or disable the creation of an external Service type.
type ExternalService struct {
	// Specifies whether to create the external Service. If set to `false`, the external Service type is not created. You can still set your cluster with external access but not create the supporting Service. Set this to `false` to manage your own Service.
	Enabled *bool `json:"enabled,omitempty"`
}

// External defines external connectivity settings in the Helm values.
type External struct {
	// Specifies addresses for the external listeners to advertise.Provide one entry for each broker in order of StatefulSet replicas. The number of brokers is defined in `statefulset.replicas`. The values can be IP addresses or DNS names. If `external.domain` is set, the domain is appended to these values.
	Addresses []string `json:"addresses,omitempty"`
	// Adds custom annotations to the external Service.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies the domain to advertise to external clients. If specified, then it will be appended to the `external.addresses` values as each broker's advertised address.
	Domain *string `json:"domain,omitempty"`
	// Specifies whether the external access is enabled.
	Enabled *bool `json:"enabled,omitempty"`
	// Configures the external Service resource.
	Service *ExternalService `json:"service,omitempty"`
	// Source range for external access. Only applicable when `external.type` is LoadBalancer.
	SourceRanges []string `json:"sourceRanges,omitempty"`
	// Specifies the external Service type. Only NodePort and LoadBalancer are supported. If undefined, then advertised listeners will be configured in Redpanda, but the Helm chart will not create a Service. NodePort is recommended in cases where latency is a priority.
	Type *string `json:"type,omitempty"`
	// Defines externalDNS configurations.
	ExternalDNS *ExternalDNS `json:"externalDns,omitempty"`
	// Specifies a naming prefix template for external Services.
	PrefixTemplate *string `json:"prefixTemplate,omitempty"`
	// Configures Gateway API TLSRoute-based external access. When enabled, ClusterIP services and TLSRoute resources are created instead of NodePort/LoadBalancer services. The Gateway itself must be managed externally.
	Gateway *GatewayExternalConfig `json:"gateway,omitempty"`
}

// GatewayExternalConfig holds configuration for Gateway API-based external access using TLSRoute resources with SNI-based routing.
type GatewayExternalConfig struct {
	// Enables Gateway API TLSRoute-based external access.
	Enabled *bool `json:"enabled,omitempty"`
	// Defines which Gateway(s) handle the TLSRoutes. At least one parent reference must be provided.
	// +kubebuilder:validation:MinItems=1
	ParentRefs []GatewayParentRefConfig `json:"parentRefs,omitempty"`
	// The port advertised to clients. Defaults to 443.
	AdvertisedPort *int32 `json:"advertisedPort,omitempty"`
}

// GatewayParentRefConfig identifies a Gateway (or ListenerSet) that should handle the TLSRoute traffic. Schema mirrors the upstream Gateway API ParentReference.
type GatewayParentRefConfig struct {
	// API group of the referent. Defaults to "gateway.networking.k8s.io".
	Group *string `json:"group,omitempty"`
	// Kind of the referent. Defaults to "Gateway".
	Kind *string `json:"kind,omitempty"`
	// Name of the referent.
	Name string `json:"name"`
	// Namespace of the referent.
	Namespace *string `json:"namespace,omitempty"`
	// Name of a section within the target resource.
	SectionName *string `json:"sectionName,omitempty"`
}

// CredentialSecretRef can be used to set cloud_storage_secret_key from referenced Kubernetes Secret
type CredentialSecretRef struct {
	AccessKey *SecretWithConfigField `json:"accessKey,omitempty"`
	SecretKey *SecretWithConfigField `json:"secretKey,omitempty"`
}

type SecretWithConfigField struct {
	Key              *string `json:"key,omitempty"`
	Name             *string `json:"name,omitempty"`
	ConfigurationKey *string `json:"configurationKey,omitempty"`
}

// PersistentVolume configures configurations for a PersistentVolumeClaim to use to store the Redpanda data directory.
type PersistentVolume struct {
	// Adds annotations to the PersistentVolumeClaims to provide additional information or metadata that can be used by other tools or libraries.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies whether to enable the Helm chart to create PersistentVolumeClaims for Pods.
	Enabled *bool `json:"enabled,omitempty"`
	// Applies labels to the PersistentVolumeClaims to facilitate identification and selection based on custom criteria.
	Labels map[string]string `json:"labels,omitempty"`
	// Specifies the storage capacity required.
	Size *resource.Quantity `json:"size,omitempty"`
	// Specifies the StorageClass for the PersistentVolumeClaims to determine how PersistentVolumes are provisioned and managed.
	StorageClass *string `json:"storageClass,omitempty"`
	// Option to change volume claim template name for tiered storage persistent volume if tiered.mountType is set to `persistentVolume`
	NameOverwrite *string `json:"nameOverwrite,omitempty"`
}

// PodTemplate will pass label and annotation to Statefulset Pod template.
type PodTemplate struct {
	Labels      map[string]string                      `json:"labels,omitempty"`
	Annotations map[string]string                      `json:"annotations,omitempty"`
	Spec        *applycorev1.PodSpecApplyConfiguration `json:"spec,omitempty"`
}

func (in *PodTemplate) DeepCopy() *PodTemplate {
	if in == nil {
		return nil
	}
	out := new(PodTemplate)
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		maps.Copy(out.Labels, in.Labels)
	}
	if in.Annotations != nil {
		out.Annotations = make(map[string]string, len(in.Annotations))
		maps.Copy(out.Annotations, in.Annotations)
	}
	if in.Spec != nil {
		// JSON round-trip deep copy for apply-configuration types.
		data, _ := json.Marshal(in.Spec)
		out.Spec = &applycorev1.PodSpecApplyConfiguration{}
		_ = json.Unmarshal(data, out.Spec)
	}
	return out
}

// Config configures Redpanda config properties supported by Redpanda that may not work correctly in a Kubernetes cluster. Changing these values from the defaults comes with some risk. Use these properties to customize various Redpanda configurations that are not available in the `RedpandaClusterSpec`. These values have no impact on the configuration or behavior of the Kubernetes objects deployed by Helm, and therefore should not be modified for the purpose of configuring those objects. Instead, these settings get passed directly to the Redpanda binary at startup.
type Config struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies cluster configuration properties. See https://docs.redpanda.com/current/reference/cluster-properties/.
	RPK *runtime.RawExtension `json:"rpk,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies cluster configuration properties. See https://docs.redpanda.com/current/reference/cluster-properties/.
	Cluster *runtime.RawExtension `json:"cluster,omitempty"`
	// Holds values (or references to values) that should be used to configure the cluster; these
	// are resolved late in order to avoid embedding secrets directly into bootstrap configurations
	// exposed as Kubernetes configmaps.
	ExtraClusterConfiguration ClusterConfiguration `json:"extraClusterConfiguration,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies broker configuration properties. See https://docs.redpanda.com/current/reference/node-properties/.
	Node *runtime.RawExtension `json:"node,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies tunable configuration properties. See https://docs.redpanda.com/current/reference/tunable-properties/.
	Tunable *runtime.RawExtension `json:"tunable,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies tunable configuration properties. See https://docs.redpanda.com/current/reference/tunable-properties/.
	SchemaRegistryClient *runtime.RawExtension `json:"schema_registry_client,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies tunable configuration properties. See https://docs.redpanda.com/current/reference/tunable-properties/.
	PandaProxyClient *runtime.RawExtension `json:"pandaproxy_client,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
}

// CPU configures CPU resources for containers. See https://docs.redpanda.com/current/manage/kubernetes/manage-resources/.
type CPU struct {
	// Specifies the number of CPU cores available to the application. Redpanda makes use of a thread per core model. For details, see https://docs.redpanda.com/current/get-started/architecture/#thread-per-core-model. For this reason, Redpanda should only be given full cores. Note: You can increase cores, but decreasing cores is not currently supported. See the GitHub issue:https://github.com/redpanda-data/redpanda/issues/350. This setting is equivalent to `--smp`, `resources.requests.cpu`, and `resources.limits.cpu`. For production, use `4` or greater.
	Cores *resource.Quantity `json:"cores,omitempty"`
	// Specifies whether Redpanda assumes it has all of the provisioned CPU. This should be `true` unless the container has CPU affinity. Equivalent to: `--idle-poll-time-us 0`, `--thread-affinity 0`, and `--poll-aio 0`. If the value of full cores in `resources.cpu.cores` is less than `1`, this setting is set to `true`.
	Overprovisioned *bool `json:"overprovisioned,omitempty"`
}

// ContainerResources defines resource limits for containers.
type ContainerResources struct {
	// Specifies the maximum resources that can be allocated to a container.
	Max *resource.Quantity `json:"max,omitempty"`
	// Specifies the minimum resources required for a container.
	Min *resource.Quantity `json:"min,omitempty"`
}

// Memory configures memory resources.
type Memory struct {
	// Defines resource limits for containers.
	Container *ContainerResources `json:"container,omitempty"`
	// Enables memory locking. For production, set to `true`.
	EnableMemoryLocking *bool `json:"enable_memory_locking,omitempty"`
	// Allows you to optionally specify the memory size for both the Redpanda process and the underlying reserved memory used by Seastar.
	Redpanda *RedpandaMemory `json:"redpanda,omitempty"`
}

// RedpandaMemory allows you to optionally specify the memory size for the Redpanda process, including the Seastar subsystem. By default, this section is omitted, and memory sizes are calculated automatically based on the container's total memory allocation. When you configure this section and manually set the memory and reserveMemory values, the automatic calculation is disabled.
//
// If you are setting these values manually, follow these guidelines carefully. Incorrect settings can lead to performance degradation, instability, or even data loss. The total memory allocated to a container is determined as the sum of the following two areas:
//
// - Redpanda (including Seastar):
// Defined by the `--memory` parameter. Includes the memory used by the Redpanda process and the reserved memory allocated for Seastar. A minimum of 2Gi per core is required, and this value typically accounts for ~80% of the container’s total memory. For production, allocate at least 8Gi.
//
// - Operating system (OS):
// Defined by the `--reserve-memory` parameter. Represents the memory available for the operating system and other processes within the container.
type RedpandaMemory struct {
	// Memory for the Redpanda process. This must be lower than the container's memory (`resources.memory.container.min` if provided, otherwise `resources.memory.container.max`). Equivalent to `--memory`. For production, use 8Gi or greater.
	Memory *resource.Quantity `json:"memory,omitempty"`
	// Memory reserved for the OS. Any value above 1Gi will provide diminishing performance benefits. Equivalent to `--reserve-memory`. For production, use 1Gi.
	ReserveMemory *resource.Quantity `json:"reserveMemory,omitempty"`
}

// RBAC configures role-based access control (RBAC).
type RBAC struct {
	// Adds custom annotations to the RBAC resources.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Whether RBAC is enabled. Enable for features that need extra privileges, such as rack awareness. If you use the Redpanda Operator, you must deploy it with the `--set rbac.createRPKBundleCRs=true` flag to give it the required ClusterRoles.
	Enabled        *bool `json:"enabled,omitempty"`
	RPKDebugBundle *bool `json:"rpkDebugBundle,omitempty"`
}

// ServiceAccount configures Service Accounts.
type ServiceAccount struct {
	// Specifies whether a service account should automount API-Credentials
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`
	// Adds custom annotations to the ServiceAccount resources.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies whether a ServiceAccount should be created.
	Create *bool `json:"create,omitempty"`
	// Specifies the name of the ServiceAccount.
	Name *string `json:"name,omitempty"`
}

// InitContainerImage configures the init container image used to perform initial setup tasks before the main containers start.
type InitContainerImage struct {
	Repository *string `json:"repository,omitempty"`
	Tag        *string `json:"tag,omitempty"`
}

// Monitoring configures monitoring resources for Redpanda. See https://docs.redpanda.com/current/manage/kubernetes/monitoring/monitor-redpanda/.
type Monitoring struct {
	// Specifies whether to create a ServiceMonitor that can be used by Prometheus Operator or VictoriaMetrics Operator to scrape the metrics.
	Enabled *bool `json:"enabled,omitempty"`
	// Adds custom labels to the ServiceMonitor resource.
	Labels map[string]string `json:"labels,omitempty"`
	// Specifies how often to scrape metrics.
	ScrapeInterval *string `json:"scrapeInterval,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specifies tls configuration properties.
	TLSConfig   *runtime.RawExtension `json:"tlsConfig,omitempty"`
	EnableHTTP2 *bool                 `json:"enableHttp2,omitempty"`
}

// ExternalDNS configures externalDNS.
type ExternalDNS struct {
	// Specifies whether externalDNS annotations are added to LoadBalancer Services. If you enable externalDns, each LoadBalancer Service defined in `external.type` will be annotated with an external-dns hostname that matches `external.addresses[i]`.`external.domain`.
	Enabled *bool `json:"enabled,omitempty"`
}

// EnterpriseLicenseSecretRef configures a reference to a Secret resource that contains the Enterprise license key.
type EnterpriseLicenseSecretRef struct {
	// Specifies the key that is contains the Enterprise license in the Secret.
	Key *string `json:"key,omitempty"`
	// Specifies the name of the Secret resource to use.
	Name *string `json:"name,omitempty"`
}

// Enterprise configures an Enterprise license key to enable Redpanda Enterprise features. Requires the post-install job to be enabled (default). See https://docs.redpanda.com/current/get-started/licenses/.
type Enterprise struct {
	// Specifies the Enterprise license key.
	License *string `json:"license,omitempty"`
	// Defines a reference to a Secret resource that contains the Enterprise license key.
	LicenseSecretRef *EnterpriseLicenseSecretRef `json:"licenseSecretRef,omitempty"`
}

type PoolConfigurator struct {
	// Chart default: []
	AdditionalCLIArgs []string `json:"additionalCLIArgs,omitempty"`
}

type PoolSetDataDirOwnership struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
}

type PoolFSValidator struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
	// Chart default: xfs
	ExpectedFS *string `json:"expectedFS,omitempty"`
}

type PoolInitContainers struct {
	FSValidator         *PoolFSValidator         `json:"fsValidator,omitempty"`
	SetDataDirOwnership *PoolSetDataDirOwnership `json:"setDataDirOwnership,omitempty"`
	Configurator        *PoolConfigurator        `json:"configurator,omitempty"`
}

// NodePoolServices configures overrides for Services created by the operator
// for this NodePool.
type NodePoolServices struct {
	// PerPod configures overrides for per-pod ClusterIP Services.
	PerPod *PerPodServices `json:"perPod,omitempty"`
}

// PerPodServices configures overrides for per-pod ClusterIP Services.
// Local overrides apply to Services for pods in the same K8s cluster as the
// NodePool; Remote overrides apply to Services created for pods in other clusters.
type PerPodServices struct {
	// Local overrides are applied to per-pod Services for pods in the local cluster.
	Local *PerPodServiceOverride `json:"local,omitempty"`
	// Remote overrides are applied to per-pod Services for pods in remote clusters.
	Remote *PerPodServiceOverride `json:"remote,omitempty"`
}

// PerPodServiceOverride defines overrides for a per-pod Service using
// apply-configuration types. Only fields that are set will be merged
// into the generated Service.
type PerPodServiceOverride struct {
	// Enabled controls whether this per-pod Service is created. Defaults to true.
	Enabled     *bool                                      `json:"enabled,omitempty"`
	Labels      map[string]string                          `json:"labels,omitempty"`
	Annotations map[string]string                          `json:"annotations,omitempty"`
	Spec        *applycorev1.ServiceSpecApplyConfiguration `json:"spec,omitempty"`
}

// IsEnabled returns whether this override allows the Service to be created.
// Defaults to true when Enabled is nil.
func (in *PerPodServiceOverride) IsEnabled() bool {
	if in == nil || in.Enabled == nil {
		return true
	}
	return *in.Enabled
}

func (in *PerPodServiceOverride) DeepCopy() *PerPodServiceOverride {
	if in == nil {
		return nil
	}
	out := new(PerPodServiceOverride)
	if in.Enabled != nil {
		out.Enabled = ptr.To(*in.Enabled)
	}
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		maps.Copy(out.Labels, in.Labels)
	}
	if in.Annotations != nil {
		out.Annotations = make(map[string]string, len(in.Annotations))
		maps.Copy(out.Annotations, in.Annotations)
	}
	if in.Spec != nil {
		// JSON round-trip deep copy for apply-configuration types.
		data, _ := json.Marshal(in.Spec)
		out.Spec = &applycorev1.ServiceSpecApplyConfiguration{}
		_ = json.Unmarshal(data, out.Spec)
		// Preserve empty Selector map (signals "clear selector") which
		// json.Marshal drops due to omitempty.
		if in.Spec.Selector != nil && out.Spec.Selector == nil {
			out.Spec.Selector = make(map[string]string)
		}
	}
	return out
}

func (in *NodePoolServices) DeepCopy() *NodePoolServices {
	if in == nil {
		return nil
	}
	return &NodePoolServices{
		PerPod: in.PerPod.DeepCopy(),
	}
}

func (in *PerPodServices) DeepCopy() *PerPodServices {
	if in == nil {
		return nil
	}
	return &PerPodServices{
		Local:  in.Local.DeepCopy(),
		Remote: in.Remote.DeepCopy(),
	}
}

type RedpandaLicenseStatus struct {
	Violation     bool     `json:"violation"`
	InUseFeatures []string `json:"inUseFeatures"`
	// +optional
	Expired *bool `json:"expired,omitempty"`
	// +optional
	Type *string `json:"type,omitempty"`
	// +optional
	Organization *string `json:"organization,omitempty"`
	// +optional
	Expiration *metav1.Time `json:"expiration,omitempty"`
}
