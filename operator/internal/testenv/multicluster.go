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
	"os/exec"
	"sync"
	"testing"

	"net"
	"strings"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// MulticlusterEnv wraps multiple [Env] instances configured as a Raft-based
// multicluster environment. It provides helpers for applying resources across
// all clusters and accessing the multicluster.Manager.
type MulticlusterEnv struct {
	Envs []*Env

	// Managers holds the multicluster.Manager per-env, indexed the same as Envs.
	// Only entries whose SetupFn was called will be non-nil.
	Managers []multicluster.Manager
}

// MulticlusterOptions configures a multicluster test environment.
type MulticlusterOptions struct {
	// Name prefix for the environments.
	Name string
	// ClusterSize is the number of clusters to create.
	ClusterSize int
	// Scheme for all clusters.
	Scheme *runtime.Scheme
	// CRDs to install on all clusters.
	CRDs []*apiextensionsv1.CustomResourceDefinition
	// Namespace is the shared namespace name across all clusters.
	Namespace string
	// Logger for all clusters.
	Logger logr.Logger
	// WatchAllNamespaces makes the controller manager watch all namespaces
	// instead of just the shared namespace. This enables parallel tests that
	// each create their own namespace via [MulticlusterEnv.CreateTestNamespace].
	WatchAllNamespaces bool
	// InstallCertManager installs cert-manager via helm on every k3d cluster.
	// Required when the test deploys resources that need TLS certificates.
	InstallCertManager bool
	// ImportImages is a list of docker images to pre-load into every k3d cluster.
	// Images must be available locally (e.g. built or pulled) before the test runs.
	ImportImages []string
	// SetupFn is called per-cluster with the multicluster.Manager. Use it to
	// register controllers. The first invocation's Manager is captured as the
	// "primary" manager.
	SetupFn func(multicluster.Manager) error
}

// NewMulticluster creates a multicluster test environment with the given options.
// It creates ClusterSize k3d-backed environments, configures Raft-based leader
// election, sets up RBAC, and registers controllers via SetupFn.
func NewMulticluster(t *testing.T, ctx context.Context, opts MulticlusterOptions) *MulticlusterEnv {
	t.Helper()

	if opts.ClusterSize == 0 {
		opts.ClusterSize = 3
	}
	if opts.Name == "" {
		opts.Name = "mc"
	}
	if opts.Namespace == "" {
		opts.Namespace = opts.Name
	}

	ports := testutil.FreePorts(t, opts.ClusterSize)

	// Pre-create k3d clusters in parallel to speed up bootstrapping.
	// Each cluster gets a unique pod/service CIDR so that pods can communicate
	// across clusters when routes are set up (flat networking).
	// k3d.GetOrCreate is safe to call concurrently (uses file-based locking).
	var wg sync.WaitGroup
	errs := make([]error, opts.ClusterSize)
	for i := range opts.ClusterSize {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Non-overlapping CIDRs: 10.{100+idx}.0.0/16 for pods, 10.{200+idx}.0.0/16 for services.
			clusterCIDR := fmt.Sprintf("10.%d.0.0/16", 100+idx)
			serviceCIDR := fmt.Sprintf("10.%d.0.0/16", 200+idx)
			_, errs[idx] = k3d.GetOrCreate(
				fmt.Sprintf("%s-%d", opts.Name, idx),
				k3d.WithAgents(1),
				k3d.WithNetwork(opts.Name),
				k3d.WithCIDRs(clusterCIDR, serviceCIDR),
			)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "creating k3d cluster %d", i)
	}

	// Set up flat networking: add routes on each k3d node so pods can reach
	// pods on other clusters via their non-overlapping CIDRs.
	setupFlatNetworking(t, opts.Name, opts.ClusterSize)

	envs := make([]*Env, opts.ClusterSize)
	for i := range opts.ClusterSize {
		envs[i] = New(t, Options{
			Name:               fmt.Sprintf("%s-%d", opts.Name, i),
			Agents:             1,
			Scheme:             opts.Scheme,
			CRDs:               opts.CRDs,
			Network:            opts.Name,
			Namespace:          opts.Namespace,
			Logger:             opts.Logger.WithName(fmt.Sprintf("%s-%d", opts.Name, i)),
			ImportImages:       opts.ImportImages,
			WatchAllNamespaces: opts.WatchAllNamespaces,
			SkipVCluster:       true,
		})
	}

	if opts.InstallCertManager {
		var cmWg sync.WaitGroup
		cmErrs := make([]error, opts.ClusterSize)
		for i, env := range envs {
			cmWg.Add(1)
			go func(idx int, env *Env) { //nolint:gosec // ctx is suite-scoped, not request-scoped
				defer cmWg.Done()
				hc, err := helm.New(helm.Options{KubeConfig: env.RESTConfig()})
				if err != nil {
					cmErrs[idx] = fmt.Errorf("creating helm client for cluster %d: %w", idx, err)
					return
				}
				if err := hc.RepoAdd(ctx, "jetstack", "https://charts.jetstack.io"); err != nil {
					cmErrs[idx] = fmt.Errorf("adding jetstack repo on cluster %d: %w", idx, err)
					return
				}
				if _, err := hc.Upgrade(ctx, "cert-manager", "jetstack/cert-manager", helm.UpgradeOptions{
					Install:         true,
					CreateNamespace: true,
					Namespace:       "cert-manager",
					Version:         "v1.17.2",
					Values: map[string]any{
						"installCRDs": true,
					},
				}); err != nil {
					cmErrs[idx] = fmt.Errorf("installing cert-manager on cluster %d: %w", idx, err)
				}
			}(i, env)
		}
		cmWg.Wait()
		for i, err := range cmErrs {
			require.NoError(t, err, "installing cert-manager on cluster %d", i)
		}
	}

	peers := make([]multicluster.RaftCluster, opts.ClusterSize)
	for i, env := range envs {
		peers[i] = multicluster.RaftCluster{
			Name:       env.Name,
			Address:    fmt.Sprintf("127.0.0.1:%d", ports[i]),
			Kubeconfig: env.RESTConfig(),
		}
	}

	managers := make([]multicluster.Manager, opts.ClusterSize)
	for i, env := range envs {
		idx := i
		sa := setupMulticlusterRBAC(t, ctx, env)
		env.SetupMulticlusterManager(
			sa,
			fmt.Sprintf("127.0.0.1:%d", ports[i]),
			peers,
			func(mgr multicluster.Manager) error {
				managers[idx] = mgr
				if opts.SetupFn != nil {
					return opts.SetupFn(mgr)
				}
				return nil
			},
		)
	}

	return &MulticlusterEnv{
		Envs:     envs,
		Managers: managers,
	}
}

// PrimaryManager returns the first non-nil Manager. Panics if none exist.
func (m *MulticlusterEnv) PrimaryManager() multicluster.Manager {
	for _, mgr := range m.Managers {
		if mgr != nil {
			return mgr
		}
	}
	panic("no managers registered")
}

// MulticlusterTestNamespace holds per-cluster namespace-scoped clients for a
// single test namespace that exists across all clusters.
type MulticlusterTestNamespace struct {
	Name    string
	Clients []client.Client
}

// CreateTestNamespace creates a unique namespace on every cluster and returns
// namespace-scoped clients. The namespace is cleaned up when the test ends.
func (m *MulticlusterEnv) CreateTestNamespace(t *testing.T) *MulticlusterTestNamespace {
	t.Helper()
	// Use the first env to generate a namespace (they all share the same scheme).
	tn := m.Envs[0].CreateTestNamespace(t)

	clients := []client.Client{tn.Client}

	// Create the same namespace on remaining clusters.
	for _, env := range m.Envs[1:] {
		ctx := t.Context()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tn.Name}}
		rawClient, err := client.New(env.RESTConfig(), client.Options{Scheme: env.Client().Scheme()})
		require.NoError(t, err)
		require.NoError(t, rawClient.Create(ctx, ns))

		nsClient := client.NewNamespacedClient(rawClient, tn.Name)
		clients = append(clients, nsClient)

		t.Cleanup(func() {
			if !testutil.Retain() {
				_ = rawClient.Delete(context.Background(), ns)
			}
		})
	}

	return &MulticlusterTestNamespace{
		Name:    tn.Name,
		Clients: clients,
	}
}

// DeleteAll deletes all instances of the given object types across all clusters
// in the given namespace.
func (m *MulticlusterEnv) DeleteAll(t *testing.T, ctx context.Context, ns string, objs ...client.Object) {
	t.Helper()
	for _, env := range m.Envs {
		for _, obj := range objs {
			require.NoError(t, env.Client().DeleteAllOf(ctx, obj, client.InNamespace(ns)))
		}
	}
}

// ApplyAll applies the given objects to every cluster using server-side apply.
func (m *MulticlusterEnv) ApplyAll(t *testing.T, ctx context.Context, objs ...client.Object) {
	t.Helper()
	for _, env := range m.Envs {
		for _, obj := range objs {
			gvk, err := env.Client().GroupVersionKindFor(obj)
			require.NoError(t, err)
			obj.SetManagedFields(nil)
			obj.SetResourceVersion("")
			obj.GetObjectKind().SetGroupVersionKind(gvk)
			require.NoError(t, env.Client().Patch(ctx, obj.DeepCopyObject().(client.Object), client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO
		}
	}
}

// ApplyAllInNamespace applies objects to every cluster in a specific namespace.
func (m *MulticlusterEnv) ApplyAllInNamespace(t *testing.T, ctx context.Context, ns string, objs ...client.Object) {
	t.Helper()
	for _, obj := range objs {
		obj.SetNamespace(ns)
	}
	m.ApplyAll(t, ctx, objs...)
}

// DialContext implements a multicluster-aware pod dialer. It resolves service
// names to actual pod names by looking up Endpoints across all clusters, then
// delegates to the appropriate cluster's PodDialer.
//
// The PodDialer expects addresses like "pod-name.namespace:port" but stretch
// cluster services use names like "pool-0-0.sc-factory:9644" where "pool-0-0"
// is a service name, not a pod name. This wrapper resolves the service to its
// backing pod by looking up the Endpoints object.
func (m *MulticlusterEnv) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Parse host:port
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	// Parse service name and namespace from the host.
	// Handles: "svc.ns", "svc.ns.svc.cluster.local", etc.
	parts := strings.Split(host, ".")
	svcName := parts[0]
	ns := "default"
	if len(parts) >= 2 {
		ns = parts[1]
	}

	// Look up the Endpoints object for this service across all clusters
	// to find the actual backing pod name.
	for i, env := range m.Envs {
		var ep corev1.Endpoints
		if err := env.Client().Get(ctx, client.ObjectKey{Name: svcName, Namespace: ns}, &ep); err != nil {
			continue
		}
		for _, subset := range ep.Subsets {
			for _, addr := range subset.Addresses {
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					// Found the pod — dial it via this cluster's PodDialer.
					podAddress := net.JoinHostPort(addr.TargetRef.Name+"."+ns, port)
					dialer := kube.NewPodDialer(m.Envs[i].RESTConfig())
					return dialer.DialContext(ctx, network, podAddress)
				}
			}
		}
		// Endpoints exist but no TargetRef (e.g. manually created for flat networking).
		// The addresses are pod IPs — find which cluster has a pod with that IP.
		for _, subset := range ep.Subsets {
			for _, addr := range subset.Addresses {
				podName, podClusterIdx := m.findPodByIP(ctx, ns, addr.IP)
				if podName != "" {
					podAddress := net.JoinHostPort(podName+"."+ns, port)
					dialer := kube.NewPodDialer(m.Envs[podClusterIdx].RESTConfig())
					return dialer.DialContext(ctx, network, podAddress)
				}
			}
		}
	}

	// Fallback: try each cluster's PodDialer directly (maybe the service name IS the pod name).
	var lastErr error
	for _, env := range m.Envs {
		dialer := kube.NewPodDialer(env.RESTConfig())
		conn, err := dialer.DialContext(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("multicluster dial failed for %s: %w", address, lastErr)
}

// findPodByIP searches all clusters for a pod with the given IP in the given namespace.
func (m *MulticlusterEnv) findPodByIP(ctx context.Context, namespace, ip string) (string, int) {
	for i, env := range m.Envs {
		var pods corev1.PodList
		if err := env.Client().List(ctx, &pods, client.InNamespace(namespace)); err != nil {
			continue
		}
		for _, pod := range pods.Items {
			if pod.Status.PodIP == ip {
				return pod.Name, i
			}
		}
	}
	return "", -1
}

// setupFlatNetworking adds routes between all k3d cluster nodes so that pods
// on different clusters can reach each other via their non-overlapping CIDRs.
// Each cluster i uses pod CIDR 10.{100+i}.0.0/16. We add a route on every node
// in cluster i pointing 10.{100+j}.0.0/16 to the server node of cluster j.
func setupFlatNetworking(t *testing.T, name string, clusterSize int) {
	t.Helper()

	// Resolve the Docker-internal IP of each cluster's server node.
	serverIPs := make([]string, clusterSize)
	for i := range clusterSize {
		container := fmt.Sprintf("k3d-%s-%d-server-0", name, i)
		out, err := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", container).Output()
		require.NoError(t, err, "inspecting container %s", container)
		serverIPs[i] = string(out[:len(out)-1]) // trim trailing newline
	}

	// For each cluster, add routes to every other cluster's pod CIDR via that cluster's server IP.
	for i := range clusterSize {
		// All node containers for cluster i: server + agents.
		nodes := []string{fmt.Sprintf("k3d-%s-%d-server-0", name, i)}
		nodes = append(nodes, fmt.Sprintf("k3d-%s-%d-agent-0", name, i))

		for j := range clusterSize {
			if i == j {
				continue
			}
			cidr := fmt.Sprintf("10.%d.0.0/16", 100+j)
			for _, node := range nodes {
				// Ignore errors — route may already exist from a previous run.
				_ = exec.Command("docker", "exec", node, "ip", "route", "add", cidr, "via", serverIPs[j]).Run()
			}
		}
	}
}

// FixupCrossClusterServices patches per-pod Services on each cluster that
// reference pods running on OTHER clusters. It removes the selector (so
// kube doesn't fight with us) and creates an EndpointSlice pointing
// to the remote pod's IP. This is the equivalent of vCluster's
// replicateServices for bare k3d multicluster setups.
func (m *MulticlusterEnv) FixupCrossClusterServices(t *testing.T, ctx context.Context, namespace string) {
	t.Helper()

	// Build a map of pod name -> pod IP by scanning all clusters.
	podIPs := map[string]string{}
	for _, env := range m.Envs {
		var pods corev1.PodList
		require.NoError(t, env.Client().List(ctx, &pods, client.InNamespace(namespace)))
		for _, pod := range pods.Items {
			if pod.Status.PodIP != "" {
				podIPs[pod.Name] = pod.Status.PodIP
			}
		}
	}

	// For each cluster, find per-pod services with no matching local endpoints
	// and point them at the correct remote pod IP via an EndpointSlice.
	for _, env := range m.Envs {
		var svcs corev1.ServiceList
		require.NoError(t, env.Client().List(ctx, &svcs, client.InNamespace(namespace)))

		for _, svc := range svcs.Items {
			podIP, ok := podIPs[svc.Name]
			if !ok {
				continue // not a per-pod service
			}

			// Check if this service already has a working endpoint locally.
			var epSlices discoveryv1.EndpointSliceList
			require.NoError(t, env.Client().List(ctx, &epSlices,
				client.InNamespace(namespace),
				client.MatchingLabels{discoveryv1.LabelServiceName: svc.Name},
			))
			hasReady := false
			for _, eps := range epSlices.Items {
				for _, ep := range eps.Endpoints {
					if len(ep.Addresses) > 0 && ptr.Deref(ep.Conditions.Ready, false) {
						hasReady = true
						break
					}
				}
			}
			if hasReady {
				continue // local pod serves this service, no fixup needed
			}

			// Remove selector so kube doesn't auto-manage endpoints.
			svc.Spec.Selector = nil
			require.NoError(t, env.Client().Update(ctx, &svc))

			// Build EndpointSlice ports from the service ports.
			var ports []discoveryv1.EndpointPort
			for _, p := range svc.Spec.Ports {
				p := p
				ports = append(ports, discoveryv1.EndpointPort{
					Name:     &p.Name,
					Port:     &p.Port,
					Protocol: &p.Protocol,
				})
			}

			epSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svc.Name + "-cross-cluster",
					Namespace: namespace,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svc.Name,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses:  []string{podIP},
					Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
				}},
				Ports: ports,
			}

			existing := &discoveryv1.EndpointSlice{}
			if err := env.Client().Get(ctx, client.ObjectKeyFromObject(epSlice), existing); err == nil {
				existing.Endpoints = epSlice.Endpoints
				existing.Ports = epSlice.Ports
				require.NoError(t, env.Client().Update(ctx, existing))
			} else {
				require.NoError(t, env.Client().Create(ctx, epSlice))
			}

			t.Logf("Fixed up cross-cluster service %s on %s -> %s", svc.Name, env.Name, podIP)
		}
	}
}

// setupMulticlusterRBAC creates a ServiceAccount with cluster-admin privileges
// for running the operator in tests.
func setupMulticlusterRBAC(t *testing.T, ctx context.Context, env *Env) string {
	t.Helper()

	name := "testenv-" + RandString(6)

	apply := func(objs ...client.Object) {
		for _, obj := range objs {
			gvk, err := env.Client().GroupVersionKindFor(obj)
			require.NoError(t, err)
			obj.SetManagedFields(nil)
			obj.SetResourceVersion("")
			obj.GetObjectKind().SetGroupVersionKind(gvk)
			require.NoError(t, env.Client().Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO
		}
	}

	apply(
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name}},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: env.Namespace(), Name: name}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"},
		},
	)

	return name
}
