// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package k3d provides a go interface to the `k3d` CLI.
// https://k3d.io/
//
//nolint:gosec // this code is for tests
package k3d

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/redpanda-data/helm-charts/pkg/kube"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultK3sImage = `rancher/k3s:v1.29.6-k3s2`
	K3sImageEnv     = `K3S_IMAGE`
)

type Cluster struct {
	Name string

	mu           sync.Mutex
	restConfig   *kube.RESTConfig
	agentCounter int32
}

func NewCluster(name string) (*Cluster, error) {
	name = strings.ToLower(name)

	image := DefaultK3sImage
	if override, ok := os.LookupEnv(K3sImageEnv); ok {
		image = override
	}

	out, err := exec.Command(
		"k3d",
		"cluster",
		"create",
		name,
		fmt.Sprintf("--agents=%d", 3),
		fmt.Sprintf("--timeout=%s", 30*time.Second),
		fmt.Sprintf("--image=%s", image),
		// If k3d cluster create will fail please uncomment no-rollback flag
		// "--no-rollback",

		// See also https://github.com/k3d-io/k3d/blob/main/docs/faq/faq.md#passing-additional-argumentsflags-to-k3s-and-on-to-eg-the-kube-apiserver
		// As the formatting is QUITE finicky.
		// Halve the node-monitor-grace-period to speed up tests that rely on dead node detection.
		`--k3s-arg`, `--kube-controller-manager-arg=node-monitor-grace-period=10s@server:*`,
		// Dramatically decrease (5m -> 10s) the default tolerations to ensure
		// Pod eviction happens in a timely fashion.
		`--k3s-arg`, `--kube-apiserver-arg=default-not-ready-toleration-seconds=10@server:*`,
		`--k3s-arg`, `--kube-apiserver-arg=default-unreachable-toleration-seconds=10@server:*`,
	).CombinedOutput()
	if err != nil {
		// If k3d cluster create will fail please uncomment the following debug logs from containers
		// containerLogs, _ := exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-agent-0", name)).CombinedOutput()
		// fmt.Printf("Agent-0 logs:\n%s\n", string(containerLogs))
		// containerLogs, _ = exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-agent-1", name)).CombinedOutput()
		// fmt.Printf("Agent-1 logs:\n%s\n", string(containerLogs))
		// containerLogs, _ = exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-agent-2", name)).CombinedOutput()
		// fmt.Printf("Agent-2 logs:\n%s\n", string(containerLogs))
		// containerLogs, _ = exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-server-0", name)).CombinedOutput()
		// fmt.Printf("serrver-0 logs:\n%s\n", string(containerLogs))
		// containerLogs, _ = exec.Command("docker", "network", "inspect", fmt.Sprintf("k3d-%s", name)).CombinedOutput()
		// fmt.Printf("docker network inspect:\n%s\n", string(containerLogs))
		return nil, fmt.Errorf("%w: %s", err, out)
	}

	kubeconfigYAML, err := exec.Command("k3d", "kubeconfig", "get", name).CombinedOutput()
	if err != nil {
		return nil, err
	}

	kubeconfig, err := clientcmd.Load(kubeconfigYAML)
	if err != nil {
		return nil, err
	}

	cfg, err := kube.ConfigToRest(*kubeconfig)
	if err != nil {
		return nil, err
	}

	return &Cluster{
		Name:         name,
		restConfig:   cfg,
		agentCounter: 3,
	}, nil
}

func (c *Cluster) RESTConfig() *kube.RESTConfig {
	return c.restConfig
}

func (c *Cluster) DeleteNode(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if out, err := exec.Command(
		"k3d",
		"node",
		"delete",
		name,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func (c *Cluster) CreateNode() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agentCounter += 1

	if out, err := exec.Command(
		"k3d",
		"node",
		"create",
		fmt.Sprintf("k3d-%s-agent-%d", c.Name, c.agentCounter),
		fmt.Sprintf("--cluster=%s", c.Name),
		"--wait",
		"--role=agent",
	).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func (c *Cluster) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := exec.Command(
		"k3d",
		"cluster",
		"delete",
		c.Name,
	).CombinedOutput()
	return err
}
