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
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type vclusterNodes []*vclusterNode

func (v vclusterNodes) ApplyAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		require.NoError(t, node.KubectlApply(ctx, manifest))
	}
}

func (v vclusterNodes) DeleteAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		require.NoError(t, node.KubectlDelete(ctx, manifest))
	}
}

func (v vclusterNodes) CheckAll(ctx context.Context, namespacedName types.NamespacedName, groupVersionKind string, fn func(o client.Object)) {
	t := framework.T(ctx)

	gvk, _ := schema.ParseKindArg(groupVersionKind)
	for _, node := range v {
		obj, err := t.Scheme().New(*gvk)
		require.NoError(t, err)

		o := obj.(client.Object)
		require.NoError(t, node.Get(ctx, namespacedName, o))
		fn(o)
	}
}

type vclusterNode struct {
	client.Client
	*vcluster.Cluster
	apiServer  string
	externalIP string
}

func (n *vclusterNode) APIServer() string {
	return n.apiServer
}

func (n *vclusterNode) ExternalIP() string {
	return n.externalIP
}

func stashNodes(ctx context.Context, name string, nodes vclusterNodes) context.Context {
	return context.WithValue(ctx, name, nodes)
}

func getNodes(ctx context.Context, name string) vclusterNodes {
	t := framework.T(ctx)

	nodes := ctx.Value(name)
	require.NotNil(t, nodes)
	return nodes.(vclusterNodes)
}

func iApplyKuberneteMulticlusterManifest(ctx context.Context, t framework.TestingT, manifest *godog.DocString) {
	nodes := getNodes(ctx, "multicluster")
	nodes.ApplyAll(ctx, []byte(manifest.Content))
	t.Cleanup(func(ctx context.Context) {
		nodes.DeleteAll(ctx, []byte(manifest.Content))
	})
}

func checkMulticlusterFinalizers(ctx context.Context, t framework.TestingT, name, namespace, groupVersionKind, finalizer string) {
	getNodes(ctx, "multicluster").CheckAll(ctx, types.NamespacedName{Namespace: namespace, Name: name}, groupVersionKind, func(o client.Object) {
		require.Contains(t, o.GetFinalizers(), finalizer)
	})
}

func createNetworkedVClusterOperators(ctx context.Context, t framework.TestingT, clusters int32) context.Context {
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
			Cluster:   cluster,
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
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster %s never got operator cluster ip", cluster.Name()))
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
			KubeConfig:     cluster.RESTConfig(),
			APIServer:      cluster.APIServer(),
			ServiceAddress: cluster.ExternalIP(),
		})
		peers = append(peers, map[string]any{
			"name":    cluster.Name(),
			"address": cluster.ExternalIP(),
		})
	}
	t.Log("bootstrapping multicluster TLS")
	require.NoError(t, bootstrap.BootstrapKubernetesClusters(ctx, "redpanda-multicluster-operator", bootstrapConfig))

	// and finally we do the operator installation in each cluster
	for _, cluster := range vclusters {
		t.Logf("deploying operator in %q", cluster.Name())
		rel, err := cluster.HelmInstall(ctx, "../operator/chart", helm.InstallOptions{
			Name: "redpanda",
			Values: map[string]any{
				"crds": map[string]any{
					"enabled": true,
				},
				"multicluster": map[string]any{
					"enabled":                  true,
					"name":                     cluster.Name(),
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
		require.NoError(t, err)
		t.Cleanup(func(ctx context.Context) {
			require.NoError(t, cluster.HelmUninstall(ctx, rel))
		})
	}

	return stashNodes(ctx, "multicluster", vclusters)
}
