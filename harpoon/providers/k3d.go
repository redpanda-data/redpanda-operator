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

	"github.com/redpanda-data/common-go/kube"

	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
)

func NewK3D(nodes int) *K3DProvider {
	return &K3DProvider{
		nodes: nodes,
	}
}

type K3DProvider struct {
	nodes                    int
	cluster                  *k3d.Cluster
	retainCluster            bool
	fastNodeFailureDetection bool
	configPath               string
	importedImages           []string
}

func (p *K3DProvider) RetainCluster() *K3DProvider {
	p.retainCluster = true
	return p
}

// WithFastNodeFailureDetection shortens the node-failure grace/tolerations to
// ~10s so a suite that kills a node sees the eviction quickly. Only enable it
// for such suites: the 10s grace flaps momentarily-loaded nodes, which is fatal
// to workloads like vclusters. The cluster gets its own name so a retained
// cluster's baked-in flags are never reused by a suite that wants the defaults.
func (p *K3DProvider) WithFastNodeFailureDetection() *K3DProvider {
	p.fastNodeFailureDetection = true
	return p
}

func (p *K3DProvider) Initialize() error {
	return nil
}

func (p *K3DProvider) Setup(_ context.Context) error {
	name := "harpoon"
	opts := []k3d.ClusterOpt{k3d.WithServerNoSchedule(), k3d.SkipManifestInstallation(), k3d.WithAgents(p.nodes)}
	if p.fastNodeFailureDetection {
		name = "harpoon-nodefailure"
		opts = append(opts, k3d.WithFastNodeFailureDetection())
	}
	cluster, err := k3d.GetOrCreate(name, opts...)
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
	if len(images) == 0 {
		return nil
	}
	p.importedImages = append(p.importedImages, images...)
	return p.cluster.ImportImage(images...)
}

func (p *K3DProvider) DeleteNode(_ context.Context, name string) error {
	return p.cluster.DeleteNode(name)
}

func (p *K3DProvider) AddNode(_ context.Context, name string) error {
	if err := p.cluster.CreateNodeWithName(name); err != nil {
		return err
	}
	// A node created after setup has none of the images LoadImages imported, so
	// a pod scheduled onto it (e.g. the node-failure scenario's replacement
	// broker) would hit ImagePullBackOff pulling localhost/... images that never
	// came from a registry. Re-import the suite's images onto the new node.
	if len(p.importedImages) == 0 {
		return nil
	}
	return p.cluster.ImportImage(p.importedImages...)
}

func (p *K3DProvider) GetBaseContext() context.Context {
	return context.Background()
}

func (p *K3DProvider) ConfigPath() string {
	return p.configPath
}
