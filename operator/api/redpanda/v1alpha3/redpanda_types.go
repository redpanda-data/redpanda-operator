package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&Redpanda{}, &RedpandaList{})
}

// Redpanda defines the CRD for Redpanda clusters.
// +kubebuilder:skipversion
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=redpandas
// +kubebuilder:resource:shortName=rp
// TODO(chrisseto): Add back subresource:status and print columns
type Redpanda struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RedpandaSpec `json:"spec"`
	// +kubebuilder:default={conditions: {{type: "Ready", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Healthy", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "LicenseValid", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "ResourcesSynced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "ConfigurationApplied", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status RedpandaStatus `json:"status,omitempty"`
}

// RedpandaList contains a list of Redpanda objects.
// +kubebuilder:object:root=true
// +kubebuilder:skipversion
type RedpandaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda resources.
	Items []Redpanda `json:"items"`
}

// RedpandaSpec defines the desired state of the Redpanda cluster.
type RedpandaSpec struct {
	// +kubebuilder:default="cluster.local."
	ClusterDomain string               `json:"clusterDomain,omitempty"`
	Enterprise    Enterprise           `json:"enterprise"`
	NodePoolSpec  EmbeddedNodePoolSpec `json:"nodePoolSpec"`
	Listeners     Listeners            `json:"listeners"`
	ClusterConfig ClusterConfig        `json:"clusterConfig"`

	// TODO
	// Console       ConsoleSpec            `json:"console,omitempty"`
}

type ClusterConfig map[string]ValueSource

type Enterprise struct {
	License License `json:"license,omitempty"`
}

type License struct {
	Value     string            `json:"value,omitempty"`
	ValueFrom *LicenseValueFrom `json:"valueFrom,omitempty"`
}

// May seem silly to have a dedicated type for this but it keeps the same
type LicenseValueFrom struct {
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef"`
}

// TODO Add enum options
type AuthenticationMethod string

// TODO Add CEL validation that requires at least 1 "internal" listener
// OR should that be implicit / defaulted if not defined?
// TODO defaults in CRD or nullable w/ implicit defaults?
// TODO enforce name uniquness
// TODO enforce port uniquness?
type Listeners struct {
	RPC RPCListener `json:"rpc,omitempty"`

	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Kafka []Listener `json:"kafka,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Admin []Listener `json:"admin,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	SchemaRegistry []Listener `json:"schemaRegistry,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// TODO(chrisseto) is PandaProxy or HTTP a better name here?
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	HTTP []Listener `json:"http,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

type RPCListener struct {
	// +default:value=33145
	Port int32        `json:"port,omitempty"`
	TLS  *ListenerTLS `json:"tls,omitempty"`
}

// https://docs.redpanda.com/current/manage/security/listener-configuration/
type Listener struct {
	Name                 string                `json:"name"`
	Port                 int32                 `json:"port"`
	AuthenticationMethod *AuthenticationMethod `json:"authenticationMethod,omitempty"`
	// TODO replace/convert these into EXPR fields.
	AdvertisedPorts []int32      `json:"advertisedPorts,omitempty"`
	PrefixTemplate  *string      `json:"prefixTemplate,omitempty"`
	TLS             *ListenerTLS `json:"tls,omitempty"`

	// Service ServiceConfig
	// TODO: Address isn't currently supported in the chart or CRD. Though it
	// might be nice to allow it in the future?
	// +default:value="0.0.0.0"
	// Address        string       `json:"address"`
	// AdvertisedHost *ValueSource `json:"advertisedHost,omitempty"`
	// AdvertisedPort *ValueSource `json:"advertisedPort,omitempty"`
}

type ListenerTLS struct {
	RequireClientAuth bool        `json:"requireClientAuth,omitempty"`
	TrustStore        *TrustStore `json:"trustStore,omitempty"`

	CertificateSource `json:",inline"`
}

type CertificateSource struct {
	IssuerRef   *IssuerRefCertificateSource   `json:"issuerRef,omitempty"`
	CertManager *CertManagerCertificateSource `json:"certManager,omitempty"`
	Secrets     *SecretCertificateSource      `json:"secrets,omitempty"`
}

type SecretCertificateSource struct {
	SecretRef       corev1.LocalObjectReference  `json:"secretRef"`
	ClientSecretRef *corev1.LocalObjectReference `json:"clientSecretRef,omitempty"`
}

// ConditionStatus represents a condition's status.
// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
type IssuerKind string

type IssuerRefCertificateSource struct {
	// Name of the resource being referred to.
	Name string `json:"name"`
	// Kind of the resource being referred to.
	Kind IssuerKind `json:"kind,omitempty"`
}

type CertManagerCertificateSource struct {
	// default:value=43800h
	Duration *metav1.Duration `json:"duration,omitempty"`
}

type TrustStore struct {
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef"`
}

type RedpandaStatus struct {
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LicenseStatus contains information about the current state of any
	// installed license in the Redpanda cluster.
	// +optional
	LicenseStatus *LicenseStatus `json:"license,omitempty"`

	// NodePools contains information about the node pools associated
	// with this cluster.
	// +optional
	NodePools []NodePoolStatus `json:"nodePools,omitempty"`

	// ConfigVersion contains the configuration version written in
	// Redpanda used for restarting broker nodes as necessary.
	// +optional
	ConfigVersion string `json:"configVersion,omitempty"`
}

type LicenseStatus struct {
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
