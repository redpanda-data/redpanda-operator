// Copyright 2025 Redpanda Data, Inc.
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
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

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

	// permitOutOfClusterDNS controls whether or not this package will use the
	// provided dialer to approximate "out of cluster DNS" by constructing a
	// [net.Resolver] that tunnels into a kube-dns Pod. Building with the
	// integration build tag will set this flag to true as that's the only
	// environment we expect to use out of cluster DNS.
	permitOutOfClusterDNS = false
)

// DialContextFunc is a function that acts as a dialer for the underlying Kafka client.
type DialContextFunc = func(ctx context.Context, network, host string) (net.Conn, error)

// AdminClient creates a client to talk to a Redpanda cluster admin API based on its helm
// configuration over its internal listeners.
func AdminClient(dot *helmette.Dot, dialer DialContextFunc, opts ...rpadmin.Opt) (*rpadmin.AdminAPI, error) {
	values := helmette.Unwrap[redpanda.Values](dot.Values)

	var err error
	var tlsConfig *tls.Config

	if values.Listeners.Admin.TLS.IsEnabled(&values.TLS) {
		tlsConfig, err = tlsConfigFromDot(dot, values.Listeners.Admin.TLS)
		if err != nil {
			return nil, err
		}
	}

	var auth rpadmin.Auth
	username, password, _, err := authFromDot(dot)
	if err != nil {
		return nil, err
	}

	if username != "" {
		auth = &rpadmin.BasicAuth{
			Username: username,
			Password: password,
		}
	} else {
		auth = &rpadmin.NopAuth{}
	}

	records, err := srvLookup(dot, dialer, redpanda.InternalAdminAPIPortName)
	if err != nil {
		return nil, err
	}

	hosts := make([]string, len(records))
	for i, record := range records {
		hosts[i] = fmt.Sprintf("%s:%d", record.Target, record.Port)
	}

	// NB: rpadmin automatically infers http or https, if not provided, based on the tlsConfig.
	client, err := rpadmin.NewAdminAPIWithDialer(hosts, auth, tlsConfig, dialer, opts...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

// SchemaRegistryClient creates a client to talk to a Redpanda cluster admin API based on its helm
// configuration over its internal listeners.
func SchemaRegistryClient(dot *helmette.Dot, dialer DialContextFunc, opts ...sr.ClientOpt) (*sr.Client, error) {
	values := helmette.Unwrap[redpanda.Values](dot.Values)
	prefix := "http://"

	// These transport values come from the TLS client options found here:
	// https://github.com/twmb/franz-go/blob/cea7aa5d803781e5f0162187795482ba1990c729/pkg/sr/clientopt.go#L48-L68
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		DialContext:           dialer,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if dialer == nil {
		transport.DialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	if values.Listeners.SchemaRegistry.TLS.IsEnabled(&values.TLS) {
		prefix = "https://"

		tlsConfig, err := tlsConfigFromDot(dot, values.Listeners.SchemaRegistry.TLS)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}

	copts := []sr.ClientOpt{sr.HTTPClient(&http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	})}

	username, password, _, err := authFromDot(dot)
	if err != nil {
		return nil, err
	}

	if username != "" {
		copts = append(copts, sr.BasicAuth(username, password))
	}

	records, err := srvLookup(dot, dialer, redpanda.InternalSchemaRegistryPortName)
	if err != nil {
		return nil, err
	}

	hosts := make([]string, len(records))
	for i, record := range records {
		hosts[i] = fmt.Sprintf("%s%s:%d", prefix, record.Target, record.Port)
	}

	copts = append(copts, sr.URLs(hosts...))

	// finally, override any calculated client opts with whatever was
	// passed in
	client, err := sr.NewClient(append(copts, opts...)...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

// KafkaClient creates a client to talk to a Redpanda cluster based on its helm
// configuration over its internal listeners.
func KafkaClient(dot *helmette.Dot, dialer DialContextFunc, opts ...kgo.Opt) (*kgo.Client, error) {
	values := helmette.Unwrap[redpanda.Values](dot.Values)

	records, err := srvLookup(dot, dialer, redpanda.InternalKafkaPortName)
	if err != nil {
		return nil, err
	}

	brokers := make([]string, len(records))
	for i, record := range records {
		brokers[i] = fmt.Sprintf("%s:%d", record.Target, record.Port)
	}

	opts = append(opts, kgo.SeedBrokers(brokers...))

	if values.Listeners.Kafka.TLS.IsEnabled(&values.TLS) {
		tlsConfig, err := tlsConfigFromDot(dot, values.Listeners.Kafka.TLS)
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

	username, password, mechanism, err := authFromDot(dot)
	if err != nil {
		return nil, err
	}

	if username != "" {
		opts = append(opts, saslOpt(username, password, mechanism))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return client, nil
}

func authFromDot(dot *helmette.Dot) (username string, password string, mechanism string, err error) {
	values := helmette.Unwrap[redpanda.Values](dot.Values)

	bootstrapUser := redpanda.SecretBootstrapUser(dot)

	if bootstrapUser != nil {
		// if we have any errors grabbing the credentials from the bootstrap user
		// then we'll just fallback to the superuser parsing code
		user, found, lookupErr := helmette.SafeLookup[corev1.Secret](dot, bootstrapUser.Namespace, bootstrapUser.Name)
		if lookupErr == nil && found {
			selector := values.Auth.SASL.BootstrapUser.SecretKeySelector(redpanda.Fullname(dot))
			mechanism := values.Auth.SASL.BootstrapUser.GetMechanism()
			if data, found := user.Data[selector.Key]; found {
				return values.Auth.SASL.BootstrapUser.Username(), string(data), mechanism, nil
			}
		}
	}

	saslUsers := redpanda.SecretSASLUsers(dot)
	saslUsersError := func(err error) error {
		return fmt.Errorf("error fetching SASL authentication for %s/%s: %w", saslUsers.Namespace, saslUsers.Name, err)
	}

	if saslUsers != nil {
		// read from the server since we're assuming all the resources
		// have already been created
		users, found, lookupErr := helmette.SafeLookup[corev1.Secret](dot, saslUsers.Namespace, saslUsers.Name)
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

func certificatesFor(dot *helmette.Dot, name string) (certSecret, certKey, clientSecret string) {
	values := helmette.Unwrap[redpanda.Values](dot.Values)

	cert, ok := values.TLS.Certs[name]
	if !ok || !ptr.Deref(cert.Enabled, true) {
		// TODO this isn't correct but it matches historical behavior.
		fullname := redpanda.Fullname(dot)
		certSecret = fmt.Sprintf("%s-%s-root-certificate", fullname, name)
		clientSecret = fmt.Sprintf("%s-default-client-cert", fullname)

		return certSecret, corev1.TLSCertKey, clientSecret
	}

	ref := cert.CASecretRef(dot, name)
	return ref.LocalObjectReference.Name, ref.Key, cert.ClientSecretName(dot, name)
}

func tlsConfigFromDot(dot *helmette.Dot, listener redpanda.InternalTLS) (*tls.Config, error) {
	namespace := dot.Release.Namespace
	serverName := redpanda.InternalDomain(dot)

	rootCertName, rootCertKey, clientCertName := certificatesFor(dot, listener.Cert)

	serverTLSError := func(err error) error {
		return fmt.Errorf("error fetching server root CA %s/%s: %w", namespace, rootCertName, err)
	}
	clientTLSError := func(err error) error {
		return fmt.Errorf("error fetching client certificate default/%s: %w", clientCertName, err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}

	serverCert, found, lookupErr := helmette.SafeLookup[corev1.Secret](dot, namespace, rootCertName)
	if lookupErr != nil {
		return nil, serverTLSError(lookupErr)
	}

	if !found {
		return nil, serverTLSError(ErrServerCertificateNotFound)
	}

	serverPublicKey, found := serverCert.Data[rootCertKey]
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

	if listener.RequireClientAuth {
		clientCert, found, lookupErr := helmette.SafeLookup[corev1.Secret](dot, namespace, clientCertName)
		if lookupErr != nil {
			return nil, clientTLSError(lookupErr)
		}

		if !found {
			return nil, clientTLSError(ErrServerCertificateNotFound)
		}

		// we always use tls.crt for client certs
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

func firstUser(data []byte) (user string, password string, mechanism string) {
	file := string(data)

	for _, line := range strings.Split(file, "\n") {
		tokens := strings.Split(line, ":")

		switch len(tokens) {
		case 2:
			return tokens[0], tokens[1], redpanda.DefaultSASLMechanism

		case 3:
			if !slices.Contains(supportedSASLMechanisms, tokens[2]) {
				continue
			}

			return tokens[0], tokens[1], tokens[2]

		default:
			continue
		}
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

func wrapTLSDialer(dialer DialContextFunc, config *tls.Config) DialContextFunc {
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		conn, err := dialer(ctx, network, host)
		if err != nil {
			return nil, err
		}
		return tls.Client(conn, config), nil
	}
}

// srvLookup performs an SRV DNS lookup on the given helm release as a form of service discovery.
//
// As with all forms of service discovery, this method may miss Pods that are
// temporarily unavailable at the time of invocation.
//
// If dialer is nil, this function assumes that it's being executed from within
// a Kubernetes and performs a DNS query through the default resolver.
//
// See also: https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#srv-records
func srvLookup(dot *helmette.Dot, dialer DialContextFunc, service string) ([]*net.SRV, error) {
	// To preserve backwards compatibility of the top level client
	// constructor's methods, we use a context with a static timeout.
	// While less than ideal, 30s should be a reasonable upper limit for this method.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A nil / zero resolver is valid. In the case that dialer is nil, we
	// assume our DNS requests will go to kube-dns and appropriately resolve.
	var resolver net.Resolver

	// Otherwise we'll perform an out of cluster DNS query. This code block
	// will only be executed from our test cases (gated on the integration
	// build tag). It's a bit sketchy but is technically valid / safe to
	// run outside of test cases. See the below comments for details.
	//
	// NB: It may not always be safe to assume that dialer != nil indicates
	// execution outside of a cluster.
	if permitOutOfClusterDNS && dialer != nil {
		ctl, err := kube.FromRESTConfig(dot.KubeConfig)
		if err != nil {
			return nil, err
		}

		resolver = net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _network, _address string) (net.Conn, error) {
				// Querying for k8s-app=kube-dns is a generally accepted / safe
				// way of finding the kube DNS. We could alternatively find the
				// kube-dns service and use its label selector.
				pods, err := kube.List[corev1.PodList](ctx, ctl, kube.NamespaceSystem, client.MatchingLabels{
					"k8s-app": "kube-dns",
				})
				if err != nil {
					return nil, err
				}

				if len(pods.Items) == 0 {
					return nil, errors.New("failed to locate core DNS Pods for out of cluster DNS queries")
				}

				pod := pods.Items[0]

				// Fun fact: core-dns and most kube-dns implementations are
				// exposed over TCP in addition to UDP which makes this method
				// possible.
				//
				// This is where things get fairly sketchy.
				//
				// We're ignoring network because kubectl portforward doesn't
				// support UDP, the standard protocol for DNS, and we're
				// assuming that kube-dns will accept TCP connections. This is
				// _generally_ a safe assumption.
				//
				// We're ignoring address because we don't want to use the
				// system supplied name servers and instead go directly to
				// kube-dns. We're also assuming the kube-dns is serving on
				// port 53, the standard port for DNS. Yet another generally
				// correct but not bulletproof assumption.
				//
				// Furthermore, this dial call will very likely result in
				// another DNS query being kicked off through the system
				// resolver to  initiate the portforward. Again, sketchy but
				// technically OK.
				return dialer(ctx, "tcp", fmt.Sprintf("%s.%s:53", pod.Name, pod.Namespace))
			},
		}
	}

	_, records, err := resolver.LookupSRV(ctx, service, "tcp", redpanda.InternalDomain(dot))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return records, nil
}
