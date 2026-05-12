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
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

type peer struct {
	addr   string
	client transportv1.TransportServiceClient

	// sendCh feeds the per-peer worker goroutine. Bounded; full-queue
	// drops are silently absorbed because raft re-sends every heartbeat
	// tick anyway. Size chosen to comfortably absorb one burst of catch-
	// up MsgApp entries for a fresh follower without dropping.
	sendCh chan raftpb.Message
}

// peerSendQueueSize is the bounded capacity of each peer's send queue.
// Sized to absorb one burst of catch-up entries for a fresh follower
// (typical size: a few dozen) without dropping.
const peerSendQueueSize = 256

// raftKeepaliveParams are baseline HTTP/2 keepalive settings applied to every
// peer gRPC connection. Without these, a silently black-holed TCP link (no
// RST, packets dropped) would keep the connection "open" until the Linux TCP
// retransmit ceiling (tcp_retries2 ≈ 15 minutes by default) and every
// subsequent DoSend from the raft Ready loop would block on that orphan
// stream. The keepalive closes the stream within Time + Timeout of the first
// missed pong and a fresh connection is dialed on the next send, so the raft
// loop recovers quickly even when the peer's TCP stack never notifies us.
var raftKeepaliveParams = keepalive.ClientParameters{
	Time:                10 * time.Second, // send a ping every 10s on idle
	Timeout:             3 * time.Second,  // close if no pong in 3s
	PermitWithoutStream: true,             // ping even when no active streams
}

func newPeer(addr string, credentials credentials.TransportCredentials, extraOpts ...grpc.DialOption) (*peer, error) {
	if credentials == nil {
		credentials = insecure.NewCredentials()
	}
	// Baseline options first; caller-supplied extraOpts can override.
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials),
		grpc.WithKeepaliveParams(raftKeepaliveParams),
	}
	opts = append(opts, extraOpts...)
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, err
	}

	return &peer{
		addr:   addr,
		client: transportv1.NewTransportServiceClient(conn),
		sendCh: make(chan raftpb.Message, peerSendQueueSize),
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

	raftState  atomic.Value // stores string
	term       atomic.Uint64
	idsToNames map[uint64]string
	localID    uint64

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

	// testHooks is nil in production. Chaos tests wire it in via the
	// LockConfiguration so they can inject faults (currently
	// BlockIngress) without touching the production code path.
	testHooks *TestHooks

	// sendTimeout bounds a single peer RPC issued by the per-peer worker
	// goroutine. Without a ceiling, a silently black-holed TCP stream
	// would block the worker until Linux TCP retransmit fires (~15 min).
	// Zero applies defaultSendTimeout. Callers thread in the configured
	// HeartbeatInterval so the worker can't sit on one send longer than
	// one heartbeat tick.
	sendTimeout time.Duration

	logger raft.Logger

	transportv1.UnimplementedTransportServiceServer
}

// defaultSendTimeout is the fallback ceiling for a single peer RPC when
// the grpcTransport is constructed with sendTimeout=0.
const defaultSendTimeout = 1 * time.Second

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

func newGRPCTransport(meta []byte, certPEM, keyPEM, caPEM []byte, addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff, sendTimeout time.Duration) (*grpcTransport, error) {
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
		sendTimeout:       sendTimeout,
	}, nil
}

func newGRPCTransportWithOptions(meta []byte, serverOptions, clientOptions []func(*tls.Config), addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff, sendTimeout time.Duration) (*grpcTransport, error) {
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
		sendTimeout:       sendTimeout,
	}, nil
}

func newInsecureGRPCTransport(meta []byte, addr string, peers map[uint64]string, fetcher KubeconfigFetcher, grpcMaxBackoff, sendTimeout time.Duration) (*grpcTransport, error) {
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
		sendTimeout:       sendTimeout,
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

// DoSend issues one Send RPC synchronously and returns the applied flag
// and any error. It is called from the per-peer worker goroutine
// (runPeerSender) and — for MsgSnap fallbacks — directly by the worker's
// snapshot path. The supplied ctx is wrapped with sendTimeout so a
// silently black-holed stream can't stall longer than one heartbeat
// tick regardless of the caller's ctx.
func (t *grpcTransport) DoSend(ctx context.Context, msg raftpb.Message) (bool, error) {
	peer, ok := t.peers[msg.To]
	if !ok {
		return false, fmt.Errorf("unknown peer %d", msg.To)
	}

	label := peerLabel(t, msg.To)

	// Track concurrent sends per peer; the histogram captures wall-clock
	// duration of one DoSend call so cross-region RTT dispersion is
	// visible to investigators.
	inflightRPCs.WithLabelValues(label).Inc()
	start := time.Now()
	defer func() {
		inflightRPCs.WithLabelValues(label).Dec()
	}()

	data, err := msg.Marshal()
	if err != nil {
		sendErrorsTotal.WithLabelValues(label, "marshal").Inc()
		sendDurationSeconds.WithLabelValues(label, "error").Observe(time.Since(start).Seconds())
		return false, fmt.Errorf("marshaling message for peer %q: %w", peer.addr, err)
	}

	timeout := t.sendTimeout
	if timeout <= 0 {
		timeout = defaultSendTimeout
	}
	sendCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := peer.client.Send(sendCtx, &transportv1.SendRequest{
		Payload: data,
	})
	if err != nil {
		sendErrorsTotal.WithLabelValues(label, sendErrorClass(err)).Inc()
		sendDurationSeconds.WithLabelValues(label, "error").Observe(time.Since(start).Seconds())
		return false, fmt.Errorf("sending to peer %q: %w", peer.addr, err)
	}

	sendDurationSeconds.WithLabelValues(label, "ok").Observe(time.Since(start).Seconds())
	return resp.Applied, nil
}

// sendErrorClass maps a DoSend error to one of a fixed six-value
// vocabulary for the `error_type` label, so cardinality stays small and
// each bucket maps to a different on-call story:
//
//   - timeout:     DeadlineExceeded — peer silent or slow.
//   - canceled:    Canceled — usually graceful shutdown.
//   - unavailable: Unavailable — connection refused / dial failure / peer down.
//   - auth:        Unauthenticated, PermissionDenied — TLS or RBAC misconfig.
//   - marshal:     Local serialisation failure (raftpb.Message.Marshal).
//   - other:       Anything else (rare — e.g. an unforeseen gRPC code).
//
// Callers that detect a marshal failure pass "marshal" directly without
// going through this function. Everything else flows here.
func sendErrorClass(err error) string {
	if err == nil {
		return "other"
	}
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.DeadlineExceeded:
			return "timeout"
		case codes.Canceled:
			return "canceled"
		case codes.Unavailable:
			return "unavailable"
		case codes.Unauthenticated, codes.PermissionDenied:
			return "auth"
		}
	}
	return "other"
}

// EnqueueSend hands a raftpb.Message off to the destination peer's
// worker goroutine. Non-blocking: if the peer's send queue is full, the
// message is dropped and raft will retransmit on the next tick. This is
// the only send path the raft Ready loop uses — decoupling the loop
// from any single slow peer. Drops are counted by the
// `raft_messages_dropped_total{peer}` metric so chronic saturation is
// observable via standard alerting (`rate(... > X)`).
func (t *grpcTransport) EnqueueSend(msg raftpb.Message) {
	peer, ok := t.peers[msg.To]
	if !ok {
		return
	}
	label := peerLabel(t, msg.To)
	select {
	case peer.sendCh <- msg:
		messagesSentTotal.WithLabelValues(msgTypeLabel(msg.Type), label).Inc()
	default:
		// Queue full. Raft re-sends on next tick.
		messagesDroppedTotal.WithLabelValues(label).Inc()
	}
}

// runPeerSender is one worker goroutine per peer. It owns that peer's
// send queue and calls DoSend sequentially. On failure it performs the
// snapshot-on-reject fallback and reports the peer unreachable — all
// work that used to happen inline in the raft Ready loop on the
// caller's goroutine and blocked other peers' sends.
func (t *grpcTransport) runPeerSender(ctx context.Context, id uint64, peer *peer) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-peer.sendCh:
			if !ok {
				return
			}
			t.sendOneMessage(ctx, id, msg)
		}
	}
}

// sendOneMessage is the body of runPeerSender's loop. Split out so the
// snapshot-on-reject path is easier to read without deeply-nested
// control flow inside the select.
func (t *grpcTransport) sendOneMessage(ctx context.Context, id uint64, msg raftpb.Message) {
	label := peerLabel(t, id)

	applied, err := t.DoSend(ctx, msg)
	if err == nil && applied {
		peerReachable.WithLabelValues(label).Set(1)
		return
	}

	if err != nil && t.logger != nil {
		t.logger.Infof("unreachable %d: %v", id, err)
	}

	// If a MsgApp / MsgHeartbeat was rejected (Applied=false, no
	// network error), the follower's log might be behind the leader's
	// progress tracker. Send a snapshot to reset its state — the
	// snapshot carries our ConfState and committed index, letting the
	// follower catch up in one step.
	if (msg.Type == raftpb.MsgApp || msg.Type == raftpb.MsgHeartbeat) && !applied && err == nil {
		storage := t.getStorage()
		if storage != nil {
			snap, snapErr := storage.Snapshot()
			if snapErr != nil && t.logger != nil {
				t.logger.Errorf("failed to get snapshot for peer %d: %v", id, snapErr)
			} else if !raft.IsEmptySnap(snap) {
				snapshotsSentTotal.WithLabelValues(label).Inc()
				if _, sendErr := t.DoSend(ctx, raftpb.Message{
					Type:     raftpb.MsgSnap,
					To:       id,
					From:     t.localID,
					Snapshot: &snap,
				}); sendErr != nil {
					snapshotSendErrorsTotal.WithLabelValues(label).Inc()
					if t.logger != nil {
						t.logger.Infof("failed to send snapshot to %d: %v", id, sendErr)
					}
				} else {
					peerReachable.WithLabelValues(label).Set(1)
					if t.testHooks != nil && t.testHooks.OnSnapshotSent != nil {
						t.testHooks.OnSnapshotSent(id)
					}
					// Snapshot accepted — follower will send a
					// MsgAppResp that fixes the progress tracker.
					return
				}
			}
		}
	}

	peerReachable.WithLabelValues(label).Set(0)
	unreachableReportsTotal.WithLabelValues(label).Inc()
	if node := t.getNode(); node != nil {
		node.ReportUnreachable(id)
	}
}

func (t *grpcTransport) Send(ctx context.Context, req *transportv1.SendRequest) (*transportv1.SendResponse, error) {
	if t.testHooks != nil && t.testHooks.BlockIngress != nil && t.testHooks.BlockIngress.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if node := t.getNode(); node != nil {
		var msg raftpb.Message
		if err := msg.Unmarshal(req.Payload); err != nil {
			return &transportv1.SendResponse{Applied: false}, nil
		}

		// Clamp MsgHeartbeat.Commit to our lastIndex. A fresh follower has
		// lastIndex = N (N ConfChange entries from StartNode) while the
		// leader's Commit may be much higher. Without clamping,
		// handleHeartbeat calls commitTo(Commit) which panics when
		// Commit > lastIndex. Clamping lets the heartbeat succeed so the
		// raft node generates a proper MsgHeartbeatResp, keeping this
		// peer active in the leader's progress tracker while normal
		// MsgApp catch-up proceeds.
		// When a follower's log is behind the leader's commit (heartbeat) or
		// prev entry (MsgApp), return Applied=false so the leader sends a
		// snapshot instead. Without this, the follower either panics (heartbeat
		// commitTo beyond lastIndex) or enters an infinite rejection loop
		// (MsgApp rejection ignored due to stale match in the leader's progress
		// tracker).
		if msg.Type == raftpb.MsgHeartbeat || msg.Type == raftpb.MsgApp {
			if s := t.getStorage(); s != nil {
				lastIdx, err := s.LastIndex()
				if err == nil {
					reject := false
					if msg.Type == raftpb.MsgHeartbeat && msg.Commit > lastIdx {
						reject = true
					} else if msg.Type == raftpb.MsgApp && msg.Index > lastIdx {
						reject = true
					}
					if reject {
						return &transportv1.SendResponse{Applied: false}, nil
					}
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

		// Record the inbound message after the unmarshal-and-clamp gate
		// so we don't count messages that arrive corrupted (those return
		// before reaching here). msg.From is 0 for messages that carry
		// no peer identity (rare, e.g. internal proposals); fall back to
		// "unknown" so the metric label is stable.
		var fromLabel string
		if msg.From != 0 {
			fromLabel = peerLabel(t, msg.From)
		} else {
			fromLabel = "unknown"
		}
		messagesReceivedTotal.WithLabelValues(msgTypeLabel(msg.Type), fromLabel).Inc()

		err := node.Step(ctx, msg)
		if err == nil {
			return &transportv1.SendResponse{Applied: true}, nil
		}
		t.logger.Errorf("error occurred: %v", err)
	}
	return &transportv1.SendResponse{Applied: false}, nil
}

func (t *grpcTransport) Check(ctx context.Context, req *transportv1.CheckRequest) (*transportv1.CheckResponse, error) {
	if t.testHooks != nil && t.testHooks.BlockIngress != nil && t.testHooks.BlockIngress.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
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

func (t *grpcTransport) Status(ctx context.Context, req *transportv1.StatusRequest) (*transportv1.StatusResponse, error) {
	leader := t.leader.Load()
	leaderName := t.idsToNames[leader]
	raftState, _ := t.raftState.Load().(string)
	term := t.term.Load()

	clusterNames := make([]string, 0, len(t.idsToNames))
	for _, name := range t.idsToNames {
		clusterNames = append(clusterNames, name)
	}

	// Determine health and unhealthy peers via the Check RPC.
	var unhealthyPeers []string
	isHealthy := leader != 0
	checkResp, err := t.Check(ctx, &transportv1.CheckRequest{})
	if err == nil && checkResp != nil {
		isHealthy = checkResp.HasLeader
		for _, id := range checkResp.UnhealthyNodes {
			if name, ok := t.idsToNames[id]; ok {
				unhealthyPeers = append(unhealthyPeers, name)
			}
		}
	}

	name := t.idsToNames[t.localID]

	return &transportv1.StatusResponse{
		Name:           name,
		RaftState:      raftState,
		Leader:         leaderName,
		Term:           term,
		ClusterNames:   clusterNames,
		UnhealthyPeers: unhealthyPeers,
		IsHealthy:      isHealthy,
	}, nil
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

	// One worker per peer. They drain per-peer send queues independently,
	// so one slow/blackholed peer cannot delay sends to the others.
	for id, p := range t.peers {
		go t.runPeerSender(ctx, id, p)
	}

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
