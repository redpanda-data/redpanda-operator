// Copyright 2025 Redpanda Data, Inc.
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
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"go.etcd.io/raft/v3"
)

const (
	defaultHeartbeatInterval = 1 * time.Second
	defaultElectionTimeout   = 10 * time.Second
)

var discardLogger = &raft.DefaultLogger{Logger: log.New(io.Discard, "", 0)}

type LeaderCallbacks struct {
	SetLeader        func(leader uint64)
	OnStartedLeading func(ctx context.Context)
	OnStoppedLeading func()
}

type LockerNode struct {
	ID      uint64
	Address string
}

type LockConfiguration struct {
	ID          uint64
	Address     string
	CA          []byte
	PrivateKey  []byte
	Certificate []byte
	Meta        []byte
	Peers       []LockerNode
	Insecure    bool
	Fetcher     KubeconfigFetcher

	ElectionTimeout   time.Duration
	HeartbeatInterval time.Duration
	Logger            raft.Logger
}

func (c *LockConfiguration) validate() error {
	if c.ID == 0 {
		return errors.New("id must be specified")
	}
	if c.Address == "" {
		return errors.New("address must be specified")
	}
	if !c.Insecure {
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

func Run(ctx context.Context, config LockConfiguration, callbacks *LeaderCallbacks) error {
	if err := config.validate(); err != nil {
		return err
	}

	nodes := peersForNodes(config.Peers)
	var transport *grpcTransport
	var err error
	if config.Insecure {
		transport, err = newInsecureGRPCTransport(config.Meta, config.Address, nodes, config.Fetcher)
	} else {
		transport, err = newGRPCTransport(config.Meta, config.Certificate, config.PrivateKey, config.CA, config.Address, nodes, config.Fetcher)
	}
	if err != nil {
		return err
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
		Logger:          config.Logger,
	}, asPeers(config.Peers))

	transport.setNode(node)

	go func() {
		compactions := 1000 // every 10 seconds
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				node.Tick()
				if compactions == 0 {
					if err := storage.Compact(node.Status().Applied); err != nil {
						config.Logger.Errorf("error compacting storage: %v", err)
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
