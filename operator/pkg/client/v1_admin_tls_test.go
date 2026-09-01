// Copyright 2026 Redpanda Data, Inc.
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
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

// adminServer stands in for a broker's admin API. Its dialer records the
// address the client asked for and connects it to the server instead, so a
// test can pin the URL the builder derives without reaching into rpadmin, and
// a completed request proves the scheme, the trusted CA, and the client
// certificate all line up.
type adminServer struct {
	srv *httptest.Server

	mu            sync.Mutex
	dialed        []string
	authorization []string
}

func newAdminServer(t *testing.T, tlsConfig *tls.Config) *adminServer {
	t.Helper()

	s := &adminServer{}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.authorization = append(s.authorization, r.Header.Get("Authorization"))
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	if tlsConfig != nil {
		s.srv.TLS = tlsConfig
		s.srv.StartTLS()
	} else {
		s.srv.Start()
	}
	t.Cleanup(s.srv.Close)

	return s
}

func (s *adminServer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	s.mu.Lock()
	s.dialed = append(s.dialed, addr)
	s.mu.Unlock()

	return (&net.Dialer{}).DialContext(ctx, network, s.srv.Listener.Addr().String())
}

func keyPair(t *testing.T, cert *bootstrap.Certificate) tls.Certificate {
	t.Helper()

	pair, err := tls.X509KeyPair(cert.Bytes(), cert.PrivateKeyBytes())
	require.NoError(t, err)
	return pair
}

// TestRedpandaAdminForV1Cluster covers the admin API client the Factory builds
// for vectorized.io/v1alpha1 Clusters. Every V2 CR and controller that reaches
// a V1 cluster over the admin API lands here: User, Role, and ShadowLink CRs,
// Broker CRs on V1 node pools, and the ghost-broker decommissioner. It used to
// refuse any Cluster with TLS on any admin listener with "non-TLS admin API is
// not supported on V1 CRD", so TLS-enabled V1 clusters could not be managed
// at all, and a plaintext internal listener was rejected when only the
// external one had TLS.
//
// Each case drives the real builder through a stub multicluster manager and
// points its dialer at an httptest server, so the assertions cover what the
// client does on the wire rather than whether a struct came back.
func TestRedpandaAdminForV1Cluster(t *testing.T) {
	ctx := context.Background()

	// The CA cert-manager writes into the node and client certificate secrets.
	// The server certificate carries the SANs the operator mints for the admin
	// API node certificate (certmanager/certificate.go): the headless service
	// FQDN without its trailing dot, and a wildcard for the pods under it.
	ca, err := bootstrap.GenerateCA("redpanda-operator", "test-ca", nil)
	require.NoError(t, err)
	serverCert, err := ca.Sign("test.default.svc.cluster.local", "*.test.default.svc.cluster.local")
	require.NoError(t, err)
	clientCert, err := ca.Sign("test-admin-api-client")
	require.NoError(t, err)

	otherCA, err := bootstrap.GenerateCA("redpanda-operator", "other-ca", nil)
	require.NoError(t, err)
	untrustedServerCert, err := otherCA.Sign("test.default.svc.cluster.local", "*.test.default.svc.cluster.local")
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	require.True(t, caPool.AppendCertsFromPEM(ca.Bytes()))

	serverTLS := &tls.Config{Certificates: []tls.Certificate{keyPair(t, serverCert)}}
	untrustedServerTLS := &tls.Config{Certificates: []tls.Certificate{keyPair(t, untrustedServerCert)}}
	mutualTLS := &tls.Config{
		Certificates: []tls.Certificate{keyPair(t, serverCert)},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	plaintext := vectorizedv1alpha1.AdminAPI{Port: 9644}
	withTLS := vectorizedv1alpha1.AdminAPI{Port: 9644, TLS: vectorizedv1alpha1.AdminAPITLS{Enabled: true}}
	withMutualTLS := vectorizedv1alpha1.AdminAPI{Port: 9644, TLS: vectorizedv1alpha1.AdminAPITLS{Enabled: true, RequireClientAuth: true}}
	// The external listener uses a shape the validating webhook accepts: a
	// port in the nodeport range ("external port must be in the following
	// range: 30000-32768") and a subdomain ("TLS requires specifying a
	// subdomain").
	externalWithTLS := vectorizedv1alpha1.AdminAPI{
		Port:     30644,
		External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true, Subdomain: "admin.example.com"},
		TLS:      vectorizedv1alpha1.AdminAPITLS{Enabled: true},
	}

	// The secrets cert-manager would create for the admin API: the node
	// certificate holding the CA the operator pins, and the client certificate
	// it presents when the listener requires client auth.
	nodeSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-admin-api-node", Namespace: "default"},
		Data:       map[string][]byte{cmmetav1.TLSCAKey: ca.Bytes()},
	}
	clientSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-admin-api-client", Namespace: "default"},
		Data: map[string][]byte{
			corev1.TLSCertKey:       clientCert.Bytes(),
			corev1.TLSPrivateKeyKey: clientCert.PrivateKeyBytes(),
		},
	}

	user := &UserAuth{Username: "travis", Password: "password"}

	const dialedFQDN = "test-0.test.default.svc.cluster.local.:9644"

	for name, tc := range map[string]struct {
		adminAPI []vectorizedv1alpha1.AdminAPI
		mutate   func(*vectorizedv1alpha1.Cluster)
		secrets  []client.Object
		server   *tls.Config
		userAuth *UserAuth

		// buildErr is a substring of the error the builder must return; the
		// remaining expectations are skipped when it is set.
		buildErr string
		// callErr is a substring of the error a request must fail with.
		callErr string
		// dialed is the address the client must have asked the dialer for.
		dialed string
		// authorization is the Authorization header the request must carry.
		authorization string
	}{
		"plaintext admin listener": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{plaintext},
			dialed:   dialedFQDN,
		},
		// HeadlessServiceFQDN drops its trailing dot when the Cluster asks for
		// it; the old builder hardcoded the host and ignored the setting.
		"plaintext admin listener without trailing dot": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{plaintext},
			mutate:   func(c *vectorizedv1alpha1.Cluster) { c.Spec.DNSTrailingDotDisabled = true },
			dialed:   "test-0.test.default.svc.cluster.local:9644",
		},
		// Cluster.AdminAPITLS() matches any listener with TLS, internal or
		// external, so this used to be rejected even though the operator only
		// ever talks to the internal listener, which is plaintext here. No
		// secrets exist: the plaintext listener must not trigger a TLS lookup.
		"plaintext internal admin listener alongside a TLS external one": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{plaintext, externalWithTLS},
			dialed:   dialedFQDN,
		},
		// Resolving the cluster certificates walks every API's listeners and
		// reads the Issuers they reference. A plaintext admin listener must not
		// depend on that: this Kafka listener points at an Issuer that does
		// not exist, and the admin client still has to come up.
		"plaintext admin listener does not resolve other listeners' certificates": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{plaintext},
			mutate: func(c *vectorizedv1alpha1.Cluster) {
				c.Spec.Configuration.KafkaAPI[0].TLS = vectorizedv1alpha1.KafkaAPITLS{
					Enabled:   true,
					IssuerRef: &cmmetav1.ObjectReference{Kind: "Issuer", Name: "missing"},
				}
			},
			dialed: dialedFQDN,
		},
		// The user credentials every other builder applies must reach the
		// admin API too, otherwise a Factory built with WithUserAuth talks to
		// V1 clusters anonymously.
		"plaintext admin listener with user auth": {
			adminAPI:      []vectorizedv1alpha1.AdminAPI{plaintext},
			userAuth:      user,
			dialed:        dialedFQDN,
			authorization: "Basic " + base64.StdEncoding.EncodeToString([]byte("travis:password")),
		},
		// A completed request over TLS proves the client dials https, trusts
		// the CA from the node certificate secret, and accepts the SANs the
		// operator mints for the trailing-dot host it derives.
		"TLS admin listener": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{withTLS},
			secrets:  []client.Object{nodeSecret},
			server:   serverTLS,
			dialed:   dialedFQDN,
		},
		// The CA is pinned, not skipped: a server certificate from another CA
		// must fail verification.
		"TLS admin listener with an untrusted server certificate": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{withTLS},
			secrets:  []client.Object{nodeSecret},
			server:   untrustedServerTLS,
			callErr:  "x509",
		},
		// Guards against the TLS provider being stubbed out again: with TLS
		// enabled the builder must resolve the CA, so a missing node
		// certificate secret has to surface as an error.
		"TLS admin listener without the node certificate secret": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{withTLS},
			buildErr: "test-admin-api-node",
		},
		"mutual TLS admin listener": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{withMutualTLS},
			secrets:  []client.Object{nodeSecret, clientSecret},
			server:   mutualTLS,
			dialed:   dialedFQDN,
		},
		"mutual TLS admin listener without the client certificate secret": {
			adminAPI: []vectorizedv1alpha1.AdminAPI{withMutualTLS},
			secrets:  []client.Object{nodeSecret},
			buildErr: "test-admin-api-client",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cluster := &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						RPCServer: vectorizedv1alpha1.SocketAddress{Port: 33145},
						KafkaAPI:  []vectorizedv1alpha1.KafkaAPI{{Port: 9092}},
						AdminAPI:  tc.adminAPI,
					},
				},
			}
			if tc.mutate != nil {
				tc.mutate(cluster)
			}

			// The builder derives one admin URL per broker pod carrying the
			// cluster's labels.
			brokerPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      "test-0",
				Namespace: cluster.Namespace,
				Labels:    labels.ForCluster(cluster),
			}}
			k8sClient := fake.NewClientBuilder().WithScheme(controller.UnifiedScheme).
				WithObjects(append([]client.Object{brokerPod}, tc.secrets...)...).
				Build()

			server := newAdminServer(t, tc.server)
			factory := &Factory{
				mgr:      &stubManager{clients: map[string]client.Client{"v1": k8sClient}},
				dialer:   server.dial,
				userAuth: tc.userAuth,
			}

	t.Run("plaintext internal admin listener alongside a TLS external one", func(t *testing.T) {
		// Cluster.AdminAPITLS() matches any listener with TLS, internal or
		// external, so this used to be rejected even though the operator only
		// ever talks to the internal listener, which is plaintext here.
		cluster := newCluster(
			vectorizedv1alpha1.AdminAPI{Port: 9644},
			vectorizedv1alpha1.AdminAPI{
				Port:     9645,
				External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true},
				TLS:      vectorizedv1alpha1.AdminAPITLS{Enabled: true},
			},
		)

		// No certificate secrets: the plaintext internal listener must not
		// trigger any TLS lookup.
		k8sClient := fake.NewClientBuilder().WithScheme(controller.UnifiedScheme).
			WithObjects(brokerPod(cluster)).
			Build()

		adminClient, err := (&Factory{}).redpandaAdminForV1ClusterWithClient(ctx, cluster, k8sClient)
		require.NoError(t, err)
		require.NotNil(t, adminClient)
		adminClient.Close()
	})

	t.Run("plaintext admin listener", func(t *testing.T) {
		cluster := newCluster(vectorizedv1alpha1.AdminAPI{Port: 9644})

		k8sClient := fake.NewClientBuilder().WithScheme(controller.UnifiedScheme).
			WithObjects(brokerPod(cluster)).
			Build()

		adminClient, err := (&Factory{}).redpandaAdminForV1ClusterWithClient(ctx, cluster, k8sClient)
		require.NoError(t, err)
		require.NotNil(t, adminClient)
		adminClient.Close()
	})
}
