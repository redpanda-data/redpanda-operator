// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
package v1beta1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
)

func init() {
	// SchemeBuilder.Register(&Redpanda{}, &RedpandaList{})
}

// Redpanda defines the CRD for Redpanda clusters.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandas
// +kubebuilder:resource:shortName=rp
// +kubebuilder:printcolumn:name="License",type="string",JSONPath=`.status.conditions[?(@.type=="ClusterLicenseValid")].message`,description=""
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
// +kubebuilder:storageversion
type Redpanda struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda cluster.
	Spec RedpandaSpec `json:"spec,omitempty"`
	// Represents the current status of the Redpanda cluster.
	Status RedpandaStatus `json:"status,omitempty"`
}

// RedpandaList contains a list of Redpanda objects.
// +kubebuilder:object:root=true
type RedpandaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda resources.
	Items []Redpanda `json:"items"`
}

type RedpandaSpec struct {
	// Notable & intentional omissions:
	// - Global: helm specific value
	// - NameOverride: helm specific, no desire to support this
	// - FullnameOverride: "
	// - CommonLabels: Use PodTemplate
	// - Monitoring: Dropping support. Configure this yourself.
	// - RBAC: More issues than benefits.
	// - AuditLogging: No need for a bespoke value.

	Console ConsoleSpec // To be typed

	Enterprise Enterprise

	// +default:value="cluster.local"
	ClusterDomain *string `json:"clusterDomain"`

	// TODO should use Jan's type from V1
	ClusterConfig map[string]any

	// SASL / Auth? Maybe just BootStrapUser?

	NodePoolSpec NodePoolSpec `json:"nodePoolSpec"`
}

type RedpandaStatus struct {
}

type NodePoolSpec struct {
	Replicas       *int32
	BrokerTemplate BrokerTemplate
}

type BrokerTemplate struct {
	Image string

	Resources corev1.ResourceRequirements

	Tuning any // To be typed

	// This should have values similar to ClusterConfig so we can refer to external resources and use CEL functions.
	// TODO would Config be more or less clear here?
	NodeConfig map[string]any

	// Likely to be merged into NodeConfig w/ CEL functions.
	// rack: Expr(node_annotation('k8s.io/failure-domain')),
	RackAwareness RackAwareness

	// Missing from chart. Punt to v25.2?
	IOConfig any

	// Is there any reason to expose this?
	RPKConfig map[string]any

	// Requirement: At least 1 listener named "internal".
	// Add defaults?
	Listeners Listeners `json:"listeners"`

	// InitContainer options e.g. set datadir ownership. FS validator.
	// TODO: These will be merged into the configurator container (NO MORE BASH!)
	SetDataDirOwnership bool

	ValidateFSXFS bool

	// Require volumes with special names to be provided.
	// datadir = required
	// ts-cache = optional tiered storage cache
	VolumeClaimTemplates []corev1.PersistentVolumeClaim

	PodTemplate *PodTemplate
}

type Enterprise struct {
	// TODO might be better off as a dedicated type so Name isn't present?
	// Plenty of places we could reuse that.
	License *corev1.EnvVar `json:"license,omitempty"`
}

// TODO should this be part of the broker config?
// Leaning towards yes.
// RackAwareness for example could be:
// rack: Expr(node_annotation('k8s.io/failure-domain')),
// https://docs.redpanda.com/current/manage/rack-awareness/#configure-rack-awareness
type RackAwareness struct {
	Value string
}

type HTTPAuthenticationMethod string
type KafkaAuthenticationMethod string
type NoAuth string

type Listeners struct {
	RPC            Listener[NoAuth]
	Kafka          []Listener[KafkaAuthenticationMethod]
	Admin          []Listener[NoAuth]
	HTTP           []Listener[HTTPAuthenticationMethod] // TODO rename to PandaProxy?
	SchemaRegistry []Listener[NoAuth]
}

// https://docs.redpanda.com/current/manage/security/listener-configuration/
type Listener[T ~string] struct {
	Name string

	Port           int32
	AdvertisedPort *int32 // Needs to be a CEL expression or similar.
	// +default:value="0.0.0.0"
	Address        *string
	AdvertisedHost *string // Needs to be a CEL expression or similar.

	AuthenticationMethod *T
	RequireClientAuth    *bool

	TLS ListenerTLS // TODO inline this for a better UX?

}

type ListenerTLS struct {
	// For the internal listeners, we'll need a way to get a client certificate.
	// key_file: /etc/redpanda/tls/broker.key
	// cert_file: /etc/redpanda/tls/broker.crt
	// truststore_file: /etc/redpanda/tls/ca.crt

	// Need to support:
	// CertManager
	// Configmaps
	// Secrets
	// Raw values?
	// Support for PKCS12? https://redpandadata.atlassian.net/browse/K8S-347
}

type PodTemplate struct {
	*applycorev1.PodTemplateApplyConfiguration `json:",inline"`
}

func (t *PodTemplate) DeepCopy() *PodTemplate {
	// For some inexplicable reason, apply configs don't have deepcopy
	// generated for them.
	//
	// DeepCopyInto can be generated with just DeepCopy implemented. Sadly, the
	// easiest way to implement DeepCopy is to run this type through JSON. It's
	// highly unlikely that we'll hit a panic but it is possible to do so with
	// invalid values for resource.Quantity and the like.
	out := new(PodTemplate)
	data, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}
