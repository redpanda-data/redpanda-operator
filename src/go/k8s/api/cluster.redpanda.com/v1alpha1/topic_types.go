// Copyright 2021-2023 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"

	krbclient "github.com/jcmturner/gokrb5/v8/client"
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// TopicSpec defines the desired state of the topic. See https://docs.redpanda.com/current/manage/kubernetes/manage-topics/.
type TopicSpec struct {
	// Specifies the number of topic shards that are distributed across the brokers in a cluster.
	// This number cannot be decreased after topic creation.
	// It can be increased after topic creation, but it is
	// important to understand the consequences that has, especially for
	// topics with semantic partitioning. When absent this will default to
	// the Redpanda cluster configuration `default_topic_partitions`.
	// See https://docs.redpanda.com/docs/reference/cluster-properties/#default_topic_partitions and
	// https://docs.redpanda.com/docs/get-started/architecture/#partitions
	Partitions *int `json:"partitions,omitempty"`
	// Specifies the number of replicas the topic should have. Must be odd value.
	// When absent this will default to the Redpanda cluster configuration `default_topic_replications`.
	// See https://docs.redpanda.com/docs/reference/cluster-properties/#default_topic_replications.
	ReplicationFactor *int `json:"replicationFactor,omitempty"`
	// Changes the topic name from the value of `metadata.name`.
	OverwriteTopicName *string `json:"overwriteTopicName,omitempty"`
	// Adds extra topic configurations. This is a free-form map of any configuration options that topics can have.
	// Examples:
	// `cleanup.policy=compact`
	// `redpanda.remote.write=true`
	// `redpanda.remote.read=true`
	// `redpanda.remote.recovery=true`
	// `redpanda.remote.delete=true`
	AdditionalConfig map[string]*string `json:"additionalConfig,omitempty"`

	// Defines client configuration for connecting to Redpanda brokers.
	KafkaAPISpec *KafkaAPISpec `json:"kafkaApiSpec,omitempty"`

	// Overwrites the fully-qualified
	// name of the metric. This should be easier to identify if
	// multiple operator instances runs inside the same Kubernetes cluster.
	// By default, it is set to `redpanda-operator`.
	MetricsNamespace *string `json:"metricsNamespace,omitempty"`

	// Defines when the topic controller will schedule the next reconciliation.
	// Default is 3 seconds.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=duration
	// +kubebuilder:default="3s"
	SynchronizationInterval *metav1.Duration `json:"interval,omitempty"`
}

// KafkaAPISpec configures client configuration settings for connecting to Redpanda brokers.
type KafkaAPISpec struct {
	// Specifies a list of broker addresses in the format <host>:<port>
	Brokers []string `json:"brokers"`
	// Defines TLS configuration settings for Redpanda clusters that have TLS enabled.
	// +optional
	TLS *KafkaTLS `json:"tls,omitempty"`
	// Defines authentication configuration settings for Redpanda clusters that have authentication enabled.
	// +optional
	SASL *KafkaSASL `json:"sasl,omitempty"`
}

func (k *KafkaAPISpec) ConfigureTLS(ctx context.Context, namespace string, cl client.Client, opts []kgo.Opt, log logr.Logger) ([]kgo.Opt, error) {
	var caCertPool *x509.CertPool

	// Root CA
	if k.TLS.CaCert != nil {
		ca, err := k.TLS.CaCert.GetValue(ctx, cl, namespace, "ca.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read ca certificate secret: %w", err)
		}

		caCertPool = x509.NewCertPool()
		isSuccessful := caCertPool.AppendCertsFromPEM(ca)
		if !isSuccessful {
			log.Info("failed to append ca file to cert pool, is this a valid PEM format?")
		}
	}

	// If configured load TLS cert & key - Mutual TLS
	var certificates []tls.Certificate
	if k.TLS.Cert != nil && k.TLS.Key != nil {
		// 1. Read certificates
		cert, err := k.TLS.Cert.GetValue(ctx, cl, namespace, "tls.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read certificate secret: %w", err)
		}

		certData := cert

		key, err := k.TLS.Cert.GetValue(ctx, cl, namespace, "tls.key")
		if err != nil {
			return nil, fmt.Errorf("failed to read key certificate secret: %w", err)
		}

		keyData := key

		// 2. Check if private key needs to be decrypted. Decrypt it if passphrase is given, otherwise return error
		pemBlock, _ := pem.Decode(keyData)
		if pemBlock == nil {
			return nil, fmt.Errorf("no valid private key found") // nolint:goerr113 // this error will not be handled by operator
		}

		tlsCert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, fmt.Errorf("cannot parse pem: %w", err)
		}
		certificates = []tls.Certificate{tlsCert}
	}

	tlsDialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
		Config: &tls.Config{
			//nolint:gosec // InsecureSkipVerify may be true upon user's responsibility.
			InsecureSkipVerify: k.TLS.InsecureSkipTLSVerify,
			Certificates:       certificates,
			RootCAs:            caCertPool,
		},
	}

	return append(opts, kgo.Dialer(tlsDialer.DialContext)), nil
}

func (k *KafkaAPISpec) ConfigureSASL(ctx context.Context, namespace string, cl client.Client, opts []kgo.Opt, log logr.Logger) ([]kgo.Opt, error) {
	// SASL Plain
	if k.SASL.Mechanism == config.SASLMechanismPlain {
		p, err := k.SASL.Password.GetValue(ctx, cl, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl plain password: %w", err)
		}
		mechanism := plain.Auth{
			User: k.SASL.Username,
			Pass: string(p),
		}.AsMechanism()
		opts = append(opts, kgo.SASL(mechanism))
	}

	// SASL SCRAM
	if k.SASL.Mechanism == config.SASLMechanismScramSHA256 ||
		k.SASL.Mechanism == config.SASLMechanismScramSHA512 {
		p, err := k.SASL.Password.GetValue(ctx, cl, namespace, "password")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch sasl scram password: %w", err)
		}
		var mechanism sasl.Mechanism
		scramAuth := scram.Auth{
			User: k.SASL.Username,
			Pass: string(p),
		}
		if k.SASL.Mechanism == config.SASLMechanismScramSHA256 {
			log.V(2).Info("configuring SCRAM-SHA-256 mechanism")
			mechanism = scramAuth.AsSha256Mechanism()
		}
		if k.SASL.Mechanism == config.SASLMechanismScramSHA512 {
			log.V(2).Info("configuring SCRAM-SHA-512 mechanism")
			mechanism = scramAuth.AsSha512Mechanism()
		}
		opts = append(opts, kgo.SASL(mechanism))
	}

	// OAuth Bearer
	if k.SASL.Mechanism == config.SASLMechanismOAuthBearer {
		t, err := k.SASL.OAUth.Token.GetValue(ctx, cl, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch token: %w", err)
		}
		mechanism := oauth.Auth{
			Token: string(t),
		}.AsMechanism()
		opts = append(opts, kgo.SASL(mechanism))
	}

	// Kerberos
	if k.SASL.Mechanism == config.SASLMechanismGSSAPI {
		log.V(2).Info("configuring SCRAM-SHA-512 mechanism")
		var krbClient *krbclient.Client

		kerbCfg, err := krbconfig.Load(k.SASL.GSSAPIConfig.KerberosConfigPath)
		if err != nil {
			return nil, fmt.Errorf("creating kerberos config from specified config (%s) filepath: %w", k.SASL.GSSAPIConfig.KerberosConfigPath, err)
		}
		switch k.SASL.GSSAPIConfig.AuthType {
		case "USER_AUTH":
			p, err := k.SASL.GSSAPIConfig.Password.GetValue(ctx, cl, namespace, "password")
			if err != nil {
				return nil, fmt.Errorf("unable to fetch sasl gssapi password: %w", err)
			}
			krbClient = krbclient.NewWithPassword(
				k.SASL.GSSAPIConfig.Username,
				k.SASL.GSSAPIConfig.Realm,
				string(p),
				kerbCfg,
				krbclient.DisablePAFXFAST(!k.SASL.GSSAPIConfig.EnableFast))
		case "KEYTAB_AUTH":
			ktb, err := keytab.Load(k.SASL.GSSAPIConfig.KeyTabPath)
			if err != nil {
				return nil, fmt.Errorf("loading keytab from (%s) key tab path: %w", k.SASL.GSSAPIConfig.KeyTabPath, err)
			}
			krbClient = krbclient.NewWithKeytab(
				k.SASL.GSSAPIConfig.Username,
				k.SASL.GSSAPIConfig.Realm,
				ktb,
				kerbCfg,
				krbclient.DisablePAFXFAST(!k.SASL.GSSAPIConfig.EnableFast))
		}
		kerberosMechanism := kerberos.Auth{
			Client:           krbClient,
			Service:          k.SASL.GSSAPIConfig.ServiceName,
			PersistAfterAuth: true,
		}.AsMechanism()
		opts = append(opts, kgo.SASL(kerberosMechanism))
	}

	// AWS MSK IAM
	if k.SASL.Mechanism == config.SASLMechanismAWSManagedStreamingIAM {
		s, err := k.SASL.AWSMskIam.SecretKey.GetValue(ctx, cl, namespace, "secret")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}
		t, err := k.SASL.AWSMskIam.SessionToken.GetValue(ctx, cl, namespace, "token")
		if err != nil {
			return nil, fmt.Errorf("unable to fetch aws msk secret key: %w", err)
		}
		mechanism := aws.Auth{
			AccessKey:    k.SASL.AWSMskIam.AccessKey,
			SecretKey:    string(s),
			SessionToken: string(t),
			UserAgent:    k.SASL.AWSMskIam.UserAgent,
		}.AsManagedStreamingIAMMechanism()
		opts = append(opts, kgo.SASL(mechanism))
	}
	return opts, nil
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

// KafkaTLS specifies TLS configuration settings for Redpanda clusters that have authentication enabled.
type KafkaTLS struct {
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

// TopicStatus defines the observed state of the Topic resource.
type TopicStatus struct {
	// ObservedGeneration is the last observed generation of the Topic.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the Topic.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TopicConfiguration is the last snapshot of the topic configuration during successful reconciliation.
	TopicConfiguration []Configuration `json:"topicConfiguration,omitempty"`
}

// Configuration was copied from https://github.com/twmb/franz-go/blob/01651affd204d4a3577a341e748c5d09b52587f8/pkg/kmsg/generated.go#L24593-L24634
type Configuration struct {
	// Name is a key this entry corresponds to (e.g. segment.bytes).
	Name string `json:"name"`

	// Value is the value for this config key. If the key is sensitive,
	// the value will be null.
	Value *string `json:"value,omitempty"`

	// ReadOnly signifies whether this is not a dynamic config option.
	//
	// Note that this field is not always correct, and you may need to check
	// whether the Source is any dynamic enum. See franz-go#91 for more details.
	ReadOnly bool `json:"readOnly"`

	// IsDefault is whether this is a default config option. This has been
	// replaced in favor of Source.
	IsDefault bool `json:"isDefault"`

	// Source is where this config entry is from.
	//
	// This field has a default of -1.
	Source string `json:"source"`

	// IsSensitive signifies whether this is a sensitive config key, which
	// is either a password or an unknown type.
	IsSensitive bool `json:"isSensitive"`

	// ConfigSynonyms contains fallback key/value pairs for this config
	// entry, in order of preference. That is, if a config entry is both
	// dynamically configured and has a default, the top level return will be
	// the dynamic configuration, while its "synonym" will be the default.
	ConfigSynonyms []ConfigSynonyms `json:"configSynonyms,omitempty"`

	// ConfigType specifies the configuration data type.
	ConfigType string `json:"configType"`

	// Documentation is optional documentation for the config entry.
	Documentation *string `json:"documentation,omitempty"`

	// UnknownTags are tags Kafka sent that we do not know the purpose of.
	UnknownTags map[string]string `json:"unknownTags"`
}

// ConfigSynonyms was copied from https://github.com/twmb/franz-go/blob/01651affd204d4a3577a341e748c5d09b52587f8/pkg/kmsg/generated.go#L24569-L24578
type ConfigSynonyms struct {
	Name string `json:"name"`

	Value *string `json:"value,omitempty"`

	Source string `json:"source"`

	// UnknownTags are tags Kafka sent that we do not know the purpose of.
	UnknownTags map[string]string `json:"unknownTags,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Topic defines the CRD for Topic resources. See https://docs.redpanda.com/current/manage/kubernetes/manage-topics/.
type Topic struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Defines the desired state of the Topic resource.
	Spec TopicSpec `json:"spec,omitempty"`
	// Represents the current status of the Topic resource.
	Status TopicStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TopicList contains a list of Topic objects.
type TopicList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Topic resources.
	Items []Topic `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Topic{}, &TopicList{})
}

func (t *Topic) GetTopicName() string {
	topicName := t.Name
	if t.Spec.OverwriteTopicName != nil && *t.Spec.OverwriteTopicName != "" {
		topicName = *t.Spec.OverwriteTopicName
	}
	return topicName
}

const (
	// ReadyCondition indicates the resource is ready and fully reconciled.
	// If the Condition is False, the resource SHOULD be considered to be in the process of reconciling and not a
	// representation of actual state.
	ReadyCondition = "Ready"
)

const (
	// ProgressingReason indicates a condition or event observed progression, for example when the reconciliation of a
	// resource or an action has started.
	//
	// When this reason is given, other conditions and types MAY no longer be considered as an up-to-date observation.
	// Producers of the specific condition type or event SHOULD provide more information about the expectations and
	// precise meaning in their API specification.
	//
	// More information about the reason or the current state of the progression MAY be available as additional metadata
	// in an attached message.
	ProgressingReason string = "Progressing"

	// SucceededReason indicates a condition or event observed a success, for example when declared desired state
	// matches actual state, or a performed action succeeded.
	//
	// More information about the reason of success MAY be available as additional metadata in an attached message.
	SucceededReason string = "Succeeded"

	// FailedReason indicates a condition or event observed a failure, for example when declared state does not match
	// actual state, or a performed action failed.
	//
	// More information about the reason of failure MAY be available as additional metadata in an attached message.
	FailedReason string = "Failed"
)

// TopicProgressing resets any failures and registers progress toward
// reconciling the given Topic by setting the meta.ReadyCondition to
// 'Unknown' for meta.ProgressingReason.
func TopicProgressing(topic *Topic) *Topic {
	return setCondition(ProgressingReason, "Topic reconciliation in progress", metav1.ConditionUnknown, topic)
}

// TopicReady resets any failures and registers ready condition
// the given Topic by setting the meta.ReadyCondition to
// 'Ready' for meta.SucceededReason.
func TopicReady(topic *Topic) *Topic {
	return setCondition(SucceededReason, "Topic reconciliation succeeded", metav1.ConditionTrue, topic)
}

// TopicFailed resets all conditions to failure the given Topic
// by setting the meta.ReadyCondition to 'Failed' for meta.FailedReason.
func TopicFailed(topic *Topic) *Topic {
	return setCondition(FailedReason, "Topic reconciliation failed", metav1.ConditionFalse, topic)
}

func setCondition(reason, message string, status metav1.ConditionStatus, topic *Topic) *Topic {
	condition := metav1.Condition{
		Type:               ReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: topic.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	for i := range topic.Status.Conditions {
		if topic.Status.Conditions[i].Type == ReadyCondition {
			if topic.Status.Conditions[i].Status == status &&
				topic.Status.Conditions[i].Reason == reason {
				return topic
			}
			topic.Status.Conditions[i] = condition
			return topic
		}
	}

	topic.Status.Conditions = append(topic.Status.Conditions, condition)
	return topic
}
