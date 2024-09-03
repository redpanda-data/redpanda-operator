package v1alpha2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/twmb/franz-go/pkg/kadm"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrUnsupportedSASLMechanism = errors.New("unsupported SASL mechanism")

// KafkaAPISpec configures client configuration settings for connecting to Redpanda brokers.
type KafkaAPISpec struct {
	// Specifies a list of broker addresses in the format <host>:<port>
	Brokers []string `json:"brokers"`
	// Defines TLS configuration settings for Redpanda clusters that have TLS enabled.
	// +optional
	TLS *CommonTLS `json:"tls,omitempty"`
	// Defines authentication configuration settings for Redpanda clusters that have authentication enabled.
	// +optional
	SASL *KafkaSASL `json:"sasl,omitempty"`
}

// KafkaSASL configures credentials to connect to Redpanda cluster that has authentication enabled.
type KafkaSASL struct {
	// Specifies the username.
	// +optional
	Username string `json:"username,omitempty"`
	// Specifies the password.
	// +optional
	Password SecretKeyRef `json:"passwordSecretRef,omitempty"`
	// Specifies the SASL/SCRAM authentication mechanism.
	Mechanism SASLMechanism `json:"mechanism"`
	// +optional
	OAUth KafkaSASLOAuthBearer `json:"oauth,omitempty"`
	// +optional
	GSSAPIConfig KafkaSASLGSSAPI `json:"gssapi,omitempty"`
	// +optional
	AWSMskIam KafkaSASLAWSMskIam `json:"awsMskIam,omitempty"`
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

// Equals determines if the given SASLMechanism is equal to this
// mechanism, it does so in a case-insensitive way.
func (s SASLMechanism) Equals(other SASLMechanism) bool {
	return strings.EqualFold(string(s), string(other))
}

func (s *SASLMechanism) ScramToKafka() (kadm.ScramMechanism, error) {
	if s == nil {
		return 0, ErrUnsupportedSASLMechanism
	}

	switch {
	case s.Equals(SASLMechanismScramSHA256):
		return kadm.ScramSha256, nil
	case s.Equals(SASLMechanismScramSHA512):
		return kadm.ScramSha512, nil
	}

	return 0, ErrUnsupportedSASLMechanism
}

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

	// EnableFAST enables FAST, which is a pre-authentication framework for Kerberos.
	// It includes a mechanism for tunneling pre-authentication exchanges using armored KDC messages.
	// FAST provides increased resistance to passive password guessing attacks.
	EnableFast bool `json:"enableFast"`
}

// KafkaSASLAWSMskIam is the config for AWS IAM SASL mechanism,
// see: https://docs.aws.amazon.com/msk/latest/developerguide/iam-access-control.html
type KafkaSASLAWSMskIam struct {
	AccessKey string       `json:"accessKey"`
	SecretKey SecretKeyRef `json:"secretKeySecretRef"`

	// SessionToken, if non-empty, is a session / security token to use for authentication.
	// See: https://docs.aws.amazon.com/STS/latest/APIReference/welcome.html
	SessionToken SecretKeyRef `json:"sessionTokenSecretRef"`

	// UserAgent is the user agent to for the client to use when connecting
	// to Kafka, overriding the default "franz-go/<runtime.Version()>/<hostname>".
	//
	// Setting a UserAgent allows authorizing based on the aws:UserAgent
	// condition key; see the following link for more details:
	// https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_condition-keys.html#condition-keys-useragent
	UserAgent string `json:"userAgent"`
}

// CommonTLS specifies TLS configuration settings for Redpanda clusters that have authentication enabled.
type CommonTLS struct {
	// CaCert is the reference for certificate authority used to establish TLS connection to Redpanda
	CaCert *SecretKeyRef `json:"caCertSecretRef,omitempty"`
	// Cert is the reference for client public certificate to establish mTLS connection to Redpanda
	Cert *SecretKeyRef `json:"certSecretRef,omitempty"`
	// Key is the reference for client private certificate to establish mTLS connection to Redpanda
	Key *SecretKeyRef `json:"keySecretRef,omitempty"`
	// InsecureSkipTLSVerify can skip verifying Redpanda self-signed certificate when establish TLS connection to Redpanda
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTlsVerify"`
}

// SecretKeyRef contains enough information to inspect or modify the referred Secret data
// See https://pkg.go.dev/k8s.io/api/core/v1#ObjectReference.
type SecretKeyRef struct {
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name"`

	// +optional
	// Key in Secret data to get value from
	Key string `json:"key,omitempty"`
}

// GetValue retrieves the value from `corev1.Secret{}`.
func (s *SecretKeyRef) GetValue(
	ctx context.Context, cl client.Client, namespace, defaultKey string,
) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: s.Name}, secret); err != nil {
		return nil, fmt.Errorf("getting Secret %s/%s: %w", namespace, s.Name, err)
	}

	b, err := s.getValue(secret, namespace, defaultKey)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (s *SecretKeyRef) getValue(
	secret *corev1.Secret, namespace, defaultKey string,
) ([]byte, error) {
	key := s.Key
	if key == "" {
		key = defaultKey
	}

	value, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("getting value from Secret %s/%s: key %s not found", namespace, s.Name, key) //nolint:goerr113 // no need to declare new error type
	}
	return value, nil
}

// AdminAPISpec defines client configuration for connecting to Redpanda's admin API.
type AdminAPISpec struct {
	// Specifies a list of broker addresses in the format <host>:<port>
	URLs []string `json:"urls"`
	// Defines TLS configuration settings for Redpanda clusters that have TLS enabled.
	// +optional
	TLS *CommonTLS `json:"tls,omitempty"`
	// Defines authentication configuration settings for Redpanda clusters that have authentication enabled.
	// +optional
	SASL *AdminSASL `json:"sasl,omitempty"`
}

// AdminSASL configures credentials to connect to Redpanda cluster that has authentication enabled.
type AdminSASL struct {
	// Specifies the username.
	// +optional
	Username string `json:"username,omitempty"`
	// Specifies the password.
	// +optional
	Password SecretKeyRef `json:"passwordSecretRef,omitempty"`
	// Specifies the SASL/SCRAM authentication mechanism.
	Mechanism SASLMechanism `json:"mechanism"`
	// +optional
	AuthToken SecretKeyRef `json:"token,omitempty"`
}

// ClusterRef represents a reference to a cluster that is being targeted.
type ClusterRef struct {
	// Name specifies the name of the cluster being referenced.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// ResourceTemplate specifies additional configuration for a resource.
type ResourceTemplate struct {
	// Metadata specifies additional metadata to associate with a resource.
	Metadata MetadataTemplate `json:"metadata"`
}

// MetadataTemplate defines additional metadata to associate with a resource.
type MetadataTemplate struct {
	// Labels specifies the Kubernetes labels to apply to a managed resource.
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations specifies the Kubernetes annotations to apply to a managed resource.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// StaticConfigurationSource configures connections to a Redpanda cluster via hard-coded
// connection strings and manually configured TLS and authentication parameters.
type StaticConfigurationSource struct {
	// Kafka is the configuration information for communicating with the Kafka
	// API of a Redpanda cluster where the object should be created.
	// +required
	Kafka *KafkaAPISpec `json:"kafka"`
	// AdminAPISpec is the configuration information for communicating with the Admin
	// API of a Redpanda cluster where the object should be created.
	// +required
	Admin *AdminAPISpec `json:"admin"`
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

func (c *ClusterSource) GetKafkaAPISpec() *KafkaAPISpec {
	if c.StaticConfiguration != nil {
		return c.StaticConfiguration.Kafka
	}
	return nil
}

func (c *ClusterSource) GetAdminAPISpec() *AdminAPISpec {
	if c.StaticConfiguration != nil {
		return c.StaticConfiguration.Admin
	}
	return nil
}

func (c *ClusterSource) GetClusterRef() *ClusterRef {
	return c.ClusterRef
}
