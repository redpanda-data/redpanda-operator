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
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/redpanda-data/helm-charts/pkg/kube"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultK3sImage = `rancher/k3s:v1.29.6-k3s2`
	K3sImageEnv     = `K3S_IMAGE`
)

//go:embed *.yaml
var manifestsFS embed.FS

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

	// Dependencies get automatically installed by k3(s|d)'s helm integration.
	// We could potentially store the tar.gz's of the charts within our
	// embedded FS to enabled airgapped clusters.
	// Alternatively, we could manage custom k3s images instead of using
	// --volume.
	certMangerVol, err := manifestVolumeMount("cert-manager.yaml")
	if err != nil {
		return nil, err
	}

	args := []string{
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
		`--volume`, certMangerVol,
	}

	out, err := exec.Command("k3d", args...).CombinedOutput()
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

	cluster := &Cluster{
		Name:         name,
		restConfig:   cfg,
		agentCounter: 3,
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

// waitForJobs blocks until all jobs in the kube-system namespace have completed.
// This is a course grain way to wait for k3s to finish installing it's bundled
// dependencies via helm.
func (c *Cluster) waitForJobs(ctx context.Context) error {
	cl, err := client.New(c.restConfig, client.Options{})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeoutCause(ctx, time.Minute, fmt.Errorf("waiting for jobs in kube-system to finish"))
	defer cancel()

	// Wait for all bootstrapping jobs to finish running.
	return wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, false, func(ctx context.Context) (done bool, err error) {
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

// manifestVolumeMount returns a string suitable for use as `--volume` to `k3d
// cluster start` which will mount a helm chart manifest into the server's manifest store.
// See: https://docs.k3s.io/helm, https://k3d.io/v5.7.4/usage/k3s/?h=
func manifestVolumeMount(name string) (string, error) {
	const manifestVolumeFmt = `%s:/var/lib/rancher/k3s/server/manifests/%s@server:*`

	base, err := writeManifests()
	if err != nil {
		return "", err
	}

	onDisk := path.Join(base, name)

	return fmt.Sprintf(manifestVolumeFmt, onDisk, name), nil
}

var writeManifests = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp(os.TempDir(), "k3d-manifests-*")
	if err != nil {
		return "", err
	}

	if err := writeFS(manifestsFS, dir); err != nil {
		return "", err
	}

	return dir, nil
})

func writeFS(f embed.FS, dir string) error {
	return fs.WalkDir(f, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip over the "base" directory as that's already been created.
		if p == "." {
			return nil
		}

		if d.IsDir() {
			return os.Mkdir(path.Join(dir, p), 0o775)
		}

		contents, err := fs.ReadFile(f, p)
		if err != nil {
			return err
		}

		return os.WriteFile(path.Join(dir, p), contents, 0o775)
	})
}
