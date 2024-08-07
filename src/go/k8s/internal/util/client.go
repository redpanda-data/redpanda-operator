package util

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/cluster.redpanda.com/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serviceLabel                = "kubernetes.io/service-name"
	helmInternalCertificateName = "default"
	defaultCertSecretSuffix     = "-default-cert"

	adminPort = "kafka"
)

type remapper interface {
	MapHost(broker string) string
	ConfigureTLS(brokers []string, config *tls.Config) *tls.Config
}

type noopRemapper struct{}

func (n *noopRemapper) MapHost(broker string) string {
	return broker
}

func (n *noopRemapper) ConfigureTLS(brokers []string, config *tls.Config) *tls.Config { return config }

func defaultCertificate(name string) string {
	return name + defaultCertSecretSuffix
}

type ClientFactory struct {
	client.Client
	dialer *net.Dialer

	// this can be overridden in tests to use for port-forwarding into kubernetes
	// installations from the host
	remapper remapper
}

func NewClientFactory(client client.Client) *ClientFactory {
	return &ClientFactory{
		Client:   client,
		remapper: &noopRemapper{},
		dialer:   &net.Dialer{Timeout: 10 * time.Second},
	}
}

// Admin returns a client able to communicate with the cluster defined by the given KafkaAPISpec.
func (c *ClientFactory) Admin(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) (*kadm.Client, error) {
	client, err := c.getClient(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}

	return kadm.NewClient(client), nil
}

// ClusterAdmin returns a client able to communicate with the given Redpanda cluster.
func (c *ClientFactory) ClusterAdmin(ctx context.Context, cluster *redpandav1alpha2.Redpanda) (*kadm.Client, error) {
	client, err := c.getClusterClient(ctx, adminPort, cluster)
	if err != nil {
		return nil, err
	}

	return kadm.NewClient(client), nil
}

type dialer func(ctx context.Context, network string, host string) (net.Conn, error)

func (c *ClientFactory) dialAndRemap(dialer dialer) dialer {
	return func(ctx context.Context, network string, host string) (net.Conn, error) {
		mapped := c.remapper.MapHost(host)
		return dialer(ctx, network, mapped)
	}
}

// getClient returns a simple kgo.Client able to communicate with the given cluster specified via KafkaAPISpec.
func (c *ClientFactory) getClient(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) (*kgo.Client, error) {
	if len(spec.Brokers) == 0 {
		return nil, errors.New("no brokers")
	}

	clientCerts, pool, err := c.getCertificates(ctx, namespace, spec)
	if err != nil {
		return nil, err
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(spec.Brokers...),
	}

	if pool != nil {
		insecure := false
		if spec.TLS != nil && spec.TLS.InsecureSkipTLSVerify {
			insecure = true
		}
		tlsDialer := (&tls.Dialer{
			NetDialer: c.dialer,
			Config: c.remapper.ConfigureTLS(spec.Brokers, &tls.Config{
				Certificates:       clientCerts,
				MinVersion:         tls.VersionTLS12,
				RootCAs:            pool,
				InsecureSkipVerify: insecure,
			}),
		})

		opts = append(opts, kgo.Dialer(c.dialAndRemap(tlsDialer.DialContext)))
	} else {
		opts = append(opts, kgo.Dialer(c.dialAndRemap(c.dialer.DialContext)))
	}

	if spec.SASL != nil {
		opts, err = spec.ConfigureSASL(ctx, namespace, c.Client, opts, logr.Discard())
		if err != nil {
			return nil, err
		}
	}

	return kgo.NewClient(opts...)
}

// getClusterClient returns a simple kgo.Client able to communicate with the given cluster.
func (c *ClientFactory) getClusterClient(ctx context.Context, portName string, cluster *redpandav1alpha2.Redpanda) (*kgo.Client, error) {
	brokers, err := c.getClusterBrokers(ctx, portName, cluster)
	if err != nil {
		return nil, err
	}

	if len(brokers) == 0 {
		return nil, errors.New("no brokers")
	}

	clientCerts, pool, err := c.getClusterCertificates(ctx, cluster)
	if err != nil {
		return nil, err
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
	}

	if pool != nil {
		tlsDialer := (&tls.Dialer{
			NetDialer: c.dialer,
			Config: c.remapper.ConfigureTLS(brokers, &tls.Config{
				Certificates: clientCerts,
				MinVersion:   tls.VersionTLS12,
				RootCAs:      pool,
			}),
		})

		opts = append(opts, kgo.Dialer(c.dialAndRemap(tlsDialer.DialContext)))
	} else {
		opts = append(opts, kgo.Dialer(c.dialAndRemap(c.dialer.DialContext)))
	}

	return kgo.NewClient(opts...)
}

// getClusterBrokers fetches the internal broker addresses for a Redpanda cluster, returning
// hostname:port pairs where port is the named port exposed by the internal cluster service.
func (c *ClientFactory) getClusterBrokers(ctx context.Context, portName string, cluster *redpandav1alpha2.Redpanda) ([]string, error) {
	service := &corev1.Service{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(cluster), service); err != nil {
		return nil, err
	}
	endpointSlices := &discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, endpointSlices, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			serviceLabel: service.Name,
		}),
		Namespace: service.Namespace,
	}); err != nil {
		return nil, err
	}

	addresses := []string{}
	for _, endpointSlice := range endpointSlices.Items {
		for _, port := range endpointSlice.Ports {
			if port.Name != nil && *port.Name == portName && port.Port != nil {
				for _, endpoint := range endpointSlice.Endpoints {
					host := fmt.Sprintf("%s.%s.%s.svc", endpoint.TargetRef.Name, service.Name, service.Namespace)
					addresses = append(addresses, fmt.Sprintf("%s:%d", host, *port.Port))
				}
			}
		}
	}
	return addresses, nil
}

// getClusterCertificates fetches and parses client and server certificates for connecting
// to a Redpanda cluster
func (c *ClientFactory) getClusterCertificates(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]tls.Certificate, *x509.CertPool, error) {
	client, server, err := c.getClusterCertificateSecrets(ctx, cluster)
	if err != nil {
		return nil, nil, err
	}

	if server == nil {
		return nil, nil, nil
	}

	serverPubKey := server.Data[corev1.TLSCertKey]
	caBlock, _ := pem.Decode(serverPubKey)
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)

	if client != nil {
		clientCert := client.Data[corev1.TLSCertKey]
		clientPrivateKey := client.Data[corev1.TLSPrivateKeyKey]
		clientKey, err := tls.X509KeyPair(clientCert, clientPrivateKey)
		if err != nil {
			return nil, nil, err
		}
		return []tls.Certificate{clientKey}, pool, nil
	}

	return nil, pool, nil
}

func (c *ClientFactory) getCertificates(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) ([]tls.Certificate, *x509.CertPool, error) {
	clientCert, clientPrivateKey, serverPubKey, err := c.getCertificateSecrets(ctx, namespace, spec)
	if err != nil {
		return nil, nil, err
	}

	if serverPubKey == nil {
		return nil, nil, nil
	}

	caBlock, _ := pem.Decode(serverPubKey)
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)

	if clientCert != nil && clientPrivateKey != nil {
		clientKey, err := tls.X509KeyPair(clientCert, clientPrivateKey)
		if err != nil {
			return nil, nil, err
		}
		return []tls.Certificate{clientKey}, pool, nil
	}

	return nil, pool, nil
}

// getCertificateSecrets fetches the client and server certs for a Redpanda cluster
func (c *ClientFactory) getCertificateSecrets(ctx context.Context, namespace string, spec *redpandav1alpha1.KafkaAPISpec) ([]byte, []byte, []byte, error) {
	tls := spec.TLS
	if tls == nil {
		return nil, nil, nil, nil
	}

	if tls.CaCert != nil {
		return c.getCertificateSecretsFromV1Refs(ctx, namespace, spec.TLS.CaCert, spec.TLS.Cert, spec.TLS.Key)
	}

	return nil, nil, nil, nil
}

// getClusterCertificateSecrets fetches the client and server certs for a Redpanda cluster
func (c *ClientFactory) getClusterCertificateSecrets(ctx context.Context, cluster *redpandav1alpha2.Redpanda) (*corev1.Secret, *corev1.Secret, error) {
	spec := cluster.Spec.ClusterSpec
	if spec == nil {
		return c.getDefaultCertificateSecrets(ctx, cluster.Name, cluster.Namespace)
	}

	tls := spec.TLS
	if tls == nil {
		return c.getDefaultCertificateSecrets(ctx, cluster.Name, cluster.Namespace)
	}
	if tls.Enabled != nil && !*tls.Enabled {
		return nil, nil, nil
	}
	if internal, ok := tls.Certs[helmInternalCertificateName]; ok {
		return c.getCertificateSecretsFromRefs(ctx, cluster.Namespace, internal.SecretRef, internal.ClientSecretRef)
	}
	return c.getDefaultCertificateSecrets(ctx, cluster.Name, cluster.Namespace)
}

// getDefaultCertificateSecrets fetches the default server secret
func (c *ClientFactory) getDefaultCertificateSecrets(ctx context.Context, name, namespace string) (*corev1.Secret, *corev1.Secret, error) {
	return c.getCertificateSecretsFromRefs(ctx, namespace, &redpandav1alpha2.SecretRef{Name: defaultCertificate(name)}, nil)
}

// getCertificateSecretsFromRefs fetches a server and client (if specified) secret from SecretRefs
func (c *ClientFactory) getCertificateSecretsFromRefs(ctx context.Context, namespace string, server, client *redpandav1alpha2.SecretRef) (*corev1.Secret, *corev1.Secret, error) {
	serverSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: server.Name}, serverSecret); err != nil {
		return nil, nil, err
	}
	if client != nil {
		clientSecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: client.Name}, clientSecret); err != nil {
			return nil, nil, err
		}
		return clientSecret, serverSecret, nil
	}
	return nil, serverSecret, nil
}

// getCertificateSecretsFromV1Refs fetches a server and client (if specified) secret from SecretRefs
func (c *ClientFactory) getCertificateSecretsFromV1Refs(ctx context.Context, namespace string, server, clientCert, clientKey *redpandav1alpha1.SecretKeyRef) ([]byte, []byte, []byte, error) {
	serverSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: server.Name}, serverSecret); err != nil {
		return nil, nil, nil, err
	}

	key := server.Key
	if key == "" {
		key = corev1.TLSCertKey
	}
	serverCA := serverSecret.Data[key]

	if clientCert != nil && clientKey != nil {
		clientCertSecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clientCert.Name}, clientCertSecret); err != nil {
			return nil, nil, nil, err
		}
		clientKeySecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clientKey.Name}, clientKeySecret); err != nil {
			return nil, nil, nil, err
		}
		return clientCertSecret.Data[clientCert.Key], clientKeySecret.Data[clientKey.Key], serverCA, nil
	}
	return nil, nil, serverCA, nil
}
