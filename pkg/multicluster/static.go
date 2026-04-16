// Copyright 2026 Redpanda Data, Inc.
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
	"errors"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"
)

// staticMulticlusterManager is a Manager backed by pre-configured REST configs.
// It is primarily intended for testing and tooling that needs to build clients
// across multiple clusters without running a full raft-based operator.
type staticMulticlusterManager struct {
	mcmanager.Manager

	local string
	names []string
}

var _ Manager = (*staticMulticlusterManager)(nil)

// NewStaticMulticlusterManager creates a Manager backed by the given REST
// configs. The local parameter names the cluster used as the local cluster.
// Non-local clusters are registered with a clusters.Provider. Call Start on
// the returned manager before using it.
func NewStaticMulticlusterManager(local string, configs map[string]*rest.Config, scheme *runtime.Scheme) (Manager, error) {
	localCfg, ok := configs[local]
	if !ok {
		return nil, fmt.Errorf("local cluster %q not found in configs", local)
	}

	provider := clusters.New()
	names := []string{local}

	for name, cfg := range configs {
		if name == local {
			continue
		}
		c, err := cluster.New(cfg, func(o *cluster.Options) {
			if scheme != nil {
				o.Scheme = scheme
			}
		})
		if err != nil {
			return nil, fmt.Errorf("creating cluster %q: %w", name, err)
		}
		if err := provider.Add(context.Background(), name, c); err != nil {
			return nil, fmt.Errorf("adding cluster %q to provider: %w", name, err)
		}
		names = append(names, name)
	}

	opts := crmanager.Options{LeaderElection: false}
	if scheme != nil {
		opts.Scheme = scheme
	}

	mgr, err := mcmanager.New(localCfg, provider, opts)
	if err != nil {
		return nil, fmt.Errorf("creating multicluster manager: %w", err)
	}

	return &staticMulticlusterManager{
		Manager: mgr,
		local:   local,
		names:   names,
	}, nil
}

func (s *staticMulticlusterManager) GetCluster(ctx context.Context, clusterName string) (cluster.Cluster, error) {
	if clusterName == s.local {
		clusterName = mcmanager.LocalCluster
	}
	return s.Manager.GetCluster(ctx, clusterName)
}

func (s *staticMulticlusterManager) GetClusterNames() []string {
	return s.names
}

func (s *staticMulticlusterManager) GetLocalClusterName() string {
	return s.local
}

func (s *staticMulticlusterManager) GetLeader() string {
	return s.local
}

func (s *staticMulticlusterManager) AddOrReplaceCluster(_ context.Context, _ string, _ cluster.Cluster) error {
	return errors.New("adding or replacing a cluster is not supported in static manager")
}

func (s *staticMulticlusterManager) Health(_ *http.Request) error {
	return nil
}

func (s *staticMulticlusterManager) IsClusterReachable(_ string) bool {
	return true
}
