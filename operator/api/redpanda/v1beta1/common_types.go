package v1beta1

import (
	"github.com/redpanda-data/console/backend/pkg/config"
	corev1 "k8s.io/api/core/v1"
)

// ClusterRef represents a reference to a cluster that is being targeted.
type ClusterRef struct {
	// Name specifies the name of the cluster being referenced.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

type KafkaAPISpec struct {
	Brokers []string   `json:"brokers"`
	TLS     *CommonTLS `json:"tls,omitempty"`
	SASL    *KafkaSASL `json:"sasl,omitempty"`
}

type KafkaSASL struct {
	Mechanism    SASLMechanism        `json:"mechanism"`
	Username     string               `json:"username,omitempty"`
	Password     SecretKeyRef         `json:"passwordSecretRef,omitempty"`
	OAUth        KafkaSASLOAuthBearer `json:"oauth,omitempty"`
	GSSAPIConfig KafkaSASLGSSAPI      `json:"gssapi,omitempty"`
	AWSMskIam    KafkaSASLAWSMSKIAM   `json:"awsMskIam,omitempty"`
}

// SASLMechanism specifies a SASL auth mechanism.
type SASLMechanism string

const (
	SASLMechanismPlain                  SASLMechanism = config.SASLMechanismPlain
	SASLMechanismScramSHA256            SASLMechanism = config.SASLMechanismScramSHA256
	SASLMechanismScramSHA512            SASLMechanism = config.SASLMechanismScramSHA512
	SASLMechanismGSSAPI                 SASLMechanism = config.SASLMechanismGSSAPI
	SASLMechanismOAuthBearer            SASLMechanism = config.SASLMechanismOAuthBearer
	SASLMechanismAWSManagedStreamingIAM SASLMechanism = config.SASLMechanismAWSManagedStreamingIAM
)

// KafkaSASLOAuthBearer is the config struct for the SASL OAuthBearer mechanism
type KafkaSASLOAuthBearer struct {
	Token SecretKeyRef `json:"tokenSecretRef"`
}

// KafkaSASLGSSAPI represents the Kafka Kerberos config.
type KafkaSASLGSSAPI struct {
	AuthType           string       `json:"authType"`
	KeyTabPath         string       `json:"keyTabPath"`
	KerberosConfigPath string       `json:"kerberosConfigPath"`
	ServiceName        string       `json:"serviceName"`
	Username           string       `json:"username"`
	Password           SecretKeyRef `json:"passwordSecretRef"`
	Realm              string       `json:"realm"`
	EnableFAST         bool         `json:"enableFast"`
}

type KafkaSASLAWSMSKIAM struct {
	AccessKey    string       `json:"accessKey"`
	SecretKey    SecretKeyRef `json:"secretKeySecretRef"`
	SessionToken SecretKeyRef `json:"sessionTokenSecretRef"`
	UserAgent    string       `json:"userAgent"`
}

type CommonTLS struct {
	CACert                *SecretKeyRef `json:"caCertSecretRef,omitempty"`
	Cert                  *SecretKeyRef `json:"certSecretRef,omitempty"`
	Key                   *SecretKeyRef `json:"keySecretRef,omitempty"`
	InsecureSkipTLSVerify bool          `json:"insecureSkipTlsVerify"`
}

type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

type AdminAPISpec struct {
	URLs []string   `json:"urls"`
	TLS  *CommonTLS `json:"tls,omitempty"`
	SASL *AdminSASL `json:"sasl,omitempty"`
}

type AdminSASL struct {
	Username  string        `json:"username,omitempty"`
	Password  SecretKeyRef  `json:"passwordSecretRef,omitempty"`
	Mechanism SASLMechanism `json:"mechanism"`
	AuthToken SecretKeyRef  `json:"token,omitempty"`
}

type SchemaRegistrySpec struct {
	URLs []string            `json:"urls"`
	TLS  *CommonTLS          `json:"tls,omitempty"`
	SASL *SchemaRegistrySASL `json:"sasl,omitempty"`
}

type SchemaRegistrySASL struct {
	Username  string        `json:"username,omitempty"`
	Password  SecretKeyRef  `json:"passwordSecretRef,omitempty"`
	Mechanism SASLMechanism `json:"mechanism"`
	AuthToken SecretKeyRef  `json:"token,omitempty"`
}

// StaticConfigurationSource configures connections to a Redpanda cluster via hard-coded
// connection strings and manually configured TLS and authentication parameters.
type StaticConfigurationSource struct {
	// Kafka is the configuration information for communicating with the Kafka
	// API of a Redpanda cluster where the object should be created.
	Kafka *KafkaAPISpec `json:"kafka,omitempty"`
	// AdminAPISpec is the configuration information for communicating with the Admin
	// API of a Redpanda cluster where the object should be created.
	Admin *AdminAPISpec `json:"admin,omitempty"`
	// SchemaRegistry is the configuration information for communicating with the Schema Registry
	// API of a Redpanda cluster where the object should be created.
	SchemaRegistry *SchemaRegistrySpec `json:"schemaRegistry,omitempty"`
}

// ClusterSource defines how to connect to a particular Redpanda cluster.
// +kubebuilder:validation:XValidation:message="either clusterRef or staticConfiguration must be set",rule="has(self.clusterRef) || has(self.staticConfiguration)"
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ClusterSource is immutable"
type ClusterSource struct {
	// ClusterRef is a reference to the cluster where the object should be created.
	// It is used in constructing the client created to configure a cluster.
	// This takes precedence over StaticConfigurationSource.
	ClusterRef *ClusterRef `json:"clusterRef,omitempty"`
	// StaticConfiguration holds connection parameters to Kafka and Admin APIs.
	StaticConfiguration *StaticConfigurationSource `json:"staticConfiguration,omitempty"`
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
// - addresses: Expr(srv_address('tcp', 'admin', 'redpanda.redpanda.cluster.svc.cluster.local'))
type Expr string
