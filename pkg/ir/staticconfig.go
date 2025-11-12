// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package ir

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"

	"github.com/cockroachdb/errors"
	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbconfig "github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/kerberos"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

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

type KafkaAPIConfiguration struct {
	Brokers []string
	TLS     *TLSConfig
	SASL    *AuthUser
}

func (k *KafkaAPISpec) Load(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (*KafkaAPIConfiguration, error) {
	config := &KafkaAPIConfiguration{
		Brokers: k.Brokers,
	}
	if k.TLS != nil {
		tls, err := k.TLS.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}
		config.TLS = tls
	}
	if k.SASL != nil {
		sasl, err := k.SASL.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}
		config.SASL = sasl
	}
	return config, nil
}

// KafkaSASL configures credentials to connect to Redpanda cluster that has authentication enabled.
type KafkaSASL struct {
	// Specifies the username.
	// +optional
	Username string `json:"username,omitempty"`
	// Specifies the password.
	// +optional
	Password *ValueSource `json:"passwordSecretRef,omitempty"`
	// Specifies the SASL/SCRAM authentication mechanism.
	Mechanism SASLMechanism `json:"mechanism"`
	// +optional
	OAUth *KafkaSASLOAuthBearer `json:"oauth,omitempty"`
	// +optional
	GSSAPIConfig *KafkaSASLGSSAPI `json:"gssapi,omitempty"`
	// +optional
	AWSMskIam *KafkaSASLAWSMskIam `json:"awsMskIam,omitempty"`
}

func (k *KafkaSASL) AsOption(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (kgo.Opt, error) {
	switch k.Mechanism {
	// SASL Plain
	case config.SASLMechanismPlain:
		p, err := k.Password.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}

		return kgo.SASL(plain.Auth{
			User: k.Username,
			Pass: p,
		}.AsMechanism()), nil

	// SASL SCRAM
	case config.SASLMechanismScramSHA256, config.SASLMechanismScramSHA512:
		p, err := k.Password.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}

		var mechanism sasl.Mechanism
		scramAuth := scram.Auth{
			User: k.Username,
			Pass: p,
		}

		if k.Mechanism == config.SASLMechanismScramSHA256 {
			mechanism = scramAuth.AsSha256Mechanism()
		}

		if k.Mechanism == config.SASLMechanismScramSHA512 {
			mechanism = scramAuth.AsSha512Mechanism()
		}

		return kgo.SASL(mechanism), nil

	// OAuth Bearer
	case config.SASLMechanismOAuthBearer:
		t, err := k.OAUth.Token.Load(ctx, client, expander)
		if err != nil {
			return nil, errors.Newf("unable to fetch token: %w", err)
		}

		return kgo.SASL(oauth.Auth{
			Token: t,
		}.AsMechanism()), nil

	// Kerberos
	case config.SASLMechanismGSSAPI:
		var krbClient *krbclient.Client

		kerbCfg, err := krbconfig.Load(k.GSSAPIConfig.KerberosConfigPath)
		if err != nil {
			return nil, errors.Newf("creating kerberos config from specified config (%s) filepath: %w", k.GSSAPIConfig.KerberosConfigPath, err)
		}

		switch k.GSSAPIConfig.AuthType {
		case "USER_AUTH":
			p, err := k.GSSAPIConfig.Password.Load(ctx, client, expander)
			if err != nil {
				return nil, errors.Newf("unable to fetch sasl gssapi password: %w", err)
			}

			krbClient = krbclient.NewWithPassword(
				k.GSSAPIConfig.Username,
				k.GSSAPIConfig.Realm,
				p,
				kerbCfg,
				krbclient.DisablePAFXFAST(!k.GSSAPIConfig.EnableFast),
			)

		case "KEYTAB_AUTH":
			ktb, err := keytab.Load(k.GSSAPIConfig.KeyTabPath)
			if err != nil {
				return nil, errors.Newf("loading keytab from (%s) key tab path: %w", k.GSSAPIConfig.KeyTabPath, err)
			}

			krbClient = krbclient.NewWithKeytab(
				k.GSSAPIConfig.Username,
				k.GSSAPIConfig.Realm,
				ktb,
				kerbCfg,
				krbclient.DisablePAFXFAST(!k.GSSAPIConfig.EnableFast),
			)
		}

		return kgo.SASL(kerberos.Auth{
			Client:           krbClient,
			Service:          k.GSSAPIConfig.ServiceName,
			PersistAfterAuth: true,
		}.AsMechanism()), nil

	// AWS MSK IAM
	case config.SASLMechanismAWSManagedStreamingIAM:
		key, err := k.AWSMskIam.SecretKey.Load(ctx, client, expander)
		if err != nil {
			return nil, errors.Newf("unable to fetch aws msk secret key: %w", err)
		}

		t, err := k.AWSMskIam.SessionToken.Load(ctx, client, expander)
		if err != nil {
			return nil, errors.Newf("unable to fetch aws msk secret key: %w", err)
		}

		return kgo.SASL(aws.Auth{
			AccessKey:    k.AWSMskIam.AccessKey,
			SecretKey:    key,
			SessionToken: t,
			UserAgent:    k.AWSMskIam.UserAgent,
		}.AsManagedStreamingIAMMechanism()), nil
	}

	return nil, errors.Newf("unsupported sasl mechanism: %s", k.Mechanism)
}

type AuthUser struct {
	Username  string
	Password  string
	Mechanism string
}

func (k *KafkaSASL) Load(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (*AuthUser, error) {
	password, err := k.Password.Load(ctx, client, expander)
	if err != nil {
		return nil, err
	}
	return &AuthUser{
		Username:  k.Username,
		Password:  password,
		Mechanism: string(k.Mechanism),
	}, nil
}

// SASLMechanism specifies a SASL auth mechanism.
type SASLMechanism string

// KafkaSASLOAuthBearer is the config struct for the SASL OAuthBearer mechanism
type KafkaSASLOAuthBearer struct {
	Token *ValueSource `json:"token"`
}

// KafkaSASLGSSAPI represents the Kafka Kerberos config.
type KafkaSASLGSSAPI struct {
	AuthType           string       `json:"authType"`
	KeyTabPath         string       `json:"keyTabPath"`
	KerberosConfigPath string       `json:"kerberosConfigPath"`
	ServiceName        string       `json:"serviceName"`
	Username           string       `json:"username"`
	Password           *ValueSource `json:"password"`
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
	SecretKey *ValueSource `json:"secretKey"`

	// SessionToken, if non-empty, is a session / security token to use for authentication.
	// See: https://docs.aws.amazon.com/STS/latest/APIReference/welcome.html
	SessionToken *ValueSource `json:"sessionToken"`

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
	CaCert *ValueSource `json:"caCert,omitempty"`
	// Cert is the reference for client public certificate to establish mTLS connection to Redpanda
	Cert *ValueSource `json:"cert,omitempty"`
	// Key is the reference for client private certificate to establish mTLS connection to Redpanda
	Key *ValueSource `json:"key,omitempty"`
	// InsecureSkipTLSVerify can skip verifying Redpanda self-signed certificate when establish TLS connection to Redpanda
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTlsVerify,omitempty"`
}

type ValueSource struct {
	// Namespace of where the value comes from used in resolving kubernetes objects.
	Namespace string `json:"namespace,omitempty"`
	// Inline is the raw value specified inline.
	Inline *string `json:"inline,omitempty"`
	// If the value is supplied by a kubernetes object reference, coordinates are embedded here.
	// For target values, the string value fetched from the source will be treated as
	// a raw string.
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Should the value be contained in a k8s secret rather than configmap, we can refer
	// to it here.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// If the value is supplied by an external source, coordinates are embedded here.
	// Note: we interpret all fetched external secrets as raw string values
	ExternalSecretRefSelector *ExternalSecretKeySelector `json:"externalSecretRef,omitempty"`
}

func (v *ValueSource) Load(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (string, error) {
	if v.Inline != nil {
		return *v.Inline, nil
	}
	if v.ConfigMapKeyRef != nil {
		return loadConfigMap(ctx, client, v.ConfigMapKeyRef.Name, v.Namespace, v.ConfigMapKeyRef.Key)
	}
	if v.SecretKeyRef != nil {
		return loadSecret(ctx, client, v.SecretKeyRef.Name, v.Namespace, v.SecretKeyRef.Key)
	}
	if v.ExternalSecretRefSelector != nil {
		if expander == nil {
			return "", errors.New("attempted to expand an external secret without enabling external secrets in the operator")
		}
		return expander.Expand(ctx, v.ExternalSecretRefSelector.Name)
	}
	return "", errors.New("called Load on an unset ValueSource")
}

// ExternalSecretKeySelector selects a key of an external Secret.
type ExternalSecretKeySelector struct {
	Name string `json:"name"`
}

// Config returns the materialized tls.Config for the CommonTLS object
func (c *CommonTLS) Config(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (*tls.Config, error) {
	config, err := c.Load(ctx, client, expander)
	if err != nil {
		return nil, err
	}

	var caCertPool *x509.CertPool
	if config.CA != "" {
		caData := []byte(config.CA)
		pemBlock, _ := pem.Decode(caData)
		if pemBlock == nil {
			return nil, errors.New("no valid ca found")
		}

		caCertPool = x509.NewCertPool()
		isSuccessful := caCertPool.AppendCertsFromPEM(caData)
		if !isSuccessful {
			return nil, errors.New("failed to append ca file to cert pool, is this a valid PEM format?")
		}
	}

	var certificates []tls.Certificate
	if config.Cert != "" && config.Key != "" {
		certData := []byte(config.Cert)
		pemBlock, _ := pem.Decode(certData)
		if pemBlock == nil {
			return nil, errors.New("no valid cert found")
		}

		keyData := []byte(config.Key)
		pemBlock, _ = pem.Decode(keyData)
		if pemBlock == nil {
			return nil, errors.New("no valid private key found")
		}

		tlsCert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, errors.Newf("cannot parse pem: %w", err)
		}
		certificates = []tls.Certificate{tlsCert}
	}
	return &tls.Config{
		//nolint:gosec // InsecureSkipVerify may be true if the user requests it.
		InsecureSkipVerify: c.InsecureSkipTLSVerify,
		Certificates:       certificates,
		RootCAs:            caCertPool,
	}, nil
}

// Load returns the materialized TLSConfig for the CommonTLS object
func (c *CommonTLS) Load(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (*TLSConfig, error) {
	tls := &TLSConfig{}

	if c.CaCert != nil {
		cert, err := c.CaCert.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}
		tls.CA = cert
	}

	if c.Cert != nil {
		cert, err := c.Cert.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}
		tls.Cert = cert
	}

	if c.Key != nil {
		key, err := c.Key.Load(ctx, client, expander)
		if err != nil {
			return nil, err
		}
		tls.Key = key
	}

	return tls, nil
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
	Auth *AdminAuth `json:"sasl,omitempty"`
}

// AdminAuth configures credentials to connect to Redpanda cluster that has authentication enabled.
type AdminAuth struct {
	// Specifies the username.
	// +optional
	Username string `json:"username,omitempty"`
	// Specifies the password.
	// +optional
	Password *ValueSource `json:"passwordSecretRef,omitempty"`
	// Specifies an auth token.
	// +optional
	AuthToken *ValueSource `json:"token,omitempty"`
}

// TODO: Move this to an AsOption method?
func (a *AdminAuth) AsCredentials(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (username, password, token string, err error) {
	if a == nil {
		return "", "", "", nil
	}

	if a.Password != nil {
		p, err := a.Password.Load(ctx, client, expander)
		if err != nil {
			return "", "", "", errors.Newf("unable to fetch sasl password: %w", err)
		}

		return a.Username, p, "", nil
	}

	if a.AuthToken != nil {
		token, err := a.AuthToken.Load(ctx, client, expander)
		if err != nil {
			return "", "", "", errors.Newf("unable to fetch sasl token: %w", err)
		}
		return "", "", token, nil
	}

	return "", "", "", errors.New("unsupported SASL mechanism, either password or auth token must be specified")
}

// SchemaRegistrySpec defines client configuration for connecting to Redpanda's admin API.
type SchemaRegistrySpec struct {
	// Specifies a list of broker addresses in the format <host>:<port>
	URLs []string `json:"urls"`
	// Defines TLS configuration settings for Redpanda clusters that have TLS enabled.
	// +optional
	TLS *CommonTLS `json:"tls,omitempty"`
	// Defines authentication configuration settings for Redpanda clusters that have authentication enabled.
	// +optional
	SASL *SchemaRegistrySASL `json:"sasl,omitempty"`
}

// SchemaRegistrySASL configures credentials to connect to Redpanda cluster that has authentication enabled.
type SchemaRegistrySASL struct {
	// Specifies the username.
	// +optional
	Username string `json:"username,omitempty"`
	// Specifies the password.
	// +optional
	Password *ValueSource `json:"password,omitempty"`
	// +optional
	AuthToken *ValueSource `json:"token,omitempty"`
}

func (s *SchemaRegistrySASL) AsOption(ctx context.Context, client client.Reader, expander *secrets.CloudExpander) (sr.ClientOpt, error) {
	if s == nil {
		return nil, nil
	}

	if s.Password != nil {
		p, err := s.Password.Load(ctx, client, expander)
		if err != nil {
			return nil, errors.Newf("unable to fetch sasl password: %w", err)
		}

		return sr.BasicAuth(s.Username, p), nil
	}

	if s.AuthToken != nil {
		token, err := s.AuthToken.Load(ctx, client, expander)
		if err != nil {
			return nil, errors.Newf("unable to fetch sasl token: %w", err)
		}
		return sr.BearerToken(token), nil
	}

	return nil, errors.New("could not determine SASL mechanism")
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
	Kafka *KafkaAPISpec `json:"kafka,omitempty"`
	// AdminAPISpec is the configuration information for communicating with the Admin
	// API of a Redpanda cluster where the object should be created.
	Admin *AdminAPISpec `json:"admin,omitempty"`
	// SchemaRegistry is the configuration information for communicating with the Schema Registry
	// API of a Redpanda cluster where the object should be created.
	SchemaRegistry *SchemaRegistrySpec `json:"schemaRegistry,omitempty"`
}

// TLSConfig is the resolved TLS configuration with cert values as PEM-formatted strings.
type TLSConfig struct {
	// CA is the root certificate authority for a TLS connection
	CA string
	// Cert is the client certificate for a TLS connection
	Cert string
	// Key is the client private key for a TLS connection
	Key string
}

func loadConfigMap(ctx context.Context, client client.Reader, name, namespace, key string) (string, error) {
	config := &corev1.ConfigMap{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, config); err != nil {
		return "", errors.Newf("getting ConfigMap %s/%s: %w", namespace, name, err)
	}

	value, ok := config.Data[key]
	if !ok {
		return "", errors.Newf("getting value from ConfigMap %s/%s: key %s not found", namespace, name, key) //nolint:goerr113 // no need to declare new error type
	}
	return value, nil
}

func loadSecret(ctx context.Context, client client.Reader, name, namespace, key string) (string, error) {
	secret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return "", errors.Newf("getting Secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok {
		return "", errors.Newf("getting value from Secret %s/%s: key %s not found", namespace, name, key) //nolint:goerr113 // no need to declare new error type
	}
	return string(value), nil
}
