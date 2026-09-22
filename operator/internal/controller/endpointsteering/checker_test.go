// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/portmapper"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
)

const (
	testCluster   = "rp"
	testNamespace = "ns"
	kafkaPort     = 9093
)

// fakeCluster is the [Cluster] the in-package tests resolve to; the real
// implementation is client.ClusterBrokers, which this package deliberately
// does not import.
type fakeCluster struct {
	podSelector    map[string]string
	clusterDomain  string
	schemaRegistry *schemaregistry.Listener
}

func (f *fakeCluster) PodSelector() map[string]string           { return f.podSelector }
func (f *fakeCluster) ClusterDomain() string                    { return f.clusterDomain }
func (f *fakeCluster) SchemaRegistry() *schemaregistry.Listener { return f.schemaRegistry }

type fakeResolver struct {
	calls   atomic.Int32
	brokers Cluster
	err     error
}

func (f *fakeResolver) Resolve(context.Context, string, string) (Cluster, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.brokers, nil
}

func testBrokers(schemaRegistry *schemaregistry.Listener) Cluster {
	return &fakeCluster{
		podSelector:    map[string]string{"app.kubernetes.io/instance": testCluster, "app.kubernetes.io/name": "redpanda"},
		clusterDomain:  "cluster.local",
		schemaRegistry: schemaRegistry,
	}
}

func brokerPod(ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testCluster + "-0",
			Namespace: testNamespace,
			UID:       "pod-0",
			Labels:    map[string]string{"app.kubernetes.io/instance": testCluster, "app.kubernetes.io/name": "redpanda"},
		},
		Spec: corev1.PodSpec{Hostname: testCluster + "-0", Subdomain: testCluster},
		Status: corev1.PodStatus{
			PodIP:      "127.0.0.1",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}},
		},
	}
}

func testService(publishNotReady bool) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc",
			Namespace:   testNamespace,
			UID:         "svc",
			Annotations: map[string]string{ServiceAnnotation: testCluster},
		},
		Spec: corev1.ServiceSpec{PublishNotReadyAddresses: publishNotReady},
	}
}

func tcpPort(name string, port int32) portmapper.Port {
	return portmapper.Port{Name: name, Port: port, Protocol: corev1.ProtocolTCP, Address: "127.0.0.1"}
}

func newTestChecker(resolver Resolver) *checker {
	c := newChecker(resolver, "cluster.local")
	// Hanging probes are part of the test matrix; don't wait the production
	// 5s for them.
	c.probeTimeout = 300 * time.Millisecond
	return c
}

// schemaRegistryServer serves handler and returns the port it listens on, so
// tests can declare it as the cluster's Schema Registry port.
func schemaRegistryServer(t *testing.T, handler http.HandlerFunc) int32 {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return int32(server.Listener.Addr().(*net.TCPAddr).Port)
}

// tlsSchemaRegistryServer serves handler over TLS with a certificate issued
// for dnsName -- the name the checker is expected to verify against.
func tlsSchemaRegistryServer(t *testing.T, dnsName string, handler http.HandlerFunc) (int32, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: cert}},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return int32(server.Listener.Addr().(*net.TCPAddr).Port), pool
}

func statusHandler(code int, hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(code)
	}
}

// closedPort returns a port nothing listens on.
func closedPort(t *testing.T) int32 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return int32(port)
}

func TestCheckerDecide(t *testing.T) {
	var hits atomic.Int32
	readyPort := schemaRegistryServer(t, statusHandler(http.StatusOK, &hits))
	replayingPort := schemaRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// /status/ready blocks while the store replays; the client's timeout
		// is the only way out.
		<-r.Context().Done()
	})
	unhealthyPort := schemaRegistryServer(t, statusHandler(http.StatusServiceUnavailable, &hits))
	downPort := closedPort(t)

	podDNSName := testCluster + "-0." + testCluster + "." + testNamespace + ".svc.cluster.local"
	tlsPort, tlsPool := tlsSchemaRegistryServer(t, podDNSName, statusHandler(http.StatusOK, &hits))

	plaintext := func(port int32) *schemaregistry.Listener {
		return &schemaregistry.Listener{Port: port}
	}
	withTLS := func(port int32, config *tls.Config) *schemaregistry.Listener {
		return &schemaregistry.Listener{Port: port, TLSConfig: func(context.Context) (*tls.Config, error) { return config, nil }}
	}

	consolePod := brokerPod(true)
	consolePod.Labels["app.kubernetes.io/name"] = "console"

	for _, tc := range []struct {
		name       string
		resolver   Resolver
		svc        *corev1.Service
		pod        *corev1.Pod
		port       portmapper.Port
		want       portmapper.Decision
		wantProbes int32
	}{
		{
			name:     "unknown cluster excludes",
			resolver: &fakeResolver{err: ErrUnknownCluster},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "failed resolution abstains",
			resolver: &fakeResolver{err: errors.New("secret unavailable")},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Abstain,
		},
		{
			// A resolver breaking its contract must not panic a port-mapper
			// goroutine; it reads as a failed lookup.
			name:     "neither cluster nor error abstains",
			resolver: &fakeResolver{},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Abstain,
		},
		{
			name:     "release-labelled non-broker excludes",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(true),
			pod:      consolePod,
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "not ready pod excluded like the native controller",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(false),
			pod:      brokerPod(false),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "not ready pod published when the service publishes not-ready addresses",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(true),
			pod:      brokerPod(false),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Include,
		},
		{
			name:     "ready broker included on a non schema registry port",
			resolver: &fakeResolver{brokers: testBrokers(plaintext(readyPort))},
			svc:      testService(false),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", kafkaPort),
			want:     portmapper.Include,
		},
		{
			name:       "schema registry answering ready includes",
			resolver:   &fakeResolver{brokers: testBrokers(plaintext(readyPort))},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("registry", readyPort),
			want:       portmapper.Include,
			wantProbes: 1,
		},
		{
			name:       "schema registry answering unhealthy excludes",
			resolver:   &fakeResolver{brokers: testBrokers(plaintext(unhealthyPort))},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("registry", unhealthyPort),
			want:       portmapper.Exclude,
			wantProbes: 1,
		},
		{
			name:       "schema registry still replaying excludes",
			resolver:   &fakeResolver{brokers: testBrokers(plaintext(replayingPort))},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("registry", replayingPort),
			want:       portmapper.Exclude,
			wantProbes: 1,
		},
		{
			name:     "schema registry not listening excludes",
			resolver: &fakeResolver{brokers: testBrokers(plaintext(downPort))},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("registry", downPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "schema registry port of a not ready pod is excluded without probing",
			resolver: &fakeResolver{brokers: testBrokers(plaintext(readyPort))},
			svc:      testService(false),
			pod:      brokerPod(false),
			port:     tcpPort("registry", readyPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "schema registry port on a cluster without the listener is an ordinary port",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("registry", downPort),
			want:     portmapper.Include,
		},
		{
			name:       "tls listener verified against the pod's DNS name includes",
			resolver:   &fakeResolver{brokers: testBrokers(withTLS(tlsPort, &tls.Config{RootCAs: tlsPool, MinVersion: tls.VersionTLS12}))},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("registry", tlsPort),
			want:       portmapper.Include,
			wantProbes: 1,
		},
		{
			name:     "tls listener failing verification abstains",
			resolver: &fakeResolver{brokers: testBrokers(withTLS(tlsPort, &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}))},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("registry", tlsPort),
			want:     portmapper.Abstain,
		},
		{
			// Certificates that can't be read yet must not take the
			// cluster's other ports down with them.
			name: "unreadable listener certificates abstain on the schema registry port only",
			resolver: &fakeResolver{brokers: testBrokers(&schemaregistry.Listener{Port: tlsPort, TLSConfig: func(context.Context) (*tls.Config, error) {
				return nil, errors.New("certificate not found")
			}})},
			svc:  testService(true),
			pod:  brokerPod(true),
			port: tcpPort("registry", tlsPort),
			want: portmapper.Abstain,
		},
		{
			name: "unreadable listener certificates do not affect other ports",
			resolver: &fakeResolver{brokers: testBrokers(&schemaregistry.Listener{Port: tlsPort, TLSConfig: func(context.Context) (*tls.Config, error) {
				return nil, errors.New("certificate not found")
			}})},
			svc:  testService(true),
			pod:  brokerPod(true),
			port: tcpPort("kafka", kafkaPort),
			want: portmapper.Include,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits.Store(0)
			checker := newTestChecker(tc.resolver)
			require.Equal(t, tc.want, checker.Decide(t.Context(), tc.svc, tc.pod, tc.port))
			require.Equal(t, tc.wantProbes, hits.Load(), "unexpected number of probes")
		})
	}
}

// TestCheckerDampsSchemaRegistryFailures pins the damping in both
// directions: a published Schema Registry survives fewer than
// probeFailureThreshold consecutive failures, and an unpublished one needs
// probeSuccessThreshold consecutive successes to come back -- so a listener
// that blinks on and off neither drops out nor writes itself back in on
// every other probe.
func TestCheckerDampsSchemaRegistryFailures(t *testing.T) {
	var code atomic.Int32
	code.Store(http.StatusOK)
	port := schemaRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(code.Load()))
	})

	checker := newTestChecker(&fakeResolver{brokers: testBrokers(&schemaregistry.Listener{Port: port})})
	svc, pod, registry := testService(true), brokerPod(true), tcpPort("registry", port)
	decide := func() portmapper.Decision {
		return checker.Decide(t.Context(), svc, pod, registry)
	}

	require.Equal(t, portmapper.Include, decide(), "the first probe is taken as-is")

	code.Store(http.StatusServiceUnavailable)
	for i := 1; i < probeFailureThreshold; i++ {
		require.Equal(t, portmapper.Include, decide(), "failure %d of %d must not unpublish", i, probeFailureThreshold)
	}
	require.Equal(t, portmapper.Exclude, decide(), "failure %d unpublishes", probeFailureThreshold)

	code.Store(http.StatusOK)
	for i := 1; i < probeSuccessThreshold; i++ {
		require.Equal(t, portmapper.Exclude, decide(), "success %d of %d must not republish", i, probeSuccessThreshold)
	}
	require.Equal(t, portmapper.Include, decide(), "success %d republishes", probeSuccessThreshold)

	// A listener alternating between answering and not stays where it is:
	// neither run of agreement is long enough to flip it.
	for range 3 {
		code.Store(http.StatusServiceUnavailable)
		require.Equal(t, portmapper.Include, decide())
		code.Store(http.StatusOK)
		require.Equal(t, portmapper.Include, decide())
	}
}

func TestClassifyProbeError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want portmapper.Decision
	}{
		{name: "deadline exceeded is still replaying", err: context.DeadlineExceeded, want: portmapper.Exclude},
		{name: "wrapped deadline exceeded", err: &url.Error{Op: "Get", Err: context.DeadlineExceeded}, want: portmapper.Exclude},
		{name: "dial timeout", err: &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ETIMEDOUT}}, want: portmapper.Exclude},
		{name: "connection refused is the listener being down", err: &url.Error{Op: "Get", Err: &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}}, want: portmapper.Exclude},
		{name: "connection reset", err: &net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}, want: portmapper.Exclude},
		{name: "server hung up", err: io.EOF, want: portmapper.Exclude},
		{name: "truncated response", err: io.ErrUnexpectedEOF, want: portmapper.Exclude},
		{name: "certificate verification says nothing about the pod", err: &url.Error{Op: "Get", Err: x509.UnknownAuthorityError{}}, want: portmapper.Abstain},
		{name: "no route is the controller's problem", err: &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}}, want: portmapper.Abstain},
		{name: "anything else abstains", err: errors.New("boom"), want: portmapper.Abstain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyProbeError(tc.err))
		})
	}
}

func TestCheckerBrokersCache(t *testing.T) {
	brokers := testBrokers(nil)
	resolver := &fakeResolver{brokers: brokers}
	checker := newTestChecker(resolver)
	now := time.Now()
	checker.now = func() time.Time { return now }

	for range 3 {
		got, err := checker.brokers(t.Context(), testNamespace, testCluster)
		require.NoError(t, err)
		require.Same(t, brokers, got)
	}
	require.EqualValues(t, 1, resolver.calls.Load(), "lookups within the TTL are served from the cache")

	now = now.Add(clusterCacheTTL + time.Second)
	_, err := checker.brokers(t.Context(), testNamespace, testCluster)
	require.NoError(t, err)
	require.EqualValues(t, 2, resolver.calls.Load(), "an expired entry is resolved again")

	// A failure is cached too, briefly: an unreadable cluster must not be
	// re-resolved once per pod per port. The error surfaces so the caller
	// abstains, which is what keeps the last published memberships.
	now = now.Add(clusterCacheTTL + time.Second)
	resolver.err = errors.New("cluster unreadable")
	for range 3 {
		_, err := checker.brokers(t.Context(), testNamespace, testCluster)
		require.ErrorContains(t, err, "cluster unreadable")
	}
	require.EqualValues(t, 3, resolver.calls.Load(), "a cached failure is not re-resolved")

	// And it is re-tried soon, so a cluster that becomes readable again
	// doesn't wait out the success TTL.
	now = now.Add(clusterFailureTTL + time.Second)
	resolver.err = nil
	got, err := checker.brokers(t.Context(), testNamespace, testCluster)
	require.NoError(t, err)
	require.Same(t, brokers, got)
	require.EqualValues(t, 4, resolver.calls.Load())
}
