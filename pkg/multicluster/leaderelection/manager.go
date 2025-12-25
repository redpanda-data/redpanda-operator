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
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

type runFactory func(ctx context.Context) error

type LeaderManager struct {
	leaderRoutines []func(ctx context.Context) error

	logger logr.Logger

	localClient transportv1.TransportServiceClient

	mutex sync.RWMutex

	isLeader atomic.Bool
	runner   runFactory
}

func (lm *LeaderManager) Health(req *http.Request) error {
	if lm.localClient == nil {
		return errors.New("raft not started")
	}
	response, err := lm.localClient.Check(req.Context(), &transportv1.CheckRequest{})
	if err != nil {
		lm.logger.Error(err, "checking local client health")
		return err
	}
	if !response.HasLeader {
		return errors.New("cluster has no leader")
	}
	return nil
}

func NewRaftLockManager(configuration LockConfiguration, setLeader func(uint64)) *LeaderManager {
	manager := &LeaderManager{}
	manager.runner = func(ctx context.Context) error {
		return run(ctx, configuration, func(cl transportv1.TransportServiceClient) {
			manager.localClient = cl
		}, &LeaderCallbacks{
			OnStartedLeading: manager.runLeaderRoutines,
			OnStoppedLeading: func() {
				manager.isLeader.Store(false)
			},
			SetLeader: setLeader,
		})
	}

	return manager
}

func (lm *LeaderManager) runLeaderRoutines(ctx context.Context) {
	lm.isLeader.Store(true)

	lm.mutex.RLock()
	defer lm.mutex.RUnlock()

	for _, fn := range lm.leaderRoutines {
		go func() {
			for {
				err := fn(ctx)
				select {
				case <-ctx.Done():
					return
				default:
					if err != nil {
						lm.logger.Error(err, "error encountered on leader routine, restarting in 10 seconds")
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(10 * time.Second):
					}
				}
			}
		}()
	}
}

func (lm *LeaderManager) RegisterRoutine(fn func(ctx context.Context) error) {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	lm.leaderRoutines = append(lm.leaderRoutines, fn)
}

func (lm *LeaderManager) IsLeader() bool {
	return lm.isLeader.Load()
}

func (lm *LeaderManager) Run(ctx context.Context) error {
	return lm.runner(ctx)
}
