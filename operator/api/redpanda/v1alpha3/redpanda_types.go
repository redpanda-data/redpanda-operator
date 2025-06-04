package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	ClusterDomain string       `json:"clusterDomain,omitempty"`
	Enterprise    Enterprise   `json:"enterprise"`
	NodePoolSpec  NodePoolSpec `json:"nodePoolSpec"`
	Listeners     Listeners    `json:"listeners"`
	Console       ConsoleSpec  `json:"console,omitempty"`
}

type Enterprise struct {
	License *License `json:"license,omitempty"`
}

type License struct {
	Value     string            `json:"value"`
	ValueFrom *LicenseValueFrom `json:"valueFrom,omitempty"`
}

// May seem silly to have a dedicated type for this but it keeps the same
type LicenseValueFrom struct {
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef"`
}

type AuthenticationMethod string

// TODO Add CEL validation that requires at least 1 "internal" listener
// OR should that be implicit / defaulted if not defined?
type Listeners struct {
	// TODO defaults in CRD or nullable w/ implicit defaults?
	RPC   Listener   `json:"rpc,omitempty"`
	Kafka []Listener `json:"kafka,omitempty"`
	Admin []Listener `json:"admin,omitempty"`
	// TODO(chrisseto) is PandaProxy or HTTP a better name here?
	HTTP           []Listener `json:"http,omitempty"`
	SchemaRegistry []Listener `json:"schemaRegistry,omitempty"`
}

// https://docs.redpanda.com/current/manage/security/listener-configuration/
type Listener struct {
	Name string `json:"name"`

	// +default:value="0.0.0.0"
	Address        *string      `json:"address"`
	AdvertisedHost *ValueSource `json:"advertisedHost,omitempty"`

	Port           int32        `json:"port"`
	AdvertisedPort *ValueSource `json:"advertisedPort,omitempty"`

	AuthenticationMethod *AuthenticationMethod `json:"authenticationMethod,omitempty"`
	RequireClientAuth    *bool                 `json:"requireClientAuth,omitempty"`

	// TLS *ListenerTLSSource `json:"tls,omitempty"`
	// Service ServiceConfig
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
