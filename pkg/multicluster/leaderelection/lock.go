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
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

const (
	defaultHeartbeatInterval = 1 * time.Second
	defaultElectionTimeout   = 10 * time.Second
	defaultGRPCMaxBackoff    = 5 * time.Second
)

var discardLogger = &raft.DefaultLogger{Logger: log.New(io.Discard, "", 0)}

// LeaderCallbacks contains the functions invoked when raft leadership
// transitions occur.
type LeaderCallbacks struct {
	// SetLeader is called whenever the known raft leader changes.
	SetLeader func(leader uint64)
	// OnStartedLeading is called when this node becomes the raft leader.
	// The provided context is cancelled when leadership is lost.
	OnStartedLeading func(ctx context.Context)
	// OnStoppedLeading is called when this node loses raft leadership.
	OnStoppedLeading func()
}

// LockerNode identifies a single node in the raft cluster.
type LockerNode struct {
	// ID is the unique raft node identifier (typically a hash of the cluster name).
	ID uint64
	// Address is the host:port where this node's gRPC transport listens.
	Address string
}

// LockConfiguration holds the configuration for a raft-based distributed
// lock, including node identity, peer topology, TLS, and timing parameters.
type LockConfiguration struct {
	// ID is the unique raft node identifier for this node.
	ID uint64
	// Address is the host:port this node's gRPC transport listens on.
	Address string
	// Meta is opaque metadata attached to this node.
	Meta []byte
	// Peers lists all nodes in the raft cluster.
	Peers []LockerNode
	// Fetcher provides kubeconfig retrieval for bootstrap mode.
	Fetcher KubeconfigFetcher

	// Insecure disables TLS on the gRPC transport.
	Insecure bool
	// ServerTLSOptions are custom TLS config mutators for the inbound gRPC listener.
	ServerTLSOptions []func(*tls.Config)
	// ClientTLSOptions are custom TLS config mutators for outbound gRPC connections.
	ClientTLSOptions []func(*tls.Config)
	// CA is the PEM-encoded CA certificate.
	CA []byte
	// PrivateKey is the PEM-encoded TLS private key.
	PrivateKey []byte
	// Certificate is the PEM-encoded TLS certificate.
	Certificate []byte

	// ElectionTimeout is the raft election timeout. Zero uses the default (10s).
	ElectionTimeout time.Duration
	// HeartbeatInterval is the raft heartbeat interval. Zero uses the default (1s).
	HeartbeatInterval time.Duration
	// GRPCMaxBackoff caps the exponential backoff delay between gRPC
	// reconnection attempts. A shorter value speeds up recovery when a
	// peer restarts (e.g. after a failover). Zero uses the default (5s).
	GRPCMaxBackoff time.Duration
	// Logger is used for raft-internal logging.
	Logger raft.Logger
}

func (c *LockConfiguration) validate() error {
	if c.ID == 0 {
		return errors.New("id must be specified")
	}
	if c.Address == "" {
		return errors.New("address must be specified")
	}
	if !c.Insecure && (len(c.ServerTLSOptions) == 0 || len(c.ClientTLSOptions) == 0) {
		if len(c.CA) == 0 {
			return errors.New("ca must be specified")
		}
		if len(c.PrivateKey) == 0 {
			return errors.New("private key must be specified")
		}
		if len(c.Certificate) == 0 {
			return errors.New("certificate must be specified")
		}
	}
	if len(c.Peers) == 0 {
		return errors.New("peers must be set")
	}

	return nil
}

func (n *LockerNode) asPeer() raft.Peer {
	return raft.Peer{
		ID:      n.ID,
		Context: []byte(n.Address),
	}
}

func peersForNodes(nodes []LockerNode) map[uint64]string {
	peers := make(map[uint64]string)
	for _, node := range nodes {
		peers[node.ID] = node.Address
	}
	return peers
}

func asPeers(nodes []LockerNode) []raft.Peer {
	peers := []raft.Peer{}
	for _, node := range nodes {
		peers = append(peers, node.asPeer())
	}
	return peers
}

// Run starts the raft node and gRPC transport, blocking until ctx is cancelled
// or an unrecoverable error occurs. Leadership transitions are reported via callbacks.
//
// # Recovery Semantics
//
// This raft implementation uses in-memory storage (no WAL). When a node
// restarts it has an empty log and must catch up from the leader via snapshot.
// Three mechanisms cooperate to ensure recovery succeeds:
//
// 1. PreVote prevents term inflation. Without PreVote, a restarting node
// runs elections that fail (its log is behind), but each attempt increments
// its term. When it later contacts the leader, the inflated term forces
// the leader to step down. The new election restarts the cycle. PreVote
// requires a majority to agree the election could succeed before advancing
// the term, breaking this livelock.
//
// 2. Synthetic MsgHeartbeatResp reactivates inactive peers. The raft library
// marks peers as inactive when they miss heartbeats and stops sending them
// messages. A restarting peer can only send MsgPreVote (which doesn't touch
// the leader's progress tracker), so without intervention it stays inactive
// permanently. The Send handler intercepts any inbound message from a peer
// and, if this node is the leader, steps a synthetic MsgHeartbeatResp to
// mark the peer active again.
//
// 3. ReportUnreachable on rejected heartbeats transitions the progress
// tracker from StateReplicate to StateProbe. When a peer restarts fresh,
// the leader may still have it tracked in StateReplicate with Match equal
// to the old lastIndex. Heartbeats to the new peer are rejected by the
// commit guard (Commit > lastIndex), returning Applied=false. Without
// ReportUnreachable, the progress stays in StateReplicate, which prevents
// sendAppend from being called (IsPaused returns true when the inflight
// buffer is full). Calling ReportUnreachable transitions to StateProbe,
// where sendAppend always fires, allowing the leader to discover the peer
// is behind and send a snapshot.
//
// # Recovery Timeline
//
// When a node dies and its replacement starts (e.g. a standby acquiring the
// K8s lease in double leader-election mode), recovery proceeds through these
// phases:
//
//   - gRPC reconnection: The leader's gRPC client reconnects to the new peer.
//     The default GRPCMaxBackoff of 5s caps the exponential backoff so
//     reconnection completes within a few seconds.
//
//   - Peer reactivation: Once the connection is established, the new peer
//     sends a MsgPreVote to the leader. The Send handler steps a synthetic
//     MsgHeartbeatResp, reactivating the peer's progress tracker. This
//     happens on the first message exchange after reconnection.
//
//   - Progress tracker repair: The leader sends a heartbeat to the newly
//     active peer. The commit guard rejects it (Applied=false) because the
//     peer's log only has N ConfChange entries. ReportUnreachable transitions
//     the progress from StateReplicate to StateProbe.
//
//   - Snapshot delivery: In StateProbe, the leader calls sendAppend, which
//     discovers the peer is far behind and sends a MsgSnap. The peer applies
//     the snapshot, advancing its lastIndex to match the leader's.
//
//   - Steady state: Subsequent heartbeats pass the commit guard. The peer
//     responds normally and transitions to StateReplicate.
//
// Total recovery time is approximately:
//
//	GRPCMaxBackoff                           // reconnection (default 5s)
//	+ ElectionTimeout                        // peer runs a PreVote round
//	+ 2-3 × HeartbeatInterval               // reactivation + probe + snapshot
//
// With defaults (ElectionTimeout=10s, HeartbeatInterval=1s, GRPCMaxBackoff=5s):
// worst case is ~18s.
//
// # Timeout Constraints
//
// The raft tick period is fixed at 10ms. ElectionTimeout and HeartbeatInterval
// are converted to ticks by dividing by 10ms. The following constraints apply:
//
//   - ElectionTimeout must be >= 10 × HeartbeatInterval. The raft library
//     enforces ElectionTick >= 10 × HeartbeatTick internally. This gives
//     heartbeats enough time to propagate before an election is triggered.
//
//   - GRPCMaxBackoff should be <= HeartbeatInterval for fast recovery. If
//     it's much larger, a restarting peer may miss many heartbeat cycles
//     while the gRPC client backs off, delaying the reconnection that
//     triggers the recovery chain.
//
//   - In double leader-election mode (K8s lease + raft), the K8s lease
//     duration bounds how quickly a standby can replace a dead active
//     replica. The total failover time is LeaseDuration (for the standby
//     to acquire the lease) + the raft recovery time above.
func Run(ctx context.Context, config LockConfiguration, callbacks *LeaderCallbacks) error {
	return run(ctx, config, nil, callbacks)
}

func run(ctx context.Context, config LockConfiguration, transportCallback func(transportv1.TransportServiceClient), callbacks *LeaderCallbacks) error {
	if err := config.validate(); err != nil {
		return err
	}

	if config.GRPCMaxBackoff == 0 {
		config.GRPCMaxBackoff = defaultGRPCMaxBackoff
	}

	nodes := peersForNodes(config.Peers)
	var transport *grpcTransport
	var err error
	if config.Insecure {
		transport, err = newInsecureGRPCTransport(config.Meta, config.Address, nodes, config.Fetcher, config.GRPCMaxBackoff)
	} else if len(config.ServerTLSOptions) == 0 || len(config.ClientTLSOptions) == 0 {
		transport, err = newGRPCTransport(config.Meta, config.Certificate, config.PrivateKey, config.CA, config.Address, nodes, config.Fetcher, config.GRPCMaxBackoff)
	} else {
		transport, err = newGRPCTransportWithOptions(config.Meta, config.ServerTLSOptions, config.ClientTLSOptions, config.Address, nodes, config.Fetcher, config.GRPCMaxBackoff)
	}
	if err != nil {
		return err
	}

	if transportCallback != nil {
		cl, err := transport.client()
		if err != nil {
			return err
		}
		transportCallback(cl)
	}
	transport.logger = config.Logger

	for node, address := range nodes {
		if config.Logger != nil {
			config.Logger.Infof("node: %d, address: %s", node, address)
		}
	}

	errs := make(chan error, 2)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := transport.Run(ctx); err != nil {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := runRaft(ctx, transport, config, callbacks); err != nil {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		cancel()
		wg.Wait()
		return err
	case <-ctx.Done():
		config.Logger.Infof("context canceled, waiting for raft and transport to exit")
		wg.Wait()
	}

	return nil
}

func runRaft(ctx context.Context, transport *grpcTransport, config LockConfiguration, callbacks *LeaderCallbacks) error {
	defer config.Logger.Info("shutting down raft")

	storage := raft.NewMemoryStorage()

	// Expose storage to the transport so its Send handler can guard against
	// MsgHeartbeat messages whose Commit index exceeds our current lastIndex
	// (which would otherwise panic inside the raft library via commitTo).
	transport.setStorage(storage)

	if config.ElectionTimeout == 0 {
		config.ElectionTimeout = defaultElectionTimeout
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = defaultHeartbeatInterval
	}
	if config.Logger == nil {
		config.Logger = discardLogger
	}

	config.Logger.Infof("starting node")
	node := raft.StartNode(&raft.Config{
		ID:              config.ID,
		ElectionTick:    int(config.ElectionTimeout.Milliseconds() / 10),
		HeartbeatTick:   int(config.HeartbeatInterval.Milliseconds() / 10),
		Storage:         storage,
		MaxSizePerMsg:   1024 * 1024,
		MaxInflightMsgs: 256,
		CheckQuorum:     true,
		PreVote:         true,
		Logger:          config.Logger,
	}, asPeers(config.Peers))

	transport.setNode(node)

	// confState captures the static cluster membership and is embedded in every
	// snapshot. CreateSnapshot must be called before Compact so the storage
	// always has a non-empty snapshot to send to lagging peers; without it,
	// maybeSendSnapshot panics with "need non-empty snapshot".
	voters := make([]uint64, 0, len(config.Peers))
	for _, p := range config.Peers {
		voters = append(voters, p.ID)
	}
	confState := raftpb.ConfState{Voters: voters}

	go func() {
		compactions := 1000 // every 10 seconds
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				node.Tick()
				if compactions == 0 {
					applied := node.Status().Applied
					// Guard against a race: node.Status().Applied can reflect entries
					// that the raft node has accepted into its unstable log but that our
					// Ready loop has not yet written to MemoryStorage via storage.Append.
					// CreateSnapshot panics if the requested index exceeds lastindex, so
					// skip this compaction cycle and retry in the next one.
					lastStored, err := storage.LastIndex()
					if err == nil && applied > 0 && applied <= lastStored {
						if _, err := storage.CreateSnapshot(applied, &confState, nil); err == nil {
							_ = storage.Compact(applied)
						}
					}
					compactions = 1000
				}
				compactions--
			}
		}
	}()

	leaderCtx, leaderCancel := context.WithCancel(ctx)

	isLeader := false
	initialized := false
	for {
		select {
		case <-ctx.Done():
			leaderCancel()
			if isLeader {
				if callbacks.OnStoppedLeading != nil {
					go callbacks.OnStoppedLeading()
				}
			}
			config.Logger.Infof("context canceled, stopping node")
			node.Stop()
			return nil
		case rd := <-node.Ready():
			// Observe soft state changes for leadership
			var nowLeader bool
			var leader uint64
			if rd.SoftState != nil {
				leader = rd.SoftState.Lead
				nowLeader = leader == config.ID || rd.SoftState.RaftState == raft.StateLeader
			} else {
				status := node.Status()
				leader = status.Lead
				nowLeader = leader == config.ID || status.RaftState == raft.StateLeader
			}

			transport.leader.Store(leader)
			transport.isLeader.Store(nowLeader)

			if callbacks != nil && callbacks.SetLeader != nil {
				callbacks.SetLeader(leader)
			}

			if nowLeader != isLeader || !initialized {
				initialized = true
				if nowLeader {
					// just became leader, start things up
					isLeader = true
					if callbacks.OnStartedLeading != nil {
						go callbacks.OnStartedLeading(leaderCtx)
					}
				} else {
					// we became a follower
					leaderCancel()
					leaderCtx, leaderCancel = context.WithCancel(ctx)
					if callbacks.OnStoppedLeading != nil {
						go callbacks.OnStoppedLeading()
					}
				}
			}

			// send out messages
			_ = storage.Append(rd.Entries)
			for _, msg := range rd.Messages {
				if msg.To == config.ID {
					if err := node.Step(ctx, msg); err != nil {
						config.Logger.Errorf("error stepping node: %v", err)
					}
					continue
				}
				for {
					applied, err := transport.DoSend(ctx, msg)
					if err != nil {
						config.Logger.Infof("unreachable %d: %v", msg.To, err)
						node.ReportUnreachable(msg.To)
						break
					}
					if !applied {
						// Heartbeats are periodic: the leader sends the next one at
						// the next HeartbeatTick. A fresh follower may transiently
						// return Applied=false while its snapshot catch-up is in
						// progress; retrying here would block the Ready loop for
						// a full second per heartbeat per lagging peer.
						//
						// However, we must still report the peer as unreachable so
						// that the progress tracker transitions from StateReplicate
						// to StateProbe. Without this, a replaced follower (fresh
						// MemoryStorage) that rejects heartbeats via the commit
						// guard leaves the leader's progress in StateReplicate with
						// Match == lastIndex, preventing sendAppend from ever being
						// called — even after the synthetic MsgHeartbeatResp
						// reactivation.
						if msg.Type == raftpb.MsgHeartbeat {
							node.ReportUnreachable(msg.To)
							break
						}
						// attempt to apply again in a second
						time.Sleep(1 * time.Second)
					} else {
						break
					}
				}
			}

			node.Advance()
		}
	}
}
