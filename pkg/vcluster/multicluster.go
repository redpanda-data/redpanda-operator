// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vcluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

// MulticlusterNode wraps a vcluster.Cluster with additional metadata needed
// for multicluster operator deployments.
type MulticlusterNode struct {
	*Cluster
	ctl        *kube.Ctl
	apiServer  string
	externalIP string
}

// APIServer returns the in-cluster API server address for this node.
func (n *MulticlusterNode) APIServer() string { return n.apiServer }

// ExternalIP returns the ClusterIP assigned to the operator service on this node.
func (n *MulticlusterNode) ExternalIP() string { return n.externalIP }

// Ctl returns a kube.Ctl client for this node.
func (n *MulticlusterNode) Ctl() *kube.Ctl { return n.ctl }

// Multicluster manages a set of vclusters configured for multicluster
// operator testing. It handles vcluster creation, cross-cluster service
// networking, TLS bootstrapping, and operator deployment.
type Multicluster struct {
	Nodes []*MulticlusterNode
	host  *k3d.Cluster
}

// MulticlusterOptions configures a multicluster vcluster environment.
type MulticlusterOptions struct {
	// Size is the number of vclusters to create. Defaults to 3.
	Size int
	// Namespace is the namespace within each vcluster for operator resources.
	// Defaults to "default".
	Namespace string
	// OperatorServiceName is the name of the Service created in front of the
	// operator's raft gRPC port. Defaults to "multicluster-operator".
	OperatorServiceName string
	// OperatorFullname is the helm fullname of the operator deployment on each
	// cluster (i.e. the Fullname produced by the operator chart for the chosen
	// release name). Used by BootstrapTLS to derive the TLS secret name
	// (<OperatorFullname>-multicluster-certificates) so it matches what the
	// helm chart creates. Defaults to "redpanda-operator", which corresponds
	// to a helm release named "redpanda" using the operator chart.
	OperatorFullname string
	// OperatorChartPath is the path to the operator helm chart. Required for
	// DeployOperators. Typically "../operator/chart" or similar.
	OperatorChartPath string
	// OperatorImage is the operator container image. Defaults to
	// "localhost/redpanda-operator:dev".
	OperatorImage string
	// OperatorTag is the operator image tag. Defaults to "dev".
	OperatorTag string
}

func (o *MulticlusterOptions) defaults() {
	if o.Size == 0 {
		o.Size = 3
	}
	if o.Namespace == "" {
		o.Namespace = metav1.NamespaceDefault
	}
	if o.OperatorServiceName == "" {
		o.OperatorServiceName = "multicluster-operator"
	}
	if o.OperatorFullname == "" {
		o.OperatorFullname = "redpanda-operator"
	}
	if o.OperatorImage == "" {
		o.OperatorImage = "localhost/redpanda-operator"
	}
	if o.OperatorTag == "" {
		o.OperatorTag = "dev"
	}
}

// NewMulticluster creates Size vclusters on the shared k3d cluster,
// configured with cross-cluster service replication so that operator pods
// can reach each other via ClusterIP services.
//
// Call BootstrapTLS to create TLS secrets, and DeployOperators to install
// the operator helm chart on each vcluster.
func NewMulticluster(t *testing.T, ctx context.Context, opts MulticlusterOptions) *Multicluster {
	t.Helper()
	opts.defaults()

	host, err := k3d.GetShared()
	require.NoError(t, err)

	nodes := createMulticlusterNodes(t, ctx, host, opts)
	assignOperatorServiceIPs(t, ctx, nodes, opts)

	env := &Multicluster{Nodes: nodes, host: host}

	t.Cleanup(func() {
		for _, node := range nodes {
			if err := node.Delete(); err != nil {
				t.Logf("error deleting vcluster %s: %v", node.Name(), err)
			}
		}
	})

	return env
}

// BootstrapTLS generates a shared CA and per-node TLS certificates, then
// distributes them as secrets across all vclusters. Returns the peer list
// suitable for passing to DeployOperators.
func (e *Multicluster) BootstrapTLS(t *testing.T, ctx context.Context, opts MulticlusterOptions) []map[string]any {
	t.Helper()
	opts.defaults()

	config := bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:      true,
		EnsureNamespace:   true,
		OperatorNamespace: opts.Namespace,
	}

	var peers []map[string]any
	for _, node := range e.Nodes {
		config.RemoteClusters = append(config.RemoteClusters, bootstrap.RemoteConfiguration{
			KubeConfig:     node.RESTConfig(),
			APIServer:      node.APIServer(),
			ServiceAddress: node.ExternalIP(),
			Name:           opts.OperatorFullname,
		})
		peers = append(peers, map[string]any{
			"name":    node.Name(),
			"address": node.ExternalIP(),
		})
	}

	t.Log("bootstrapping multicluster TLS")
	require.NoError(t, bootstrap.BootstrapKubernetesClusters(ctx, "redpanda-multicluster-operator", config))

	return peers
}

// ValuesFunc builds helm values for a single node. It receives the node so the
// caller can incorporate per-node identity (name, API server, etc.) into the
// typed chart values.
type ValuesFunc func(node *MulticlusterNode) any

// DeployOperators installs the operator helm chart on each vcluster. The
// valuesFunc is called per node to produce the helm values; use it to
// construct typed chart values (e.g. PartialValues) that pass schema
// validation.
func (e *Multicluster) DeployOperators(t *testing.T, ctx context.Context, opts MulticlusterOptions, valuesFunc ValuesFunc) {
	t.Helper()
	opts.defaults()
	require.NotEmpty(t, opts.OperatorChartPath, "OperatorChartPath is required")

	for _, node := range e.Nodes {
		t.Logf("deploying operator in %q", node.Name())
		rel, err := node.HelmInstall(ctx, opts.OperatorChartPath, helm.InstallOptions{
			Name:      "redpanda",
			Values:    valuesFunc(node),
			Namespace: opts.Namespace,
		})
		require.NoError(t, err)

		t.Cleanup(func() {
			if err := node.HelmUninstall(ctx, rel); err != nil {
				t.Logf("error uninstalling operator from %s: %v", node.Name(), err)
			}
		})
	}
}

// RESTConfigs returns the REST configs for all nodes.
func (e *Multicluster) RESTConfigs() []*rest.Config {
	configs := make([]*rest.Config, len(e.Nodes))
	for i, n := range e.Nodes {
		configs[i] = n.RESTConfig()
	}
	return configs
}

func createMulticlusterNodes(t *testing.T, ctx context.Context, host *k3d.Cluster, opts MulticlusterOptions) []*MulticlusterNode {
	t.Helper()

	nodes := make([]*MulticlusterNode, opts.Size)
	var wg sync.WaitGroup
	errs := make([]error, opts.Size)

	for i := range opts.Size {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			vcValues := DefaultValues + networkingValuesForMulticluster(int32(idx), int32(opts.Size))
			cluster, err := New(ctx, host.RESTConfig(),
				WithName(fmt.Sprintf("vc-%d", idx)),
				WithValues(helm.RawYAML(vcValues)),
			)
			if err != nil {
				errs[idx] = fmt.Errorf("creating vcluster %d: %w", idx, err)
				return
			}

			// Use PortForwardedRESTConfig so that the kube.Ctl has a
			// standard REST config (without custom Dial). This allows
			// SPDY-based operations like kubectl port-forward to work
			// against pods inside the vcluster.
			pfCfg, err := cluster.PortForwardedRESTConfig(ctx)
			if err != nil {
				errs[idx] = fmt.Errorf("creating port-forwarded config for vcluster %d: %w", idx, err)
				return
			}
			ctl, err := kube.FromRESTConfig(pfCfg)
			if err != nil {
				errs[idx] = fmt.Errorf("creating kube.Ctl for vcluster %d: %w", idx, err)
				return
			}

			var apiServer corev1.Service
			if err := ctl.Get(ctx, kube.ObjectKey{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, &apiServer); err != nil {
				errs[idx] = fmt.Errorf("getting API server service for vcluster %d: %w", idx, err)
				return
			}

			nodes[idx] = &MulticlusterNode{
				Cluster:   cluster,
				ctl:       ctl,
				apiServer: fmt.Sprintf("https://%s", apiServer.Spec.ClusterIPs[0]),
			}

			t.Logf("created vcluster %d (name: %q)", idx, cluster.Name())
		}(i)
	}

	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "creating vcluster %d", i)
	}
	return nodes
}

func assignOperatorServiceIPs(t *testing.T, ctx context.Context, nodes []*MulticlusterNode, opts MulticlusterOptions) {
	t.Helper()

	for _, node := range nodes {
		require.NoError(t, node.ctl.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opts.OperatorServiceName,
				Namespace: opts.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Protocol:   corev1.ProtocolTCP,
					Port:       9443,
					TargetPort: intstr.FromInt(9443),
				}},
				Selector: map[string]string{
					"app.kubernetes.io/instance": "redpanda",
					"app.kubernetes.io/name":     "operator",
				},
				PublishNotReadyAddresses: true,
			},
		}))

		require.Eventually(t, func() bool {
			var svc corev1.Service
			if err := node.ctl.Get(ctx, kube.ObjectKey{
				Name: opts.OperatorServiceName, Namespace: opts.Namespace,
			}, &svc); err != nil {
				return false
			}
			if len(svc.Spec.ClusterIPs) == 0 {
				return false
			}
			node.externalIP = svc.Spec.ClusterIPs[0]
			return true
		}, 3*time.Minute, 1*time.Second,
			"cluster %s never got operator service IP", node.Name())
	}
}

// networkingValuesForMulticluster generates vCluster networking YAML for
// cross-cluster service replication.
func networkingValuesForMulticluster(index, total int32) string {
	var entries []string
	for j := range total {
		if j == index {
			continue
		}
		vcName := fmt.Sprintf("vc-%d", j)
		entries = append(entries, fmt.Sprintf(
			"    - from: %s/multicluster-operator-x-default-x-%s\n      to: default/multicluster-operator-%s",
			vcName, vcName, vcName,
		))
	}
	if len(entries) == 0 {
		return ""
	}
	return fmt.Sprintf(`
networking:
  replicateServices:
    fromHost:
%s
`, strings.Join(entries, "\n"))
}
