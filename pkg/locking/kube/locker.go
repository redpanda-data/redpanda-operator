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
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
)

const (
	defaultRenewDeadline = 15 * time.Second
	defaultLeaseDuration = 30 * time.Second
	defaultRetryPeriod   = 5 * time.Second
)

type ClusterNamespaceConfig struct {
	Namespace string
	Config    *rest.Config
}

type LockConfiguration struct {
	ID   string
	Name string

	Configs []ClusterNamespaceConfig

	LeaderLabels     map[string]string
	RenewDeadline    time.Duration
	LeaseDuration    time.Duration
	RetryPeriod      time.Duration
	RecorderProvider recorder.Provider
}

func (lc *LockConfiguration) validate() error {
	if lc.Name == "" {
		return fmt.Errorf("lock name must be specified")
	}

	if len(lc.Configs) == 0 {
		return fmt.Errorf("configurations must be specified")
	}

	for _, config := range lc.Configs {
		if config.Namespace == "" {
			return fmt.Errorf("all lock configs must specify namespace")
		}
		if config.Config == nil {
			return fmt.Errorf("all lock configs must specify kube config")
		}
	}

	if lc.RenewDeadline == 0 {
		lc.RenewDeadline = defaultRenewDeadline
	}

	if lc.LeaseDuration == 0 {
		lc.LeaseDuration = defaultLeaseDuration
	}

	if lc.RetryPeriod == 0 {
		lc.RetryPeriod = defaultRetryPeriod
	}

	return nil
}

type LeaderCallbacks struct {
	OnStartedLeading func(ctx context.Context)
	OnStoppedLeading func()
}

func Run(ctx context.Context, config LockConfiguration, callbacks *LeaderCallbacks) error {
	if err := config.validate(); err != nil {
		return err
	}

	id := config.ID
	if id == "" {
		id, err := os.Hostname()
		if err != nil {
			return err
		}
		id = id + "_" + string(uuid.NewUUID())
	}

	var err error
	var lock resourcelock.Interface
	if len(config.Configs) == 1 {
		lock, err = newSingleResourceLock(id, config, config.Configs[0])
		if err != nil {
			return fmt.Errorf("could not create resource lock: %w", err)
		}
	} else {
		lock, err = newMultiResourceLock(id, config)
		if err != nil {
			return fmt.Errorf("could not create resource lock: %w", err)
		}
	}

	elector, err := leaderElector(config.Name, lock, callbacks, config)
	if err != nil {
		return fmt.Errorf("could not create leader elector: %w", err)
	}

	elector.Run(ctx)
	return nil
}

func leaderElector(name string, lock resourcelock.Interface, callbacks *LeaderCallbacks, config LockConfiguration) (*leaderelection.LeaderElector, error) {
	var initialized atomic.Bool
	var isLeader atomic.Bool

	leaderElector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: config.LeaseDuration,
		RenewDeadline: config.RenewDeadline,
		RetryPeriod:   config.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				if callbacks != nil {
					initialized.Store(true)
					if !isLeader.Swap(true) && callbacks.OnStartedLeading != nil {
						callbacks.OnStartedLeading(ctx)
					}
				}
			},
			OnStoppedLeading: func() {
				if callbacks != nil {
					if (!initialized.Swap(true) || isLeader.Swap(false)) && callbacks.OnStoppedLeading != nil {
						callbacks.OnStoppedLeading()
					}
				}
			},
			OnNewLeader: func(_ string) {
				if callbacks != nil {
					if (!initialized.Swap(true) || isLeader.Swap(false)) && callbacks.OnStoppedLeading != nil {
						callbacks.OnStoppedLeading()
					}
				}
			},
		},
		ReleaseOnCancel: true,
		Name:            name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize leader elector: %w", err)
	}

	return leaderElector, nil
}
