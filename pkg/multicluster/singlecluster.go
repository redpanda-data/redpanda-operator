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
	"errors"
	"net/http"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

type singleClusterManager struct {
	mcmanager.Manager
}

func (s *singleClusterManager) GetLeader() string {
	return mcmanager.LocalCluster
}

func (s *singleClusterManager) GetClusterNames() []string {
	return []string{mcmanager.LocalCluster}
}

func (s *singleClusterManager) AddOrReplaceCluster(_ context.Context, _ string, _ cluster.Cluster) error {
	return errors.New("adding a cluster not supported in single cluster mode")
}

func (s *singleClusterManager) Health(req *http.Request) error {
	return nil
}

func NewSingleClusterManager(config *rest.Config, opts manager.Options) (Manager, error) {
	mgr, err := ctrl.NewManager(config, opts)
	if err != nil {
		return nil, err
	}

	mcmgr, err := mcmanager.WithMultiCluster(mgr, nil)
	if err != nil {
		return nil, err
	}

	return &singleClusterManager{mcmgr}, nil
}
