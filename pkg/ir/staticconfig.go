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
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (k *KafkaAPISpec) Load(ctx context.Context, client client.Reader) (*KafkaAPIConfiguration, error) {
	config := &KafkaAPIConfiguration{
		Brokers: k.Brokers,
	}
	if k.TLS != nil {
		tls, err := k.TLS.Load(ctx, client)
		if err != nil {
			return nil, err
		}
		config.TLS = tls
	}
	if k.SASL != nil {
		sasl, err := k.SASL.Load(ctx, client)
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
	Password *SecretKeyRef `json:"passwordSecretRef,omitempty"`
	// Specifies the SASL/SCRAM authentication mechanism.
	Mechanism SASLMechanism `json:"mechanism"`
	// +optional
	OAUth *KafkaSASLOAuthBearer `json:"oauth,omitempty"`
	// +optional
	GSSAPIConfig *KafkaSASLGSSAPI `json:"gssapi,omitempty"`
	// +optional
	AWSMskIam *KafkaSASLAWSMskIam `json:"awsMskIam,omitempty"`
}

type AuthUser struct {
	Username  string
	Password  string
	Mechanism string
}

func (k *KafkaSASL) Load(ctx context.Context, client client.Reader) (*AuthUser, error) {
	password, err := loadSecret(ctx, client, k.Password.Name, k.Password.Namespace, k.Password.Key)
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
	CaCert *ObjectKeyRef `json:"caCertSecretRef,omitempty"`
	// Cert is the reference for client public certificate to establish mTLS connection to Redpanda
	Cert *SecretKeyRef `json:"certSecretRef,omitempty"`
	// Key is the reference for client private certificate to establish mTLS connection to Redpanda
	Key *SecretKeyRef `json:"keySecretRef,omitempty"`
	// InsecureSkipTLSVerify can skip verifying Redpanda self-signed certificate when establish TLS connection to Redpanda
	// +optional
	InsecureSkipTLSVerify bool `json:"insecureSkipTlsVerify"`
}

// Config returns the materialized tls.Config for the CommonTLS object
func (c *CommonTLS) Config(ctx context.Context, client client.Reader) (*tls.Config, error) {
	config, err := c.Load(ctx, client)
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
			return nil, fmt.Errorf("cannot parse pem: %w", err)
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
func (c *CommonTLS) Load(ctx context.Context, client client.Reader) (*TLSConfig, error) {
	tls := &TLSConfig{}

	if c.CaCert != nil {
		key := "ca.crt"
		if c.CaCert.ConfigMapKeyRef != nil {
			if c.CaCert.ConfigMapKeyRef.Key != "" {
				key = c.CaCert.ConfigMapKeyRef.Key
			}
			ca, err := loadConfigMap(ctx, client, c.CaCert.ConfigMapKeyRef.Name, c.CaCert.Namespace, key)
			if err != nil {
				return nil, err
			}
			tls.CA = ca
		} else if c.CaCert.SecretKeyRef != nil {
			if c.CaCert.SecretKeyRef.Key != "" {
				key = c.CaCert.SecretKeyRef.Key
			}
			ca, err := loadSecret(ctx, client, c.CaCert.SecretKeyRef.Name, c.CaCert.Namespace, key)
			if err != nil {
				return nil, err
			}
			tls.CA = ca
		}
	}

	if c.Cert != nil {
		key := "tls.crt"
		if c.Cert.Key != "" {
			key = c.Cert.Key
		}
		cert, err := loadSecret(ctx, client, c.Cert.Name, c.Cert.Namespace, key)
		if err != nil {
			return nil, err
		}
		tls.Cert = cert
	}

	if c.Key != nil {
		key := "tls.key"
		if c.Key.Key != "" {
			key = c.Key.Key
		}
		key, err := loadSecret(ctx, client, c.Key.Name, c.Key.Namespace, key)
		if err != nil {
			return nil, err
		}
		tls.Key = key
	}

	return tls, nil
}

type ObjectKeyRef struct {
	Namespace       string                       `json:"namespace,omitempty"`
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef,omitempty"`
}

// SecretKeyRef contains enough information to inspect or modify the referred Secret data
// See https://pkg.go.dev/k8s.io/api/core/v1#ObjectReference.
type SecretKeyRef struct {
	Namespace string `json:"namespace,omitempty"`

	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name"`

	// +optional
	// Key in Secret data to get value from
	Key string `json:"key,omitempty"`
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
	Password SecretKeyRef `json:"passwordSecretRef,omitempty"`
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
	Password SecretKeyRef `json:"passwordSecretRef,omitempty"`
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
		return "", fmt.Errorf("getting ConfigMap %s/%s: %w", namespace, name, err)
	}

	value, ok := config.Data[key]
	if !ok {
		return "", fmt.Errorf("getting value from ConfigMap %s/%s: key %s not found", namespace, name, key) //nolint:goerr113 // no need to declare new error type
	}
	return value, nil
}

func loadSecret(ctx context.Context, client client.Reader, name, namespace, key string) (string, error) {
	secret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return "", fmt.Errorf("getting Secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("getting value from Secret %s/%s: key %s not found", namespace, name, key) //nolint:goerr113 // no need to declare new error type
	}
	return string(value), nil
}
