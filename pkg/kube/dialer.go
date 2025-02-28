// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/httpstream"
	httpstreamspdy "k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/spdy"
)

const (
	defaultClusterDomain = "cluster.local"
	defaultNamespace     = "default"
	svcLabel             = "svc"

	portForwardProtocolV1Name = "portforward.k8s.io"
)

var (
	ErrInvalidPodFQDN = errors.New("invalid pod FQDN")
	ErrNoPort         = errors.New("no port specified")

	negotiatedSerializer = serializer.NewCodecFactory(runtime.NewScheme()).WithoutConversion()
)

// PodDialer is a basic port-forwarding dialer that doesn't start
// any local listeners, but returns a net.Conn directly.
type PodDialer struct {
	config        *rest.Config
	clusterDomain string
	requestID     int
}

// NewPodDialer create a PodDialer.
func NewPodDialer(config *rest.Config) *PodDialer {
	return &PodDialer{
		config:        config,
		clusterDomain: defaultClusterDomain,
	}
}

// WithClusterDomain overrides the domain used for FQDN parsing.
func (p *PodDialer) WithClusterDomain(domain string) *PodDialer {
	p.clusterDomain = domain
	return p
}

// DialContext dials the given pod's service-based DNS address and returns a
// net.Conn that can be used to reach the pod directly. It uses the passed in
// context to close the underlying connection when
func (p *PodDialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("dialer only supports TCP-based networks: %w", net.UnknownNetworkError(network))
	}

	pod, port, err := p.parseDNS(address)
	if err != nil {
		return nil, err
	}

	conn, err := p.connectionForPod(pod)
	if err != nil {
		return nil, err
	}

	p.requestID++

	headers := http.Header{}
	headers.Set(corev1.PortHeader, strconv.Itoa(port))
	headers.Set(corev1.PortForwardRequestIDHeader, strconv.Itoa(p.requestID))

	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	errorStream, err := conn.CreateStream(headers)
	if err != nil {
		conn.Close()

		return nil, fmt.Errorf("creating error stream: %w", err)
	}

	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	dataStream, err := conn.CreateStream(headers)
	if err != nil {
		conn.Close()

		return nil, fmt.Errorf("creating data stream: %w", err)
	}

	onClose := func() {
		conn.Close()
	}

	return wrapConn(onClose, network, address, dataStream, errorStream), nil
}

// parseDNS attempts to determine the intended pod to target, currently the
// following formats are supported for resolution of "service-based" hostnames:
//  1. [pod-name].[service-name].[namespace-name].svc.[cluster-domain]
//  2. [pod-name].[service-name].[namespace-name].svc
//  3. [pod-name].[service-name].[namespace-name]
//
// If no cluster-domain is supplied, the dialer's configured domain is assumed.
//
// No validation is done to ensure that the pod is actually referenced by the
// given service or that the DNS record exists in Kubernetes, instead this assumes
// that things are set up properly in Kubernetes such that the FQDN passed here
// matches Kubernetes' own DNS records and a pod within the cluster would be able
// to resolve the same FQDN to the pod that we do.
//
// These assumptions allow us to use this dialer to issues requests *as if* we are
// in the Kubernetes network even from outside of it (i.e. in tests that attempt to
// connect to a pod at a given hostname).
//
// The implementation of parsing for shorter, non-`svc` suffixed domains does not
// follow the typical service DNS scheme. Rather it allows for the following custom
// pod direct-dialing strategy:
//
//  4. [pod-name].[namespace-name]
//  5. [pod-name]
//
// If no namespace-name is supplied, the default namespace is assumed.
func (p *PodDialer) parseDNS(fqdn string) (types.NamespacedName, int, error) {
	var pod types.NamespacedName

	addressPort := strings.Split(fqdn, ":")
	if len(addressPort) != 2 {
		return pod, 0, ErrNoPort
	}

	port, err := strconv.Atoi(addressPort[1])
	if err != nil {
		return pod, 0, ErrNoPort
	}

	fqdn = addressPort[0]

	// Trim any empty labels as our Helm chart-based
	// URLS use FQDNs ending with an empty label
	fqdn = strings.TrimSuffix(fqdn, ".")

	isServiceDNS := true

	if strings.Count(fqdn, ".") < 2 {
		// we have a direct pod DNS address
		isServiceDNS = false
	} else {
		// we have a service-based DNS address
		fqdn = strings.TrimSuffix(fqdn, "."+p.clusterDomain)
		fqdn = strings.TrimSuffix(fqdn, "."+svcLabel)
	}

	labels := strings.Split(fqdn, ".")

	// since we only dial pods we require 2 labels
	// (assuming we're trying to dial a pod in the
	// default namespace) or 3 (for a pod outside
	// of the default namespace)
	switch len(labels) {
	case 1:
		if !isServiceDNS {
			pod.Namespace = defaultNamespace
		} else {
			return pod, 0, ErrInvalidPodFQDN
		}
	case 2:
		if isServiceDNS {
			pod.Namespace = defaultNamespace
		} else {
			pod.Namespace = labels[1]
		}
	case 3:
		pod.Namespace = labels[2]
	default:
		return pod, 0, ErrInvalidPodFQDN
	}

	pod.Name = labels[0]

	return pod, port, nil
}

func (p *PodDialer) connectionForPod(pod types.NamespacedName) (httpstream.Connection, error) {
	transport, upgrader, err := roundTripperFor(p.config)
	if err != nil {
		return nil, err
	}

	cfg := p.config
	cfg.APIPath = "/api"
	cfg.GroupVersion = &schema.GroupVersion{Version: "v1"}
	cfg.NegotiatedSerializer = negotiatedSerializer
	restClient, err := rest.RESTClientFor(cfg)
	if err != nil {
		return nil, err
	}

	req := restClient.Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	conn, protocol, err := dialer.Dial(portForwardProtocolV1Name)
	if err != nil {
		return nil, err
	}

	if protocol != portForwardProtocolV1Name {
		if conn != nil {
			conn.Close()
		}

		return nil, fmt.Errorf("unable to negotiate protocol: client supports %q, server returned %q", portForwardProtocolV1Name, protocol)
	}

	return conn, nil
}

type conn struct {
	dataStream  httpstream.Stream
	errorStream httpstream.Stream
	network     string
	remote      string
	onClose     func()

	errCh  chan error
	closed atomic.Bool
}

var _ net.Conn = (*conn)(nil)

func wrapConn(onClose func(), network, remote string, s, err httpstream.Stream) *conn {
	c := &conn{
		dataStream:  s,
		errorStream: err,
		network:     network,
		remote:      remote,
		onClose:     onClose,
		errCh:       make(chan error, 1),
	}

	go c.pollErrors()

	return c
}

func (c *conn) pollErrors() {
	defer c.Close()

	data, err := io.ReadAll(c.errorStream)
	if err != nil {
		c.writeError(err)
		return
	}

	if len(data) != 0 {
		c.writeError(fmt.Errorf("received error message from error stream: %s", string(data)))
		return
	}
}

func (c *conn) writeError(err error) {
	select {
	case c.errCh <- err:
	default:
	}
}

func (c *conn) checkError() error {
	select {
	case err := <-c.errCh:
		return err
	default:
		if c.closed.Load() {
			return net.ErrClosed
		}
		return nil
	}
}

func (c *conn) Read(data []byte) (int, error) {
	if err := c.checkError(); err != nil {
		return 0, err
	}

	n, err := c.dataStream.Read(data)

	// prioritize any sort of checks propagated on
	// the error stream
	if err := c.checkError(); err != nil {
		return n, err
	}
	return n, err
}

func (c *conn) Write(b []byte) (int, error) {
	if err := c.checkError(); err != nil {
		return 0, err
	}

	n, err := c.dataStream.Write(b)

	// prioritize any sort of checks propagated on
	// the error stream
	if err := c.checkError(); err != nil {
		return n, err
	}
	return n, err
}

func (c *conn) Close() error {
	// make Close idempotent since we may close off
	// the stream when a context is canceled but also
	// may have had Close called manually
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	// call our onClose cleanup handler
	defer c.onClose()

	// closing the underlying connection should cause
	// our error stream reading routine to stop
	_ = c.errorStream.Reset()
	closeErr := c.dataStream.Reset()

	// prioritize any sort of checks propagated on
	// the error stream
	if err := c.checkError(); err != nil {
		if !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	return closeErr
}

func (c *conn) SetDeadline(t time.Time) error {
	return c.dataStream.(net.Conn).SetDeadline(t)
}

func (c *conn) SetReadDeadline(t time.Time) error {
	return c.dataStream.(net.Conn).SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	return c.dataStream.(net.Conn).SetWriteDeadline(t)
}

func (c *conn) LocalAddr() net.Addr {
	return addr{c.network, "localhost:0"}
}

func (c *conn) RemoteAddr() net.Addr {
	return addr{c.network, c.remote}
}

type addr struct {
	Net  string
	Addr string
}

func (a addr) Network() string { return a.Net }
func (a addr) String() string  { return a.Addr }

// roundTripperFor is a re-implementation of [spdy.RoundTripperFor] that
// supports nested PodDialers (vClusters).
// The SpdyRoundTripper implementation makes specifying Proxier (HTTP proxy
// support) and specifying UpgradeTransport (Transport level proxy support)
// mutually exclusive. The "official" implementation of RoundTripperFor opted
// to support only HTTP Proxies.
// This implementation drops the HTTP proxy support in favor of supporting
// Transport layer proxying.
func roundTripperFor(config *rest.Config) (http.RoundTripper, spdy.Upgrader, error) {
	// TransportFor will appropriately aggregate TLSClientConfig and preserve
	// any dialer adjustments for us to pass on to the spdy roundtripper.
	rt, err := rest.TransportFor(config)
	if err != nil {
		return nil, nil, err
	}

	// The original implementation calls rest.TLSConfigFor and passes in nil
	// for UpgradeTransport to permit Proxier being non-nil. The round tripper
	// will extract the TLSConfig and Dial from the transport.
	upgradeRoundTripper, err := httpstreamspdy.NewRoundTripperWithConfig(httpstreamspdy.RoundTripperConfig{
		PingPeriod:       time.Second * 5,
		UpgradeTransport: rt,
	})
	if err != nil {
		return nil, nil, err
	}

	wrapper, err := rest.HTTPWrappersForConfig(config, upgradeRoundTripper)
	if err != nil {
		return nil, nil, err
	}

	// IMPORTANT!! wrapper and upgradeRoundTripper MUST have the same
	// SpdyRoundTripper instance. The RoundTrip call stashes the HTTP
	// connection into itself which is later reused by NewConnection.
	// (Super janky, right? Thanks K8s.)
	// P.S. There's no error checking or synchronization.
	return wrapper, upgradeRoundTripper, nil
}
