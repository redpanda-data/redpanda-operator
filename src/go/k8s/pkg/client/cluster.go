// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandachart "github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Factory) dotFor(cluster *redpandav1alpha2.Redpanda) (*helmette.Dot, error) {
	var values []byte
	var partial redpandachart.PartialValues

	values, err := json.Marshal(cluster.Spec.ClusterSpec)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(values, &partial); err != nil {
		return nil, err
	}

	release := helmette.Release{
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
		Service:   "redpanda",
		IsInstall: true,
	}

	return redpandachart.Dot(release, partial)
}

// RedpandaAdminForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, error) {
	dot, err := c.dotFor(cluster)
	if err != nil {
		return nil, err
	}

	return c.getAdminClient(ctx, dot, c.dialer)
}

// KafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) kafkaForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kgo.Client, error) {
	dot, err := c.dotFor(cluster)
	if err != nil {
		return nil, err
	}

	return c.getKafkaClient(ctx, dot, c.dialer, opts...)
}

var (
	ErrServerCertificateNotFound          = errors.New("server TLS certificate not found")
	ErrServerCertificatePublicKeyNotFound = errors.New("server TLS certificate does not contain a public key")

	ErrClientCertificateNotFound           = errors.New("client TLS certificate not found")
	ErrClientCertificatePublicKeyNotFound  = errors.New("client TLS certificate does not contain a public key")
	ErrClientCertificatePrivateKeyNotFound = errors.New("client TLS certificate does not contain a private key")

	ErrSASLSecretNotFound          = errors.New("users secret not found")
	ErrSASLSecretKeyNotFound       = errors.New("users secret key not found")
	ErrSASLSecretSuperuserNotFound = errors.New("users secret has no users")

	supportedSASLMechanisms = []string{
		"SCRAM-SHA-256", "SCRAM-SHA-512",
	}
)

// getAdminClient creates a client to talk to a Redpanda cluster admin API based on its helm
// configuration over its internal listeners.
func (c *Factory) getAdminClient(ctx context.Context, dot *helmette.Dot, dialer redpanda.DialContextFunc) (*rpadmin.AdminAPI, error) {
	values := helmette.Unwrap[redpandachart.Values](dot.Values)
	name := redpandachart.Fullname(dot)
	domain := redpandachart.InternalDomain(dot)
	prefix := "http://"

	var tlsConfig *tls.Config
	var err error

	if redpandachart.TLSEnabled(dot) {
		prefix = "https://"

		tlsConfig, err = c.tlsConfigFromDot(ctx, dot, values.Listeners.Kafka.TLS.Cert)
		if err != nil {
			return nil, err
		}
	}

	var auth rpadmin.Auth = &rpadmin.NopAuth{}

	if c.userAuth != nil {
		auth = &rpadmin.BasicAuth{
			Username: c.userAuth.Username,
			Password: c.userAuth.Password,
		}
	} else {
		username, password, _, err := c.authFromDot(ctx, dot)
		if err != nil {
			return nil, err
		}

		if username != "" {
			auth = &rpadmin.BasicAuth{
				Username: username,
				Password: password,
			}
		}
	}

	hosts := redpandachart.ServerList(values.Statefulset.Replicas, prefix, name, domain, values.Listeners.Admin.Port)

	return rpadmin.NewAdminAPIWithDialer(hosts, auth, tlsConfig, dialer)
}

// getKafkaClient creates a client to talk to a Redpanda cluster based on its helm
// configuration over its internal listeners.
func (c *Factory) getKafkaClient(ctx context.Context, dot *helmette.Dot, dialer redpanda.DialContextFunc, opts ...kgo.Opt) (*kgo.Client, error) {
	values := helmette.Unwrap[redpandachart.Values](dot.Values)
	name := redpandachart.Fullname(dot)
	domain := redpandachart.InternalDomain(dot)

	brokers := redpandachart.ServerList(values.Statefulset.Replicas, "", name, domain, values.Listeners.Kafka.Port)

	opts = append(opts, kgo.SeedBrokers(brokers...))

	if redpandachart.TLSEnabled(dot) {
		tlsConfig, err := c.tlsConfigFromDot(ctx, dot, values.Listeners.Kafka.TLS.Cert)
		if err != nil {
			return nil, err
		}

		// we can only specify one of DialTLSConfig or Dialer
		if dialer == nil {
			opts = append(opts, kgo.DialTLSConfig(tlsConfig))
		} else {
			opts = append(opts, kgo.Dialer(wrapTLSDialer(dialer, tlsConfig)))
		}
	} else if dialer != nil {
		opts = append(opts, kgo.Dialer(dialer))
	}

	if c.userAuth != nil {
		opts = append(opts, saslOpt(c.userAuth.Username, c.userAuth.Password, c.userAuth.Mechanism))
	} else {
		username, password, mechanism, err := c.authFromDot(ctx, dot)
		if err != nil {
			return nil, err
		}

		if username != "" {
			opts = append(opts, saslOpt(username, password, mechanism))
		}
	}

	return kgo.NewClient(opts...)
}

func (c *Factory) authFromDot(ctx context.Context, dot *helmette.Dot) (username string, password string, mechanism string, err error) {
	saslUsers := redpandachart.SecretSASLUsers(dot)
	saslUsersError := func(err error) error {
		return fmt.Errorf("error fetching SASL authentication for %s/%s: %w", saslUsers.Namespace, saslUsers.Name, err)
	}

	if saslUsers != nil {
		// read from the server since we're assuming all the resources
		// have already been created
		var users corev1.Secret
		found, lookupErr := c.safeLookup(ctx, saslUsers.Namespace, saslUsers.Name, &users)
		if lookupErr != nil {
			err = saslUsersError(lookupErr)
			return
		}

		if !found {
			err = saslUsersError(ErrSASLSecretNotFound)
			return
		}

		data, found := users.Data["users.txt"]
		if !found {
			err = saslUsersError(ErrSASLSecretKeyNotFound)
			return
		}

		username, password, mechanism = firstUser(data)
		if username == "" {
			err = saslUsersError(ErrSASLSecretSuperuserNotFound)
			return
		}
	}

	return
}

func (c *Factory) tlsConfigFromDot(ctx context.Context, dot *helmette.Dot, cert string) (*tls.Config, error) {
	name := redpandachart.Fullname(dot)
	namespace := dot.Release.Namespace
	serviceName := redpandachart.ServiceName(dot)
	clientCertName := fmt.Sprintf("%s-client", name)
	rootCertName := fmt.Sprintf("%s-%s-root-certificate", name, cert)
	serverName := fmt.Sprintf("%s.%s.svc", serviceName, namespace)

	serverTLSError := func(err error) error {
		return fmt.Errorf("error fetching server root CA %s/%s: %w", namespace, rootCertName, err)
	}
	clientTLSError := func(err error) error {
		return fmt.Errorf("error fetching client certificate default/%s: %w", clientCertName, err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}

	var serverCert corev1.Secret
	found, lookupErr := c.safeLookup(ctx, namespace, rootCertName, &serverCert)
	if lookupErr != nil {
		return nil, serverTLSError(lookupErr)
	}

	if !found {
		return nil, serverTLSError(ErrServerCertificateNotFound)
	}

	serverPublicKey, found := serverCert.Data[corev1.TLSCertKey]
	if !found {
		return nil, serverTLSError(ErrServerCertificatePublicKeyNotFound)
	}

	block, _ := pem.Decode(serverPublicKey)
	serverParsedCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, serverTLSError(fmt.Errorf("unable to parse public key %w", err))
	}
	pool := x509.NewCertPool()
	pool.AddCert(serverParsedCertificate)

	tlsConfig.RootCAs = pool

	if redpandachart.ClientAuthRequired(dot) {
		var clientCert corev1.Secret
		found, lookupErr := c.safeLookup(ctx, namespace, clientCertName, &clientCert)
		if lookupErr != nil {
			return nil, clientTLSError(lookupErr)
		}

		if !found {
			return nil, clientTLSError(ErrServerCertificateNotFound)
		}

		clientPublicKey, found := clientCert.Data[corev1.TLSCertKey]
		if !found {
			return nil, clientTLSError(ErrClientCertificatePublicKeyNotFound)
		}

		clientPrivateKey, found := clientCert.Data[corev1.TLSPrivateKeyKey]
		if !found {
			return nil, clientTLSError(ErrClientCertificatePrivateKeyNotFound)
		}

		clientKey, err := tls.X509KeyPair(clientPublicKey, clientPrivateKey)
		if err != nil {
			return nil, clientTLSError(fmt.Errorf("unable to parse public and private key %w", err))
		}

		tlsConfig.Certificates = []tls.Certificate{clientKey}
	}

	return tlsConfig, nil
}

func (c *Factory) safeLookup(ctx context.Context, namespace, name string, o client.Object) (bool, error) {
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, o); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func firstUser(data []byte) (user string, password string, mechanism string) {
	file := string(data)

	for _, line := range strings.Split(file, "\n") {
		tokens := strings.Split(line, ":")
		if len(tokens) != 3 {
			continue
		}

		if !slices.Contains(supportedSASLMechanisms, tokens[2]) {
			continue
		}

		user, password, mechanism = tokens[0], tokens[1], tokens[2]
		return
	}

	return
}

func saslOpt(user, password, mechanism string) kgo.Opt {
	var m sasl.Mechanism
	switch mechanism {
	case "SCRAM-SHA-256", "SCRAM-SHA-512":
		scram := scram.Auth{User: user, Pass: password}

		switch mechanism {
		case "SCRAM-SHA-256":
			m = scram.AsSha256Mechanism()
		case "SCRAM-SHA-512":
			m = scram.AsSha512Mechanism()
		}
	default:
		panic(fmt.Sprintf("unhandled SASL mechanism: %s", mechanism))
	}

	return kgo.SASL(m)
}
