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

	"github.com/juju/mutex/v2"
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
	// K3d appears to have a race condition which stalls cluster creation when
	// clusters are created at (or close to) the same time. To prevent tests
	// from stalling out, we wrap this function in a machine wide mutext
	// (powered by a lockfile under the hood).
	releaser, err := mutex.Acquire(mutex.Spec{
		Name:    "k3d-go-test-mutex",
		Clock:   systemClock{},
		Timeout: 10 * time.Minute,
		Delay:   5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire machine wide mutext: %w", err)
	}

	defer releaser.Release()

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
		`--kubeconfig-switch-context=false`,
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

type systemClock struct{}

var _ mutex.Clock = systemClock{}

func (systemClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (systemClock) Now() time.Time {
	return time.Now()
}
