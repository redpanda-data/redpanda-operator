// Copyright 2025 Redpanda Data, Inc.
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
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

const (
	DefaultK3sImage = `rancher/k3s:v1.29.6-k3s2`
	K3sImageEnv     = `K3S_IMAGE`
)

var (
	//go:embed *.yaml
	manifestsFS embed.FS

	ErrExists = errors.New("cluster with name already exists")
)

type Cluster struct {
	Name string

	mu           sync.Mutex
	restConfig   *kube.RESTConfig
	agentCounter int32
}

type ClusterOpt interface {
	apply(config *clusterConfig)
}

type clusterOpt func(config *clusterConfig)

func (c clusterOpt) apply(config *clusterConfig) {
	c(config)
}

type clusterConfig struct {
	agents  int
	timeout time.Duration
	image   string
}

func defaultClusterConfig() *clusterConfig {
	image := DefaultK3sImage
	if override, ok := os.LookupEnv(K3sImageEnv); ok {
		image = override
	}

	return &clusterConfig{
		agents:  3,
		timeout: 3 * time.Minute,
		image:   image,
	}
}

func WithAgents(agents int) clusterOpt {
	return func(config *clusterConfig) {
		config.agents = agents
	}
}

func WithImage(image string) clusterOpt {
	return func(config *clusterConfig) {
		config.image = image
	}
}

func WithTimeout(timeout time.Duration) clusterOpt {
	return func(config *clusterConfig) {
		config.timeout = timeout
	}
}

func GetOrCreate(name string, opts ...ClusterOpt) (*Cluster, error) {
	config := defaultClusterConfig()
	for _, opt := range opts {
		opt.apply(config)
	}

	cluster, err := NewCluster(name, opts...)
	if err != nil {
		if errors.Is(err, ErrExists) {
			return loadCluster(name, config)
		}
		return nil, err
	}
	return cluster, nil
}

func NewCluster(name string, opts ...ClusterOpt) (*Cluster, error) {
	name = strings.ToLower(name)

	config := defaultClusterConfig()
	for _, opt := range opts {
		opt.apply(config)
	}

	args := []string{
		"cluster",
		"create",
		name,
		fmt.Sprintf("--agents=%d", config.agents),
		fmt.Sprintf("--timeout=%s", config.timeout),
		fmt.Sprintf("--image=%s", config.image),
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
		`--verbose`,
	}

	out, err := exec.Command("k3d", args...).CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte(`because a cluster with that name already exists`)) {
			return nil, errors.Wrapf(ErrExists, "%q", name)
		}

		// If k3d cluster create will fail please uncomment the following debug logs from containers
		for i := 0; i < config.agents; i++ {
			containerLogs, _ := exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-agent-%d", name, i)).CombinedOutput()
			fmt.Printf("Agent-%d logs:\n%s\n", i, string(containerLogs))
		}
		containerLogs, _ := exec.Command("docker", "logs", fmt.Sprintf("k3d-%s-server-0", name)).CombinedOutput()
		fmt.Printf("server-0 logs:\n%s\n", string(containerLogs))
		containerLogs, _ = exec.Command("docker", "network", "inspect", fmt.Sprintf("k3d-%s", name)).CombinedOutput()
		fmt.Printf("docker network inspect:\n%s\n", string(containerLogs))

		return nil, errors.Wrapf(err, "%s", out)
	}

	return loadCluster(name, config)
}

func loadCluster(name string, config *clusterConfig) (*Cluster, error) {
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

	cluster := &Cluster{
		Name:         name,
		restConfig:   cfg,
		agentCounter: int32(config.agents),
	}

	if err := cluster.waitForJobs(context.Background()); err != nil {
		return nil, err
	}

	return cluster, nil
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

func forceCleanup(name string) {
	// attempt to cleanup no matter what
	// we just ignore any output in case
	// the cluster doesn't exist, etc.
	_, _ = exec.Command(
		"k3d",
		"cluster",
		"delete",
		name,
	).CombinedOutput()
}

// setupCluster applies any embedded manifests and  blocks until all jobs in
// the kube-system namespace have completed. This is a course grain way to wait
// for k3s to finish installing it's bundled dependencies via helm.
// See: https://docs.k3s.io/helm, https://k3d.io/v5.7.4/usage/k3s/?h=
func (c *Cluster) waitForJobs(ctx context.Context) error {
	cl, err := client.New(c.restConfig, client.Options{})
	if err != nil {
		return err
	}

	// NB: Originally this functionality was achieved via the --volume flag to
	// k3d but CI runs via a docker in docker setup which makes it unreasonable
	// to use --volume.
	// Alternatively, we could make our own k3s images and directly copy in the
	// manifests in the Dockerfile.
	for _, obj := range startupManifests() {
		// we deep copy so we don't modify the startup manifests when multiple k3d objects are created
		cloned := obj.DeepCopyObject().(client.Object)
		if err := cl.Patch(ctx, cloned, client.Apply, client.FieldOwner(fmt.Sprintf("k3d-setup-%s", c.Name)), client.ForceOwnership); err != nil {
			return errors.WithStack(err)
		}
	}

	// Wait for all bootstrapping jobs to finish running.
	return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		var jobs batchv1.JobList
		if err := cl.List(ctx, &jobs, client.InNamespace("kube-system")); err != nil {
			return false, err
		}

		for _, job := range jobs.Items {
			idx := slices.IndexFunc(job.Status.Conditions, func(cond batchv1.JobCondition) bool {
				return cond.Type == batchv1.JobComplete
			})

			if idx == -1 || job.Status.Conditions[idx].Status != corev1.ConditionTrue {
				return false, nil
			}
		}

		return true, nil
	})
}

// startupManifests parses the embedded FS of Kubernetes manifests as a slice
// of [client.Object]s.
var startupManifests = sync.OnceValue(func() []client.Object {
	var objs []client.Object
	if err := fs.WalkDir(manifestsFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		contents, err := fs.ReadFile(manifestsFS, p)
		if err != nil {
			return err
		}

		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(contents, &obj); err != nil {
			return errors.Wrapf(err, "unmarshalling %q", p)
		}

		objs = append(objs, &obj)

		return nil
	}); err != nil {
		panic(err)
	}

	return objs
})
