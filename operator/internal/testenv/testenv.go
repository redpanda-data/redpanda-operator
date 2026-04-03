// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testenv

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type Env struct {
	t                  *testing.T
	ctx                context.Context
	cancel             context.CancelFunc
	namespace          *corev1.Namespace
	logger             logr.Logger
	scheme             *runtime.Scheme
	group              *errgroup.Group
	host               *k3d.Cluster
	config             *rest.Config
	client             client.Client
	watchAllNamespaces bool
	Name               string
}

type Options struct {
	Name                string
	Agents              int
	SkipVCluster        bool
	SkipNamespaceClient bool
	// WatchAllNamespaces makes the controller manager watch all namespaces
	// instead of just the test namespace. This enables parallel tests that
	// each create their own namespace via [Env.CreateTestNamespace].
	WatchAllNamespaces bool
	Scheme             *runtime.Scheme
	CRDs               []*apiextensionsv1.CustomResourceDefinition
	Logger             logr.Logger
	Network            string
	Namespace          string
	ImportImages       []string
	Domain             string
	Port               int
	PortMappings       []k3d.PortMapping
}

// New returns a configured [Env] that utilizes an [vcluster.Cluster] in a
// shared k3d cluster.
//
// Due to the shared nature, the k3d cluster will NOT be shutdown at the end of
// tests. The vCluster will be deleted unless -retain is specified.
func New(t *testing.T, options Options) *Env {
	t.Helper()

	if options.Agents == 0 {
		options.Agents = 3
	}

	if options.Name == "" {
		options.Name = k3d.SharedClusterName
	}

	if options.Scheme == nil {
		options.Scheme = goclientscheme.Scheme
	}

	if options.Logger.IsZero() {
		options.Logger = logr.Discard()
	}

	opts := []k3d.ClusterOpt{k3d.WithAgents(options.Agents)}

	if options.Network != "" {
		opts = append(opts, k3d.WithNetwork(options.Network))
	}
	if options.Domain != "" {
		opts = append(opts, k3d.WithDomain(options.Domain))
	}
	if options.Port != 0 {
		opts = append(opts, k3d.WithPort(options.Port))
	}
	opts = append(opts, k3d.WithMappedPorts(options.PortMappings...))

	host, err := k3d.GetOrCreate(options.Name, opts...)
	require.NoError(t, err)

	for _, image := range options.ImportImages {
		options.Logger.Info("importing image", "image", image)
		require.NoError(t, host.ImportImage(image))
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec

	var cluster *vcluster.Cluster
	config := host.RESTConfig()

	if !options.SkipVCluster {
		cluster, err = vcluster.New(ctx, host.RESTConfig())
		require.NoError(t, err)
		config = cluster.RESTConfig()
	}

	if len(options.CRDs) > 0 {
		crds, err := envtest.InstallCRDs(config, envtest.CRDInstallOptions{
			CRDs:               dupCRDs(options.CRDs),
			ErrorIfPathMissing: false,
		})
		// Tolerate "already exists" errors when running with -p=N, since
		// multiple test packages may install CRDs into the same shared cluster.
		if err != nil && !k8sapierrors.IsAlreadyExists(err) && !strings.Contains(err.Error(), "already exists") {
			require.NoError(t, err)
		}
		if err == nil {
			require.Equal(t, len(options.CRDs), len(crds))
		}
	}

	c, err := client.New(config, client.Options{Scheme: options.Scheme})
	require.NoError(t, err)
	g, ctx := errgroup.WithContext(ctx)

	// Create a unique Namespace to perform tests within.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		GenerateName: "testenv-",
	}}

	if options.Namespace != "" {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: options.Namespace,
		}}
	}
	createErr := c.Create(ctx, ns)
	if !k8sapierrors.IsAlreadyExists(createErr) {
		require.NoError(t, createErr)
	}

	var otelClient client.Client
	if options.SkipNamespaceClient {
		otelClient = otelkube.NewClient(c)
	} else {
		otelClient = otelkube.NewClient(client.NewNamespacedClient(c, ns.Name))
	}

	env := &Env{
		t:                  t,
		scheme:             options.Scheme,
		logger:             options.Logger,
		namespace:          ns,
		group:              g,
		ctx:                ctx,
		cancel:             cancel,
		host:               host,
		config:             config,
		client:             otelClient,
		watchAllNamespaces: options.WatchAllNamespaces,
		Name:               options.Name,
	}

	if !options.SkipVCluster {
		t.Logf("Executing in namespace '%s' of vCluster '%s'", ns.Name, cluster.Name())
		t.Logf("Connect to vCluster using 'vcluster connect --namespace %s %s -- bash'", cluster.Name(), cluster.Name())
	} else {
		t.Logf("Executing in namespace '%s'", ns.Name)
	}

	t.Cleanup(func() {
		// Dump diagnostics before cleanup if the test failed.
		if t.Failed() {
			env.dumpDiagnostics()
		}

		env.cancel()
		assert.NoError(env.t, env.group.Wait())

		if !testutil.Retain() {
			if !options.SkipVCluster {
				require.NoError(t, cluster.Delete())
			}

			// Clean up any clusters that aren't shared.
			if env.host.Name != k3d.SharedClusterName {
				require.NoError(t, env.host.Cleanup())
			}
		}
	})

	return env
}

func (e *Env) Client() client.Client {
	return e.client
}

func (e *Env) RESTConfig() *rest.Config {
	return e.config
}

func (e *Env) Namespace() string {
	return e.namespace.Name
}

// TestNamespace represents an isolated namespace for a single test, with its
// own namespace-scoped client.
type TestNamespace struct {
	Name   string
	Client client.Client
}

// CreateTestNamespace creates a new isolated namespace for a single test and
// returns a namespace-scoped client. The namespace is deleted when the test
// completes (unless -retain is set). This enables parallel test execution
// within a shared testenv.
func (e *Env) CreateTestNamespace(t *testing.T) *TestNamespace {
	t.Helper()

	ctx := t.Context()

	// Create a unique namespace for this test.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		GenerateName: "testenv-",
	}}
	// Use a non-namespaced client for namespace creation.
	rawClient, err := client.New(e.config, client.Options{Scheme: e.scheme})
	require.NoError(t, err)
	require.NoError(t, rawClient.Create(ctx, ns))

	t.Logf("Created test namespace %q", ns.Name)

	nsClient := otelkube.NewClient(client.NewNamespacedClient(rawClient, ns.Name))

	t.Cleanup(func() {
		if t.Failed() {
			// Dump diagnostics for this namespace.
			envCopy := &Env{t: t, client: nsClient, namespace: ns}
			envCopy.dumpDiagnostics()
		}
		if !testutil.Retain() {
			if err := rawClient.Delete(context.Background(), ns); err != nil {
				t.Logf("WARNING: failed to delete namespace %s: %v", ns.Name, err)
			}
		}
	})

	return &TestNamespace{
		Name:   ns.Name,
		Client: nsClient,
	}
}

func (e *Env) SetupMulticlusterManager(serviceAccount string, address string, peers []multicluster.RaftCluster, fn func(multicluster.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	if serviceAccount != "" {
		config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)
	}

	manager, err := multicluster.NewRaftRuntimeManager(multicluster.RaftConfiguration{
		Name:               e.Name,
		Address:            address,
		Peers:              peers,
		RestConfig:         config,
		Scheme:             e.scheme,
		Logger:             e.logger,
		Insecure:           true,
		SkipNameValidation: true,
		ElectionTimeout:    1 * time.Second,
		HeartbeatInterval:  100 * time.Millisecond,
		BaseContext: func() context.Context {
			return e.ctx
		},
	})
	require.NoError(e.t, err)
	require.NoError(e.t, fn(manager))

	e.group.Go(func() error {
		if err := manager.Start(e.ctx); err != nil && e.ctx.Err() != nil {
			return err
		}
		return nil
	})
}

func (e *Env) SetupManager(serviceAccount string, fn func(multicluster.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	if serviceAccount != "" {
		config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)
	}

	// TODO: Webhooks likely aren't going to place nicely with this method of
	// testing. The Kube API server will have to dial out of the cluster to the
	// local machine which could prove to be difficult across all docker/docker
	// in docker environments.
	// See also https://k3d.io/v5.4.6/faq/faq/?h=host#how-to-access-services-like-a-database-running-on-my-docker-host-machine
	cacheOpts := cache.Options{}
	if !e.watchAllNamespaces {
		// Limit this manager to only interacting with objects within our
		// namespace.
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			e.namespace.Name: {},
		}
	}

	manager, err := multicluster.NewSingleClusterManager(config, ctrl.Options{
		Cache:   cacheOpts,
		Metrics: server.Options{BindAddress: "0"}, // Disable metrics server to avoid port conflicts.
		Scheme:  e.scheme,
		Logger:  e.logger,
		BaseContext: func() context.Context {
			return e.ctx
		},
	})
	require.NoError(e.t, err)

	require.NoError(e.t, fn(manager))

	e.group.Go(func() error {
		if err := manager.Start(e.ctx); err != nil && e.ctx.Err() != nil {
			return err
		}
		return nil
	})

	// No Without leader election enabled, this is just a wait for the manager
	// to start up.
	<-manager.Elected()
}

// dumpDiagnostics collects Kubernetes state from the test namespace to help
// debug failures. If INTEGRATION_ARTIFACTS_DIR is set, output is written to
// files; otherwise it is logged via t.Log.
func (e *Env) dumpDiagnostics() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	namespace := e.Namespace()
	testName := e.t.Name()
	e.t.Logf("=== DIAGNOSTICS for %s (namespace=%s) ===", testName, namespace)

	// Collect pod statuses.
	var podList corev1.PodList
	if err := e.client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		e.t.Logf("[diagnostics] error listing pods: %v", err)
	} else {
		for _, pod := range podList.Items {
			e.t.Logf("[diagnostics/pod] %s phase=%s reason=%s", pod.Name, pod.Status.Phase, pod.Status.Reason)
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					e.t.Logf("[diagnostics/pod] %s/%s waiting: %s - %s", pod.Name, cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
				if cs.State.Terminated != nil {
					e.t.Logf("[diagnostics/pod] %s/%s terminated: exitCode=%d reason=%s", pod.Name, cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
				}
			}
		}
	}

	// Collect events.
	var eventList corev1.EventList
	if err := e.client.List(ctx, &eventList, client.InNamespace(namespace)); err != nil {
		e.t.Logf("[diagnostics] error listing events: %v", err)
	} else {
		for _, event := range eventList.Items {
			e.t.Logf("[diagnostics/event] %s %s/%s: %s (count=%d)", event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message, event.Count)
		}
	}

	// Write to artifact directory if configured.
	artifactsDir := os.Getenv("INTEGRATION_ARTIFACTS_DIR")
	if artifactsDir == "" {
		return
	}

	dir := filepath.Join(artifactsDir, sanitizeTestName(testName))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		e.t.Logf("[diagnostics] failed to create artifacts directory: %v", err)
		return
	}

	// Dump pod descriptions.
	var podDescs []byte
	for _, pod := range podList.Items {
		podDescs = append(podDescs, []byte(fmt.Sprintf("--- Pod: %s (phase=%s) ---\n", pod.Name, pod.Status.Phase))...)
		for _, cs := range pod.Status.ContainerStatuses {
			podDescs = append(podDescs, []byte(fmt.Sprintf("  container %s: ready=%v restarts=%d\n", cs.Name, cs.Ready, cs.RestartCount))...)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "pods.txt"), podDescs, 0o600)

	// Dump events.
	var eventDescs []byte
	for _, event := range eventList.Items {
		eventDescs = append(eventDescs, []byte(fmt.Sprintf("%s %s/%s: %s (count=%d)\n", event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message, event.Count))...)
	}
	_ = os.WriteFile(filepath.Join(dir, "events.txt"), eventDescs, 0o600)
}

func sanitizeTestName(name string) string {
	// Replace path separators and non-filesystem-safe characters.
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return replacer.Replace(name)
}

func RandString(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	name := ""
	for i := 0; i < length; i++ {
		//nolint:gosec // not meant to be a secure random string.
		name += string(alphabet[rand.IntN(len(alphabet))])
	}

	return name
}

func dupCRDs(crds []*apiextensionsv1.CustomResourceDefinition) []*apiextensionsv1.CustomResourceDefinition {
	cloned := []*apiextensionsv1.CustomResourceDefinition{}
	for _, crd := range crds {
		cloned = append(cloned, crd.DeepCopy())
	}
	return cloned
}
