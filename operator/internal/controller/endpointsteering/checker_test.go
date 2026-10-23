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
	// adminPort is a port no probe has a say over.
	adminPort = 9644
)

// fakeCluster is the [Cluster] the in-package tests resolve to; the real
// implementation is client.ClusterBrokers, which this package deliberately
// does not import.
type fakeCluster struct {
	podSelector    map[string]string
	clusterDomain  string
	schemaRegistry *schemaregistry.Listener
	// externalPorts are the registry's external listener ports, which the
	// probed internal one gates alongside its own.
	externalPorts []int32
	kafkaPorts    []int32
}

func (f *fakeCluster) PodSelector() map[string]string           { return f.podSelector }
func (f *fakeCluster) ClusterDomain() string                    { return f.clusterDomain }
func (f *fakeCluster) SchemaRegistry() *schemaregistry.Listener { return f.schemaRegistry }
func (f *fakeCluster) KafkaPorts() []int32                      { return f.kafkaPorts }

// SchemaRegistryPorts mirrors the real implementation's invariant: the
// probed listener's port is always in the set.
func (f *fakeCluster) SchemaRegistryPorts() []int32 {
	if f.schemaRegistry == nil {
		return f.externalPorts
	}
	return append([]int32{f.schemaRegistry.Port}, f.externalPorts...)
}

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

// externalRegistry is testBrokers with external listener ports alongside the
// probed internal one.
func externalRegistry(schemaRegistry *schemaregistry.Listener, external ...int32) Cluster {
	brokers := testBrokers(schemaRegistry).(*fakeCluster)
	brokers.externalPorts = external
	return brokers
}

// kafkaBrokers is testBrokers serving Kafka on ports.
func kafkaBrokers(ports ...int32) Cluster {
	brokers := testBrokers(nil).(*fakeCluster)
	brokers.kafkaPorts = ports
	return brokers
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
	c := newChecker(resolver)
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

// toggledListener listens on a loopback port, returning it and a switch that
// takes the listener down, so dials are refused, and back up on the same
// port. Nothing accepts: the kernel completes the handshake regardless.
func toggledListener(t *testing.T) (int32, func(up bool)) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	t.Cleanup(func() {
		if listener != nil {
			_ = listener.Close()
		}
	})
	return int32(listener.Addr().(*net.TCPAddr).Port), func(up bool) {
		t.Helper()
		switch {
		case up && listener == nil:
			listener, err = net.Listen("tcp", address)
			require.NoError(t, err)
		case !up && listener != nil:
			require.NoError(t, listener.Close())
			listener = nil
		}
	}
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
	kafkaUpPort, _ := toggledListener(t)

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
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "failed resolution abstains",
			resolver: &fakeResolver{err: errors.New("secret unavailable")},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Abstain,
		},
		{
			// A resolver breaking its contract must not panic a port-mapper
			// goroutine; it reads as a failed lookup.
			name:     "neither cluster nor error abstains",
			resolver: &fakeResolver{},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Abstain,
		},
		{
			name:     "release-labelled non-broker excludes",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(true),
			pod:      consolePod,
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "not ready pod excluded like the native controller",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(false),
			pod:      brokerPod(false),
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "not ready pod published when the service publishes not-ready addresses",
			resolver: &fakeResolver{brokers: testBrokers(nil)},
			svc:      testService(true),
			pod:      brokerPod(false),
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Include,
		},
		{
			name:     "ready broker included on a non schema registry port",
			resolver: &fakeResolver{brokers: testBrokers(plaintext(readyPort))},
			svc:      testService(false),
			pod:      brokerPod(true),
			port:     tcpPort("admin", adminPort),
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
			// An external listener is the port cloud publishes; leaving it
			// ungated would send clients to a replaying broker through the
			// very Service the internal port protects.
			name:       "external schema registry port is gated by the internal probe",
			resolver:   &fakeResolver{brokers: externalRegistry(plaintext(replayingPort), 8084)},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("schema-default", 8084),
			want:       portmapper.Exclude,
			wantProbes: 1,
		},
		{
			name:       "external schema registry port follows a healthy internal probe",
			resolver:   &fakeResolver{brokers: externalRegistry(plaintext(readyPort), 8084)},
			svc:        testService(true),
			pod:        brokerPod(true),
			port:       tcpPort("schema-default", 8084),
			want:       portmapper.Include,
			wantProbes: 1,
		},
		{
			// With no internal listener there is nothing to probe, and
			// excluding every broker would take the port down.
			name:     "external schema registry port is ungated without an internal listener",
			resolver: &fakeResolver{brokers: externalRegistry(nil, 8084)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("schema-default", 8084),
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
			port: tcpPort("admin", adminPort),
			want: portmapper.Include,
		},
		{
			name:     "kafka listener accepting includes",
			resolver: &fakeResolver{brokers: kafkaBrokers(kafkaUpPort)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", kafkaUpPort),
			want:     portmapper.Include,
		},
		{
			name:     "kafka listener refusing excludes",
			resolver: &fakeResolver{brokers: kafkaBrokers(downPort)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka", downPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "each kafka listener is dialed on its own",
			resolver: &fakeResolver{brokers: kafkaBrokers(kafkaUpPort, downPort)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("kafka-default", downPort),
			want:     portmapper.Exclude,
		},
		{
			name:     "a refusing kafka listener does not affect other ports",
			resolver: &fakeResolver{brokers: kafkaBrokers(downPort)},
			svc:      testService(true),
			pod:      brokerPod(true),
			port:     tcpPort("admin", adminPort),
			want:     portmapper.Include,
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

// TestCheckerDampsProbeFailures pins the damping in both directions: a
// published listener survives fewer than probeFailureThreshold consecutive
// failures, and an unpublished one needs probeSuccessThreshold consecutive
// successes to come back -- so a listener that blinks on and off neither
// drops out nor writes itself back in on every other probe.
func TestCheckerDampsProbeFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		// serve starts a listener of the kind under test and returns the
		// cluster serving it, its port, and a switch for its health.
		serve func(t *testing.T) (Cluster, portmapper.Port, func(healthy bool))
	}{
		{
			name: "schema registry",
			serve: func(t *testing.T) (Cluster, portmapper.Port, func(bool)) {
				var code atomic.Int32
				code.Store(http.StatusOK)
				port := schemaRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(int(code.Load()))
				})
				return testBrokers(&schemaregistry.Listener{Port: port}), tcpPort("registry", port), func(healthy bool) {
					if healthy {
						code.Store(http.StatusOK)
					} else {
						code.Store(http.StatusServiceUnavailable)
					}
				}
			},
		},
		{
			name: "kafka",
			serve: func(t *testing.T) (Cluster, portmapper.Port, func(bool)) {
				port, toggle := toggledListener(t)
				return kafkaBrokers(port), tcpPort("kafka", port), toggle
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brokers, port, setHealthy := tc.serve(t)
			checker := newTestChecker(&fakeResolver{brokers: brokers})
			svc, pod := testService(true), brokerPod(true)
			decide := func() portmapper.Decision {
				return checker.Decide(t.Context(), svc, pod, port)
			}

			require.Equal(t, portmapper.Include, decide(), "the first probe is taken as-is")

			setHealthy(false)
			for i := 1; i < probeFailureThreshold; i++ {
				require.Equal(t, portmapper.Include, decide(), "failure %d of %d must not unpublish", i, probeFailureThreshold)
			}
			require.Equal(t, portmapper.Exclude, decide(), "failure %d unpublishes", probeFailureThreshold)

			setHealthy(true)
			for i := 1; i < probeSuccessThreshold; i++ {
				require.Equal(t, portmapper.Exclude, decide(), "success %d of %d must not republish", i, probeSuccessThreshold)
			}
			require.Equal(t, portmapper.Include, decide(), "success %d republishes", probeSuccessThreshold)

			// A listener alternating between answering and not stays where
			// it is: neither run of agreement is long enough to flip it.
			for range 3 {
				setHealthy(false)
				require.Equal(t, portmapper.Include, decide())
				setHealthy(true)
				require.Equal(t, portmapper.Include, decide())
			}
		})
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
		{name: "refused dial", err: &net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, want: portmapper.Exclude},
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
