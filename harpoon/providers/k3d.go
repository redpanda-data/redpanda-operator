// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package providers

import (
	"context"
	"errors"
	"os"

	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

func NewK3D(nodes int) *K3DProvider {
	return &K3DProvider{
		nodes: nodes,
	}
}

type K3DProvider struct {
	nodes         int
	cluster       *k3d.Cluster
	retainCluster bool
	configPath    string
}

func (p *K3DProvider) RetainCluster() *K3DProvider {
	p.retainCluster = true
	return p
}

func (p *K3DProvider) Initialize() error {
	return nil
}

func (p *K3DProvider) Setup(_ context.Context) error {
	cluster, err := k3d.GetOrCreate("harpoon", k3d.WithServerNoSchedule(), k3d.SkipManifestInstallation(), k3d.WithAgents(p.nodes))
	if err != nil {
		return err
	}

	configPath, err := os.CreateTemp("", "k3d-harpoon-")
	if err != nil {
		return errors.Join(err, cluster.Cleanup())
	}

	if err := kube.WriteToFile(kube.RestToConfig(cluster.RESTConfig()), configPath.Name()); err != nil {
		return errors.Join(err, cluster.Cleanup(), os.RemoveAll(configPath.Name()))
	}

	p.cluster = cluster
	p.configPath = configPath.Name()
	return nil
}

func (p *K3DProvider) Teardown(_ context.Context) error {
	if !p.retainCluster && p.cluster != nil {
		return errors.Join(p.cluster.Cleanup(), os.RemoveAll(p.configPath))
	}
	return nil
}

func (p *K3DProvider) LoadImages(_ context.Context, images []string) error {
	for _, image := range images {
		if err := p.cluster.ImportImage(image); err != nil {
			return err
		}
	}
	return nil
}

func (p *K3DProvider) DeleteNode(_ context.Context, name string) error {
	return p.cluster.DeleteNode(name)
}

func (p *K3DProvider) AddNode(_ context.Context, name string) error {
	return p.cluster.CreateNodeWithName(name)
}

func (p *K3DProvider) GetBaseContext() context.Context {
	return context.Background()
}

func (p *K3DProvider) ConfigPath() string {
	return p.configPath
}
