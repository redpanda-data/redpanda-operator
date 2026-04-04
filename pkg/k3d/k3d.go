// Copyright 2026 Redpanda Data, Inc.
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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"golang.org/x/sys/unix"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	DefaultK3sImage   = `rancher/k3s:v1.32.13-k3s1`
	K3sImageEnv       = `K3S_IMAGE`
	SharedClusterName = "testenv"
)

var (
	//go:embed *.yaml
	manifestsFS embed.FS

	ErrExists = errors.New("cluster with name already exists")

	// usagePermitted controls whether or not the k3d package may be utilized.
	// To keep the development cycle of unittests short, usage of the k3d
	// package is disallowed. Setting either the integration or acceptance
	// build tag will toggle this variable to true.
	usagePermitted = false
)

type Cluster struct {
	Name string

	mu            sync.Mutex
	restConfig    *kube.RESTConfig
	agentCounter  int32
	skipManifests bool
}

type ClusterOpt interface {
	apply(config *clusterConfig)
}

type clusterOpt func(config *clusterConfig)

func (c clusterOpt) apply(config *clusterConfig) {
	c(config)
}

type clusterConfig struct {
	agents           int
	timeout          time.Duration
	image            string
	skipManifests    bool
	serverNoSchedule bool
	network          string
	domain           string
	port             int
	portMappings     []PortMapping
}

func defaultClusterConfig() *clusterConfig {
	image := DefaultK3sImage
	if override, ok := os.LookupEnv(K3sImageEnv); ok {
		image = override
	}

	return &clusterConfig{
		agents:  3,
		timeout: 5 * time.Minute,
		image:   image,
	}
}

func WithAgents(agents int) clusterOpt {
	return func(config *clusterConfig) {
		config.agents = agents
	}
}

func WithServerNoSchedule() clusterOpt {
	return func(config *clusterConfig) {
		config.serverNoSchedule = true
	}
}

func SkipManifestInstallation() clusterOpt {
	return func(config *clusterConfig) {
		config.skipManifests = true
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

func WithNetwork(network string) clusterOpt {
	return func(config *clusterConfig) {
		config.network = network
	}
}

func WithDomain(domain string) clusterOpt {
	return func(config *clusterConfig) {
		config.domain = domain
	}
}

func WithPort(port int) clusterOpt {
	return func(config *clusterConfig) {
		config.port = port
	}
}

type PortMapping struct {
	Host   int
	Target int
}

func WithMappedPorts(mappings ...PortMapping) clusterOpt {
	return func(config *clusterConfig) {
		config.portMappings = mappings
	}
}

// GetShared gets or creates the shared "testenv" k3d cluster. Most tests
// should use this method in combination with [vcluster.New].
//
// If your test needs to delete Nodes, DO NOT USE THE SHARED CLUSTER.
func GetShared() (*Cluster, error) {
	return GetOrCreate(SharedClusterName)
}

func GetOrCreate(name string, opts ...ClusterOpt) (*Cluster, error) {
	config := defaultClusterConfig()
	config.network = fmt.Sprintf("k3d-%s", name)
	for _, opt := range opts {
		opt.apply(config)
	}

	// Use a file-based lock to coordinate cluster creation across parallel
	// test processes (go test -p=N). The in-process sync.Mutex on Cluster
	// only protects goroutines within a single process.
	unlock, err := lockFile(name + "-create")
	if err != nil {
		return nil, errors.Wrap(err, "acquiring cluster lock")
	}
	defer unlock()

	cluster, err := NewCluster(name, opts...)
	if err != nil {
		if errors.Is(err, ErrExists) {
			return loadCluster(name, config)
		}
		return nil, err
	}

	if err := cluster.importImages("localhost/redpanda-operator:dev"); err != nil {
		return nil, err
	}

	return cluster, nil
}

// imageMarkerPath returns a file path used to track whether an image has
// already been imported into a given k3d cluster.
func imageMarkerPath(clusterName, image string) string {
	// Sanitize image name for use as a filename.
	safe := strings.NewReplacer("/", "_", ":", "_", ".", "_").Replace(image)
	return filepath.Join(os.TempDir(), fmt.Sprintf("k3d-%s-img-%s", clusterName, safe))
}

func imageAlreadyImported(clusterName, image string) bool {
	_, err := os.Stat(imageMarkerPath(clusterName, image))
	return err == nil
}

func markImageImported(clusterName, image string) {
	os.WriteFile(imageMarkerPath(clusterName, image), nil, 0o600) //nolint:errcheck
}

// lockFile acquires an exclusive file lock for the given cluster name.
// It returns an unlock function that must be called when the critical section is done.
func lockFile(name string) (func(), error) {
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("k3d-%s.lock", name))
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}

	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:errcheck
		f.Close()
	}, nil
}

func NewCluster(name string, opts ...ClusterOpt) (*Cluster, error) {
	if !usagePermitted {
		return nil, errors.New(`use of the k3d package is only permitted when using the integration or acceptance build tag.
Use testutils.SkipIfNotIntegration or testutils.SkipIfNotAcceptance to gate tests that use this package.`)
	}

	name = strings.ToLower(name)

	config := defaultClusterConfig()
	config.network = fmt.Sprintf("k3d-%s", name)
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
		// Disable bundled k3s components that we don't use in tests.
		`--k3s-arg`, `--disable=traefik@server:*`,
		`--k3s-arg`, `--disable=servicelb@server:*`,
		`--network`, config.network,
		`--verbose`,
	}

	for _, mapping := range config.portMappings {
		args = append(args, `--port`, fmt.Sprintf("%d:%d@loadbalancer", mapping.Host, mapping.Target))
	}

	if config.domain != "" {
		args = append(args, `--k3s-arg`, `--cluster-domain=`+config.domain+"@server:*")
	}

	if config.port != 0 {
		args = append(args, `--api-port`, strconv.Itoa(config.port))
	}

	if config.serverNoSchedule {
		args = append(args, []string{
			// This can be useful for tests in which we don't want to accidentally
			// kill the API server when we delete arbitrary nodes to simulate
			// hardware failures
			`--k3s-arg`, `--node-taint=server=true:NoSchedule@server:*`,
		}...)
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
		containerLogs, _ = exec.Command("docker", "network", "inspect", config.network).CombinedOutput()
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
		Name:          name,
		restConfig:    cfg,
		agentCounter:  int32(config.agents),
		skipManifests: config.skipManifests,
	}

	if err := cluster.waitForJobs(context.Background()); err != nil {
		return nil, err
	}

	return cluster, nil
}

func (c *Cluster) RESTConfig() *kube.RESTConfig {
	return c.restConfig
}

func (c *Cluster) ImportImage(images ...string) error {
	// Use a file-based lock to coordinate image imports across parallel
	// test processes. k3d creates a temporary container named
	// "k3d-<cluster>-tools" for imports which will conflict if multiple
	// processes import concurrently.
	unlock, err := lockFile(c.Name + "-import")
	if err != nil {
		return errors.Wrap(err, "acquiring cluster lock for image import")
	}
	defer unlock()

	return c.importImages(images...)
}

// importImages is the lock-free implementation of ImportImage, for use by
// callers that already hold a lock (e.g. GetOrCreate).
func (c *Cluster) importImages(images ...string) error {
	// Filter out images that have already been imported into this cluster
	// (by a previous test process). Uses marker files in /tmp to track.
	var needed []string
	for _, img := range images {
		if !imageAlreadyImported(c.Name, img) {
			needed = append(needed, img)
		}
	}
	if len(needed) == 0 {
		return nil
	}

	args := append([]string{"image", "import", fmt.Sprintf("--cluster=%s", c.Name)}, needed...)
	if out, err := exec.Command("k3d", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}

	// Mark all images as imported.
	for _, img := range needed {
		markImageImported(c.Name, img)
	}
	return nil
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

	return c.CreateNodeWithName(fmt.Sprintf("%s-agent-%d", c.Name, c.agentCounter))
}

func (c *Cluster) CreateNodeWithName(name string) error {
	if out, err := exec.Command(
		"k3d",
		"node",
		"create",
		name,
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

	if !c.skipManifests {
		// NB: Originally this functionality was achieved via the --volume flag to
		// k3d but CI runs via a docker in docker setup which makes it unreasonable
		// to use --volume.
		// Alternatively, we could make our own k3s images and directly copy in the
		// manifests in the Dockerfile.
		for _, obj := range startupManifests() {
			// we deep copy so we don't modify the startup manifests when multiple k3d objects are created
			cloned := obj.DeepCopyObject().(client.Object)
			if err := cl.Patch(ctx, cloned, client.Apply, client.FieldOwner(fmt.Sprintf("k3d-setup-%s", c.Name)), client.ForceOwnership); err != nil { //nolint:staticcheck // TODO: migrate to client.Client.Apply()
				return errors.WithStack(err)
			}
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
