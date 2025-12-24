// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type vclusterNode struct {
	client.Client
	cluster    *vcluster.Cluster
	apiServer  string
	externalIP string
}

func (n *vclusterNode) APIServer() string {
	return n.apiServer
}

func (n *vclusterNode) ExternalIP() string {
	return n.externalIP
}

func createNetworkedVClusterOperators(ctx context.Context, t framework.TestingT, clusters int32) {
	namespace := metav1.NamespaceDefault

	vclusters := []*vclusterNode{}
	t.Logf("creating %d vclusters", clusters)
	for i := range clusters {
		t.Logf("creating vcluster %d", i+1)
		cluster, err := vcluster.New(ctx, t.RestConfig())
		require.NoError(t, err)

		t.Logf("finished creating vcluster %d (name: %q)", i+1, cluster.Name())

		t.Cleanup(func(ctx context.Context) {
			require.NoError(t, cluster.Delete())
		})
		c, err := cluster.Client(client.Options{})

		var apiServer corev1.Service
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, &apiServer))

		vclusters = append(vclusters, &vclusterNode{
			Client:    c,
			cluster:   cluster,
			apiServer: fmt.Sprintf("https://%s", apiServer.Spec.ClusterIPs[0]),
		})
	}

	// create a cluster ip service for each of the operators so that we can address them ahead of time
	// statically by ip rather than using DNS
	// TODO: consider using a combination of Endpoint + Service for static references to mimic
	// global DNS a bit better. Also document that we'll need to know the network-resolvable DNS
	// of the operators prior to deploying them.
	for _, cluster := range vclusters {
		require.NoError(t, cluster.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "multicluster-operator",
				Namespace: namespace,
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
				// this is necessary since we can't see readiness
				// until the quorum forms
				PublishNotReadyAddresses: true,
			},
		}))

		require.Eventually(t, func() bool {
			var operatorService corev1.Service
			require.NoError(t, cluster.Get(ctx, types.NamespacedName{Name: "multicluster-operator", Namespace: metav1.NamespaceDefault}, &operatorService))
			if len(operatorService.Spec.ClusterIPs) == 0 {
				return false
			}
			cluster.externalIP = operatorService.Spec.ClusterIPs[0]
			return true
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster %s never got operator cluster ip", cluster.cluster.Name))
	}

	// next we bootstrap TLS for Raft in the cluster
	bootstrapConfig := bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:      true,
		EnsureNamespace:   true,
		OperatorNamespace: namespace,
		ServiceName:       "redpanda-operator-multicluster",
	}
	peers := []any{}
	for _, cluster := range vclusters {
		bootstrapConfig.RemoteClusters = append(bootstrapConfig.RemoteClusters, bootstrap.RemoteConfiguration{
			KubeConfig:     cluster.cluster.RESTConfig(),
			APIServer:      cluster.APIServer(),
			ServiceAddress: cluster.ExternalIP(),
		})
		peers = append(peers, map[string]any{
			"name":    cluster.cluster.Name(),
			"address": cluster.ExternalIP(),
		})
	}
	t.Log("bootstrapping multicluster TLS")
	require.NoError(t, bootstrap.BootstrapKubernetesClusters(ctx, "redpanda-multicluster-operator", bootstrapConfig))

	// and finally we do the operator installation in each cluster
	for _, cluster := range vclusters {
		t.Logf("deploying operator in %q", cluster.cluster.Name())
		rel := cluster.helmInstall(ctx, "../operator/chart", helm.InstallOptions{
			Name: "redpanda",
			Values: map[string]any{
				"crds": map[string]any{
					"enabled": true,
				},
				"multicluster": map[string]any{
					"enabled":                  true,
					"name":                     cluster.cluster.Name(),
					"apiServerExternalAddress": cluster.APIServer(),
					"peers":                    peers,
				},
				"image": map[string]any{
					"repository": "localhost/redpanda-operator",
					"tag":        "dev",
				},
			},
			Namespace: namespace,
		})
		t.Cleanup(func(ctx context.Context) {
			cluster.helmUninstall(ctx, rel)
		})
	}
}

// the functions below differ from our other helm mechanisms since they leverage helm as
// a library rather than using the CLI, this is necessary for VCluster since we
// do a bunch of hole punching and proxying that can't be persisted to disk.

func (c *vclusterNode) helmInstall(ctx context.Context, chartName string, options helm.InstallOptions) *release.Release {
	t := framework.T(ctx)

	actionConfig := new(action.Configuration)
	require.NoError(t, actionConfig.Init(c.cluster.AsRESTClientGetter(), options.Namespace, "secret", t.Logf))

	install := action.NewInstall(actionConfig)
	install.ReleaseName = options.Name
	install.Namespace = options.Namespace

	chart, err := loader.Load(chartName)
	require.NoError(t, err)

	rel, err := install.Run(chart, options.Values.(map[string]any))
	require.NoError(t, err)

	return rel
}

func (c *vclusterNode) helmUninstall(ctx context.Context, rel *release.Release) {
	t := framework.T(ctx)

	actionConfig := new(action.Configuration)
	require.NoError(t, actionConfig.Init(c.cluster.AsRESTClientGetter(), rel.Namespace, "secret", t.Logf))
	uninstall := action.NewUninstall(actionConfig)
	_, err := uninstall.Run(rel.Name)
	if err != nil && !strings.Contains(err.Error(), "release: not found") {
		require.NoError(t, err)
	}
}
