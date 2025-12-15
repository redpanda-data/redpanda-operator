// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"sort"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/pkg/locking"
)

type Manager interface {
	mcmanager.Manager
	GetLeader() string
	GetClusterNames() []string
	// the context passed here, when canceled will stop the cluster
	AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error
}

type managerI struct {
	mcmanager.Manager
	runnable            *leaderRunnable
	logger              logr.Logger
	getLeader           func() string
	getClusters         func() map[string]cluster.Cluster
	addOrReplaceCluster func(ctx context.Context, clusterName string, cl cluster.Cluster) error
}

func (m *managerI) AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error {
	return m.addOrReplaceCluster(ctx, clusterName, cl)
}

func (m *managerI) GetClusterNames() []string {
	clusters := []string{mcmanager.LocalCluster}
	if m.getClusters == nil {
		return clusters
	}

	for cluster := range m.getClusters() {
		clusters = append(clusters, cluster)
	}
	sort.Strings(clusters)
	return clusters
}

func (m *managerI) GetLeader() string {
	if m.getLeader == nil {
		return ""
	}
	return m.getLeader()
}

func newManager(logger logr.Logger, config *rest.Config, provider multicluster.Provider, restart chan struct{}, getLeader func() string, getClusters func() map[string]cluster.Cluster, addOrReplaceCluster func(ctx context.Context, clusterName string, cl cluster.Cluster) error, manager *locking.LeaderManager, opts manager.Options) (Manager, error) {
	mgr, err := mcmanager.New(config, provider, opts)
	if err != nil {
		return nil, err
	}

	manager.RegisterRoutine(func(ctx context.Context) error {
		logger.Info("got leader")
		<-ctx.Done()
		logger.Info("lost leader")
		return nil
	})

	runnable := &leaderRunnable{manager: manager, logger: logger, restart: restart, getClusters: getClusters}
	if err := mgr.Add(runnable); err != nil {
		return nil, err
	}
	return &managerI{Manager: mgr, runnable: runnable, logger: logger, getLeader: getLeader, getClusters: getClusters, addOrReplaceCluster: addOrReplaceCluster}, nil
}

func (m *managerI) Add(r mcmanager.Runnable) error {
	if _, ok := r.(reconcile.TypedReconciler[mcreconcile.Request]); ok {
		m.logger.Info("adding multicluster reconciler")
		m.runnable.Add(r)
		return nil
	}

	if _, ok := r.(manager.LeaderElectionRunnable); ok {
		m.logger.Info("adding leader election runnable")
		m.runnable.Add(r)
		return nil
	}

	return m.Manager.Add(r)
}

type warmupRunnable interface {
	Warmup(context.Context) error
}

type leaderRunnable struct {
	runnables   []mcmanager.Runnable
	manager     *locking.LeaderManager
	logger      logr.Logger
	restart     chan struct{}
	getClusters func() map[string]cluster.Cluster
}

func (l *leaderRunnable) Add(r mcmanager.Runnable) {
	doEngage := func() {
		for name, cluster := range l.getClusters() {
			// engage any static clusters
			_ = r.Engage(context.Background(), name, cluster)
		}
	}

	l.runnables = append(l.runnables, r)
	if warmup, ok := r.(warmupRunnable); ok {
		// start caches and sources
		l.manager.RegisterRoutine(l.wrapStart(doEngage, warmup.Warmup))
	}
	l.manager.RegisterRoutine(l.wrapStart(doEngage, r.Start))
}

func (l *leaderRunnable) Engage(ctx context.Context, s string, c cluster.Cluster) error {
	for _, runnable := range l.runnables {
		if err := runnable.Engage(ctx, s, c); err != nil {
			l.logger.Info("engaging runnable")
			return err
		}
	}
	return nil
}

func (l *leaderRunnable) Start(ctx context.Context) error {
	return l.manager.Run(ctx)
}

func (l *leaderRunnable) wrapStart(doEngage func(), fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		doEngage()

		go func() {
			for {
				select {
				case <-cancelCtx.Done():
					return
				case <-l.restart:
					// re-engage
					doEngage()
				}
			}
		}()

		return fn(cancelCtx)
	}
}
