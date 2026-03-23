// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package leaderelection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

type peer struct {
	addr   string
	client transportv1.TransportServiceClient
}

func newPeer(addr string, credentials credentials.TransportCredentials, extraOpts ...grpc.DialOption) (*peer, error) {
	if credentials == nil {
		credentials = insecure.NewCredentials()
	}
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(credentials)}, extraOpts...)
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, err
	}

	return &peer{
		addr:   addr,
		client: transportv1.NewTransportServiceClient(conn),
	}, nil
}

// InsecureClientFor creates a gRPC transport client for the given node,
// using TLS with InsecureSkipVerify when the config is not fully insecure.
func InsecureClientFor(config LockConfiguration, node LockerNode) (transportv1.TransportServiceClient, error) {
	var err error
	var credentials credentials.TransportCredentials

	if config.Insecure {
		credentials = insecure.NewCredentials()
	} else {
		credentials, err = clientTLSConfig(config.Certificate, config.PrivateKey, config.CA, true)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to initialize client credentials: %w", err)
	}

	conn, err := grpc.NewClient(node.Address, grpc.WithTransportCredentials(credentials))
	if err != nil {
		return nil, err
	}
	return transportv1.NewTransportServiceClient(conn), nil
}

// ClientFor creates a gRPC transport client for the given node using the
// TLS configuration from config.
func ClientFor(config LockConfiguration, node LockerNode) (transportv1.TransportServiceClient, error) {
	var err error
	var creds credentials.TransportCredentials

	if config.Insecure {
		creds = insecure.NewCredentials()
	} else if len(config.ServerTLSOptions) == 0 || len(config.ClientTLSOptions) == 0 {
		creds, err = clientTLSConfig(config.Certificate, config.PrivateKey, config.CA)
	} else {
		clientTLSConfig := &tls.Config{} // nolint:gosec // the tls version is configurable by calling code
		for _, opt := range config.ClientTLSOptions {
			opt(clientTLSConfig)
		}
		creds = credentials.NewTLS(clientTLSConfig)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to initialize client credentials: %w", err)
	}

	conn, err := grpc.NewClient(node.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return transportv1.NewTransportServiceClient(conn), nil
}

// KubeconfigFetcher retrieves a kubeconfig for the local cluster, used in
// bootstrap mode to share credentials with the raft leader.
type KubeconfigFetcher interface {
	Fetch(context.Context) ([]byte, error)
}

// KubeconfigFetcherFn is a function adapter for KubeconfigFetcher.
type KubeconfigFetcherFn func(context.Context) ([]byte, error)

func (fn KubeconfigFetcherFn) Fetch(ctx context.Context) ([]byte, error) {
	return fn(ctx)
}

type grpcTransport struct {
	addr  string
	peers map[uint64]*peer
	meta  []byte

	leader   atomic.Uint64
	isLeader atomic.Bool

	node     raft.Node
	nodeLock sync.RWMutex

	// storage is set by runRaft after creating MemoryStorage so that the Send
	// handler can consult LastIndex before stepping heartbeat messages.
	storage     *raft.MemoryStorage
	storageLock sync.RWMutex

	kubeconfigFetcher KubeconfigFetcher

	serverCredentials credentials.TransportCredentials
	clientCredentials credentials.TransportCredentials
	extraDialOptions  []grpc.DialOption

	logger raft.Logger

	transportv1.UnimplementedTransportServiceServer
}

// backoffDialOption returns a grpc.DialOption that caps the exponential
// backoff between reconnection attempts at maxDelay. If maxDelay is zero
// the gRPC library default is used and nil is returned.
func backoffDialOption(maxDelay time.Duration) []grpc.DialOption {
	if maxDelay <= 0 {
		return nil
	}
	bc := backoff.DefaultConfig
	bc.MaxDelay = maxDelay
	return []grpc.DialOption{grpc.WithConnectParams(grpc.ConnectParams{Backoff: bc})}
}

func newGRPCTransport(meta []byte, certPEM, keyPEM, caPEM []byte, addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff time.Duration) (*grpcTransport, error) {
	serverCredentials, err := serverTLSConfig(certPEM, keyPEM, caPEM)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize server credentials: %w", err)
	}
	clientCredentials, err := clientTLSConfig(certPEM, keyPEM, caPEM)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize client credentials: %w", err)
	}

	extraOpts := backoffDialOption(grpcMaxBackoff)
	initializedPeers := make(map[uint64]*peer, len(peers))
	for id, peer := range peers {
		initialized, err := newPeer(peer, clientCredentials, extraOpts...)
		if err != nil {
			return nil, err
		}
		initializedPeers[id] = initialized
	}

	return &grpcTransport{
		meta:              meta,
		addr:              addr,
		peers:             initializedPeers,
		serverCredentials: serverCredentials,
		clientCredentials: clientCredentials,
		extraDialOptions:  extraOpts,
		kubeconfigFetcher: fetcher,
	}, nil
}

func newGRPCTransportWithOptions(meta []byte, serverOptions, clientOptions []func(*tls.Config), addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff time.Duration) (*grpcTransport, error) {
	serverTLSConfig := &tls.Config{} // nolint:gosec // the tls version is configurable by calling code
	for _, opt := range serverOptions {
		opt(serverTLSConfig)
	}
	serverCredentials := credentials.NewTLS(serverTLSConfig)

	clientTLSConfig := &tls.Config{} // nolint:gosec // the tls version is configurable by calling code
	for _, opt := range clientOptions {
		opt(clientTLSConfig)
	}
	clientCredentials := credentials.NewTLS(clientTLSConfig)

	extraOpts := backoffDialOption(grpcMaxBackoff)
	initializedPeers := make(map[uint64]*peer, len(peers))
	for id, peer := range peers {
		initialized, err := newPeer(peer, clientCredentials, extraOpts...)
		if err != nil {
			return nil, err
		}
		initializedPeers[id] = initialized
	}

	return &grpcTransport{
		meta:              meta,
		addr:              addr,
		peers:             initializedPeers,
		serverCredentials: serverCredentials,
		clientCredentials: clientCredentials,
		extraDialOptions:  extraOpts,
		kubeconfigFetcher: fetcher,
	}, nil
}

func newInsecureGRPCTransport(meta []byte, addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff time.Duration) (*grpcTransport, error) {
	extraOpts := backoffDialOption(grpcMaxBackoff)
	initializedPeers := make(map[uint64]*peer, len(peers))
	for id, peer := range peers {
		initialized, err := newPeer(peer, nil, extraOpts...)
		if err != nil {
			return nil, err
		}
		initializedPeers[id] = initialized
	}

	return &grpcTransport{
		meta:              meta,
		addr:              addr,
		peers:             initializedPeers,
		extraDialOptions:  extraOpts,
		kubeconfigFetcher: fetcher,
	}, nil
}

func (t *grpcTransport) setNode(node raft.Node) {
	t.nodeLock.Lock()
	defer t.nodeLock.Unlock()
	t.node = node
}

func (t *grpcTransport) getNode() raft.Node {
	t.nodeLock.RLock()
	defer t.nodeLock.RUnlock()
	return t.node
}

func (t *grpcTransport) setStorage(s *raft.MemoryStorage) {
	t.storageLock.Lock()
	defer t.storageLock.Unlock()
	t.storage = s
}

func (t *grpcTransport) getStorage() *raft.MemoryStorage {
	t.storageLock.RLock()
	defer t.storageLock.RUnlock()
	return t.storage
}

func (t *grpcTransport) client() (transportv1.TransportServiceClient, error) {
	peer, err := newPeer(t.addr, t.clientCredentials, t.extraDialOptions...)
	if err != nil {
		return nil, err
	}

	return peer.client, nil
}

func (t *grpcTransport) DoSend(ctx context.Context, msg raftpb.Message) (bool, error) {
	peer, ok := t.peers[msg.To]
	if !ok {
		return false, fmt.Errorf("unknown peer %d", msg.To)
	}

	data, err := msg.Marshal()
	if err != nil {
		return false, fmt.Errorf("marshaling message for peer %q: %w", peer.addr, err)
	}

	resp, err := peer.client.Send(ctx, &transportv1.SendRequest{
		Payload: data,
	})
	if err != nil {
		return false, fmt.Errorf("sending to peer %q: %w", peer.addr, err)
	}

	return resp.Applied, nil
}

func (t *grpcTransport) Send(ctx context.Context, req *transportv1.SendRequest) (*transportv1.SendResponse, error) {
	if node := t.getNode(); node != nil {
		var msg raftpb.Message
		if err := msg.Unmarshal(req.Payload); err != nil {
			return &transportv1.SendResponse{Applied: false}, nil
		}

		// Guard against "tocommit(X) is out of range [lastIndex(Y)]" panic.
		//
		// When a node restarts fresh, raft.StartNode populates its log with N
		// ConfChange entries (N = number of peers), so lastIndex = N. If the
		// existing cluster has already committed beyond N, the raft library's
		// handleHeartbeat will call commitTo(Commit) and panic because
		// Commit > lastIndex.
		//
		// We intercept such heartbeats before passing them to node.Step and
		// return Applied=false, signalling the leader to retry on the next
		// heartbeat tick. Meanwhile the leader discovers the follower is
		// lagging via MsgApp rejection and sends a MsgSnap, after which
		// lastIndex advances to the snapshot index and heartbeats are safe.
		if msg.Type == raftpb.MsgHeartbeat {
			if s := t.getStorage(); s != nil {
				if lastIdx, err := s.LastIndex(); err == nil && msg.Commit > lastIdx {
					return &transportv1.SendResponse{Applied: false}, nil
				}
			}
		}

		// When this node is the leader and it receives any message from a
		// peer, step a synthetic MsgHeartbeatResp. This reactivates the
		// peer's progress tracker (sets RecentActive=true, ProbeSent=false)
		// which is critical when a peer restarts fresh: raft marks inactive
		// peers and stops producing messages for them, but MsgVote (the only
		// message a fresh peer can send) doesn't touch the progress tracker.
		// Without this, the leader and restarted peer deadlock: the leader
		// won't send because the peer is inactive, and the peer can't become
		// active because it never receives from the leader.
		if t.isLeader.Load() && msg.From != 0 {
			_ = node.Step(ctx, raftpb.Message{
				Type: raftpb.MsgHeartbeatResp,
				From: msg.From,
			})
		}

		err := node.Step(ctx, msg)
		if err == nil {
			return &transportv1.SendResponse{Applied: true}, nil
		}
		t.logger.Errorf("error occurred: %v", err)
	}
	return &transportv1.SendResponse{Applied: false}, nil
}

func (t *grpcTransport) Check(ctx context.Context, req *transportv1.CheckRequest) (*transportv1.CheckResponse, error) {
	leader := t.leader.Load()
	isLeader := t.isLeader.Load()
	if leader != 0 {
		response := &transportv1.CheckResponse{HasLeader: true, Meta: t.meta}
		if !req.FromLeader && isLeader {
			for id, peer := range t.peers {
				followerResp, err := peer.client.Check(ctx, &transportv1.CheckRequest{
					FromLeader: true,
				})
				if err != nil || !followerResp.HasLeader {
					response.UnhealthyNodes = append(response.UnhealthyNodes, id)
				}
			}
		} else if !req.FromLeader {
			peer, ok := t.peers[leader]
			if !ok {
				return &transportv1.CheckResponse{HasLeader: false, Meta: t.meta}, nil
			}
			return peer.client.Check(ctx, &transportv1.CheckRequest{})
		}

		return response, nil
	}

	return &transportv1.CheckResponse{HasLeader: false, Meta: t.meta}, nil
}

func (t *grpcTransport) Kubeconfig(ctx context.Context, req *transportv1.KubeconfigRequest) (*transportv1.KubeconfigResponse, error) {
	if t.kubeconfigFetcher == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no kubeconfig fetcher specified")
	}
	data, err := t.kubeconfigFetcher.Fetch(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s", err.Error())
	}

	return &transportv1.KubeconfigResponse{Payload: data}, nil
}

func (t *grpcTransport) Run(ctx context.Context) error {
	defer t.logger.Info("shutting down grpc transport")

	credentials := t.serverCredentials
	if credentials == nil {
		credentials = insecure.NewCredentials()
	}

	server := grpc.NewServer(grpc.Creds(credentials))
	transportv1.RegisterTransportServiceServer(server, t)

	lis, err := net.Listen("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", t.addr, err)
	}
	defer lis.Close()

	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(done)

		if err := server.Serve(lis); err != nil {
			errs <- err
		}
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	case err := <-errs:
		server.Stop()
		return err
	}
}

func serverTLSConfig(certPEM, keyPEM, caPEM []byte) (credentials.TransportCredentials, error) {
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	capool := x509.NewCertPool()
	if !capool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("unable to append the CA certificate to CA pool")
	}

	tlsConfig := &tls.Config{ // nolint:gosec // linter complains about TLS min version, we pin all our certs here though, so ignore it
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    capool,
	}
	return credentials.NewTLS(tlsConfig), nil
}

func clientTLSConfig(certPEM, keyPEM, caPEM []byte, insecure ...bool) (credentials.TransportCredentials, error) {
	isInsecure := false
	if len(insecure) > 0 {
		isInsecure = insecure[0]
	}

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	capool := x509.NewCertPool()
	if !capool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("unable to append the CA certificate to CA pool")
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{certificate},
		RootCAs:            capool,
		InsecureSkipVerify: isInsecure, // nolint:gosec
	}
	return credentials.NewTLS(tlsConfig), nil
}
