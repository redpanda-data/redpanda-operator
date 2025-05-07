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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
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

	// Long term Console should be broken out into it's own CRD. We're not
	// going to take that on in v25.1.1 BUT we will type this in such a way
	// that it seems to be it's own CRD.
	Console *ConsoleSpec `json:"console"`

	Enterprise Enterprise `json:"enterprise"`

	// +default:value="cluster.local"
	ClusterDomain *string `json:"clusterDomain,omitempty"`

	// TODO should use Jan's type from V1
	// Conversion of this could be tricky.
	ClusterConfig map[string]ValueSource `json:"clusterConfig"`

	// SASL / Auth? Maybe just BootStrapUser?

	// While listeners are technically defined on a per broker basis, it's
	// unlikely anyone will want to configure them that way.
	//
	// Requirement: At least 1 listener named "internal".
	// Considering: Moving Service configuration into Listeners.
	// Add defaults?
	Listeners Listeners `json:"listeners"`

	NodePoolSpec NodePoolSpec `json:"nodePoolSpec"`
}

type RedpandaStatus struct {
	// This will very likely likely be the same as v1alpha2
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

// TODO define enum values
// There's no support for generics in kube-builder: https://github.com/kubernetes-sigs/controller-tools/issues/844
// So we're limited to using a single catch all type
type AuthenticationMethod string

// TODO Add CEL validation that requires at least 1 "internal" listener
// OR should that be implicit / defaulted if not defined?
type Listeners struct {
	// TODO defaults in CRD or nullable w/ implicit defaults?
	RPC            Listener   `json:"rpc,omitempty"`
	Kafka          []Listener `json:"kafka,omitempty"`
	Admin          []Listener `json:"admin,omitempty"`
	HTTP           []Listener `json:"http,omitempty"` // TODO is PandaProxy or HTTP a better name here?
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

	TLS *ListenerTLSSource `json:"tls,omitempty"`
	// Service ServiceConfig
}

// Need to support
// - Service types (LB vs NodePort)
// - Per Broker vs All Brokers
// - Aggregation onto a single service (e.g. name?)
// - External DNS (Annotation + Templating on a per broker basis)
// - Full service spec support??
// - Label + Annotation control
// -
type ServiceConfig struct {
	// Optional, when set will aggregate this service definition with all others of the same name.
	Name *string `json:"name,omitempty"`
	// Should this service be configured and deployed once per broker or should there be a single instance thereof?
	PerBroker bool `json:"perBroker"`
	// Template  applycorev1.ServiceApplyConfiguration
}

type ListenerTLSSource struct {
	Direct      *DirectListenerTLS `json:"direct,omitempty"`
	CertManager *CertManagerTLS    `json:"certManager,omitempty"`

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

type CertManagerTLS struct {
	// If Provided, the issuer to use when creating certificates.
	// Otherwise one will be generated and bootstrapped for you.
	IssuerRef map[string]string `json:"IssuerRef"` // Should allow ClusterIssuer or Issuer

	// Mutually exclusive with Issuer. Sets the name of the Issuer to use which
	// allows sharing of a trust chain. Also present for backwards
	// compatibility.
	IssuerName string `json:"issuerName"`

	// TODO ??
	// Additional templated Common Names
	// Duration              string
	// ApplyInternalDNSNames bool
}

// Supports secret references, configmap references, and raw values.
type DirectListenerTLS struct {
	Key        corev1.EnvVar  `json:"key"`
	Cert       corev1.EnvVar  `json:"cert"`
	TrustStore *corev1.EnvVar `json:"trustStore,omitempty"`
	Client     *corev1.EnvVar `json:"client,omitempty"`
}

// ValueSource is a generic "value" type that permits sourcing the actual
// (runtime) value from a variety of sources.
// In most cases, this should always output a CEL expression that's resolved at runtime.
// Example use cases are:
// - ClusterConfig e.g. Secret values
// - NodeConfig e.g. Dynamic advertised_host
// - RPKConfig e.g. Runtime resolved listing of broker addresses via SRV records.
type ValueSource struct {
	Value           string                       `json:"value,omitempty"`
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef,omitempty"`
	Expr            Expr                         `json:"expr,omitempty"`
}

// CEL Expr for more complex values
// Examples:
// - rack awareness: Expr(node_annotation('k8s.io/failure-domain')),
// - addresses: Expr(srv_addres('tcp', 'admin', 'redpanda.redpanda.cluster.svc.cluster.local'))
type Expr string
