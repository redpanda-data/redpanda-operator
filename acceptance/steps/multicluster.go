// Copyright 2026 Redpanda Data, Inc.
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
	"slices"
<<<<<<< HEAD
=======
	"strings"
	"sync"
>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))
	"time"

	"github.com/cucumber/godog"
<<<<<<< HEAD
=======
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))
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

<<<<<<< HEAD
=======
// dumpDiagnostics logs pod statuses and events from each vcluster to aid
// debugging when multicluster tests fail.
func (v vclusterNodes) dumpDiagnostics(_ context.Context, t framework.TestingT) {
	diagCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, node := range v {
		t.Logf("[multicluster-diagnostics] === vcluster %s (host namespace: %s) ===", node.Name(), node.Name())

		// Dump pods from the host namespace (where vcluster components run).
		hostClient, err := client.New(t.RestConfig(), client.Options{})
		if err != nil {
			t.Logf("[multicluster-diagnostics] failed to create host client: %v", err)
			continue
		}
		var hostPods corev1.PodList
		if err := hostClient.List(diagCtx, &hostPods, client.InNamespace(node.Name())); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list host pods: %v", err)
		} else {
			for _, pod := range hostPods.Items {
				t.Logf("[multicluster-diagnostics] host pod %s: phase=%s", pod.Name, pod.Status.Phase)
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.State.Waiting != nil {
						t.Logf("[multicluster-diagnostics]   container %s: waiting reason=%s", cs.Name, cs.State.Waiting.Reason)
					}
					if cs.State.Terminated != nil {
						t.Logf("[multicluster-diagnostics]   container %s: terminated exitCode=%d reason=%s", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
					}
					if cs.RestartCount > 0 {
						t.Logf("[multicluster-diagnostics]   container %s: restarts=%d", cs.Name, cs.RestartCount)
					}
				}
			}
		}

		// Dump pods from inside the vcluster (where the operator runs).
		var vcPods corev1.PodList
		if err := node.List(diagCtx, &vcPods); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list vcluster pods: %v", err)
		} else {
			for _, pod := range vcPods.Items {
				t.Logf("[multicluster-diagnostics] vcluster pod %s/%s: phase=%s", pod.Namespace, pod.Name, pod.Status.Phase)
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.State.Waiting != nil {
						t.Logf("[multicluster-diagnostics]   container %s: waiting reason=%s", cs.Name, cs.State.Waiting.Reason)
					}
					if cs.State.Terminated != nil {
						t.Logf("[multicluster-diagnostics]   container %s: terminated exitCode=%d reason=%s", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
					}
					if cs.RestartCount > 0 {
						t.Logf("[multicluster-diagnostics]   container %s: restarts=%d", cs.Name, cs.RestartCount)
					}
				}
			}
		}

		// Dump events from inside the vcluster.
		var vcEvents corev1.EventList
		if err := node.List(diagCtx, &vcEvents); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list vcluster events: %v", err)
		} else {
			for _, event := range vcEvents.Items {
				t.Logf("[multicluster-diagnostics] event %s %s/%s: %s", event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message)
			}
		}
	}
}

var nameMap = map[string]string{
	"vc-0": "first",
	"vc-1": "second",
	"vc-2": "third",
}

>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))
func (v vclusterNodes) ApplyAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, manifest))
	}
}

func (v vclusterNodes) DeleteAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		require.NoError(t, node.KubectlDelete(ctx, manifest))
	}
}

func (v vclusterNodes) CheckAll(ctx context.Context, namespacedName types.NamespacedName, groupVersionKind string, fn func(o client.Object) bool) {
	t := framework.T(ctx)

	gvk, _ := schema.ParseKindArg(groupVersionKind)
	for _, node := range v {
		ok := assert.Eventually(t, func() bool {
			obj, err := t.Scheme().New(*gvk)
			if err != nil {
				t.Logf("error creating object for GVK %v: %v", gvk, err)
				return false
			}

			o := obj.(client.Object)
			t.Logf("fetching (%T) object: %q from %s", o, namespacedName.String(), node.Name())
			if err := node.Get(ctx, namespacedName, o); err != nil {
				t.Logf("error fetching %q from %s: %v", namespacedName.String(), node.Name(), err)
				return false
			}
			return fn(o)
		}, 5*time.Minute, 1*time.Second, "condition not met on %s", node.Name())
		if !ok {
			v.dumpDiagnostics(ctx, t)
			t.FailNow()
		}
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

type multiclusterKey string

func stashNodes(ctx context.Context, name string, nodes vclusterNodes) context.Context {
	return context.WithValue(ctx, multiclusterKey(name), nodes)
}

func getNodes(ctx context.Context, name string) vclusterNodes {
	t := framework.T(ctx)

	nodes := ctx.Value(multiclusterKey(name))
	require.NotNil(t, nodes)
	return nodes.(vclusterNodes)
}

func iApplyKuberneteMulticlusterManifest(ctx context.Context, t framework.TestingT, clusterName string, manifest *godog.DocString) {
	nodes := getNodes(ctx, clusterName)
	nodes.ApplyAll(ctx, []byte(manifest.Content))
	t.Cleanup(func(ctx context.Context) {
		nodes.DeleteAll(ctx, []byte(manifest.Content))
	})
}

func checkMulticlusterFinalizers(ctx context.Context, t framework.TestingT, clusterName, name, namespace, groupVersionKind, finalizer string) {
	getNodes(ctx, clusterName).CheckAll(ctx, types.NamespacedName{Namespace: namespace, Name: name}, groupVersionKind, func(o client.Object) bool {
		return slices.Contains(o.GetFinalizers(), finalizer)
	})
}

func createNetworkedVClusterOperators(ctx context.Context, t framework.TestingT, clusterName string, clusters int32) context.Context {
	namespace := metav1.NamespaceDefault
<<<<<<< HEAD

	vclusters := []*vclusterNode{}
	t.Logf("creating %d vclusters", clusters)
	for i := range clusters {
		t.Logf("creating vcluster %d", i+1)
		cluster, err := vcluster.New(ctx, t.RestConfig())
		require.NoError(t, err)
		cluster.SetScheme(t.Scheme())
=======
	redpandaLicense := os.Getenv(LicenseEnvVar)
	require.NotEmpty(t, redpandaLicense, LicenseEnvVar+" env var must be set")

	// Build per-vcluster values upfront. The networking config references
	// other vclusters by name so names must be deterministic.
	vclusterValuesList := make([]string, clusters)
	for i := range clusters {
		vals := vcluster.DefaultValues
		switch i {
		case 0:
			vals += `
networking:
  replicateServices:
    fromHost:
    - from: vc-1/second-0-x-default-x-vc-1
      to: default/second-0
    - from: vc-2/third-0-x-default-x-vc-2
      to: default/third-0
`
		case 1:
			vals += `
networking:
  replicateServices:
    fromHost:
    - from: vc-0/first-0-x-default-x-vc-0
      to: default/first-0
    - from: vc-2/third-0-x-default-x-vc-2
      to: default/third-0
`
		case 2:
			vals += `
networking:
  replicateServices:
    fromHost:
    - from: vc-0/first-0-x-default-x-vc-0
      to: default/first-0
    - from: vc-1/second-0-x-default-x-vc-1
      to: default/second-0
`
		}
		vclusterValuesList[i] = vals
	}
>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))

	// Create all vclusters in parallel.
	t.Logf("creating %d vclusters in parallel", clusters)
	vclusters := make([]*vclusterNode, clusters)
	var wg sync.WaitGroup
	errs := make([]error, clusters)
	for i := range clusters {
		wg.Add(1)
		go func(idx int32) {
			defer wg.Done()
			t.Logf("creating vcluster %d", idx+1)

<<<<<<< HEAD
		t.Cleanup(func(ctx context.Context) {
			require.NoError(t, cluster.Delete())
		})
		c, err := cluster.Client(client.Options{Scheme: t.Scheme()})
		require.NoError(t, err)

		var apiServer corev1.Service
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, &apiServer))

		vclusters = append(vclusters, &vclusterNode{
			Client:    c,
			Cluster:   cluster,
			apiServer: fmt.Sprintf("https://%s", apiServer.Spec.ClusterIPs[0]),
=======
			cluster, err := vcluster.New(ctx, t.RestConfig(), vcluster.WithName(fmt.Sprintf("vc-%d", idx)), vcluster.WithValues(helm.RawYAML(vclusterValuesList[idx])))
			if err != nil {
				errs[idx] = fmt.Errorf("vcluster %d: %w", idx, err)
				return
			}
			scheme := t.Scheme()
			if err := certmanagerv1.AddToScheme(scheme); err != nil {
				errs[idx] = fmt.Errorf("vcluster %d scheme: %w", idx, err)
				return
			}
			cluster.SetScheme(scheme)

			t.Logf("finished creating vcluster %d (name: %q)", idx+1, cluster.Name())

			c, err := cluster.Client(client.Options{Scheme: t.Scheme()})
			if err != nil {
				errs[idx] = fmt.Errorf("vcluster %d client: %w", idx, err)
				return
			}

			var apiServer corev1.Service
			if err := c.Get(ctx, types.NamespacedName{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, &apiServer); err != nil {
				errs[idx] = fmt.Errorf("vcluster %d apiserver: %w", idx, err)
				return
			}

			vclusters[idx] = &vclusterNode{
				Client:    c,
				Cluster:   cluster,
				apiServer: fmt.Sprintf("https://%s", apiServer.Spec.ClusterIPs[0]),
			}
		}(i)
	}
	wg.Wait()

	// Check for errors and register cleanup.
	for i, err := range errs {
		require.NoError(t, err, "failed to create vcluster %d", i)
	}
	for _, vc := range vclusters {
		vc := vc // capture for closure
		cleanupWrapper(t, func(ctx context.Context) {
			require.NoError(t, vc.Cluster.Delete())
>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))
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
				// this is necessary since we don't mark the operators as
				// ready until the quorum forms, but in order to do so
				// they need to resolve the IP published here.
				PublishNotReadyAddresses: true,
			},
		}))

		require.Eventually(t, func() bool {
			var operatorService corev1.Service
			if err := cluster.Get(ctx, types.NamespacedName{Name: "multicluster-operator", Namespace: metav1.NamespaceDefault}, &operatorService); err != nil {
				t.Logf("error fetching operator service from %s: %v", cluster.Name(), err)
				return false
			}
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
<<<<<<< HEAD
=======
		t.Logf("creating license secret in %q", cluster.Name())
		require.NoError(t, cluster.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "redpanda-license",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"redpanda.license": []byte(redpandaLicense),
			},
		}))
		t.Logf("creating TLS secrets and issuers in %q", cluster.Name())
		require.NoError(t, cluster.Create(ctx, defaultSecret.DeepCopy()))
		require.NoError(t, cluster.Create(ctx, externalSecret.DeepCopy()))

		defaultIssuer := newCAIssuer("cluster", "default", namespace)
		externalIssuer := newCAIssuer("cluster", "external", namespace)
		webhookRetry := wait.Backoff{
			Steps:    100,
			Duration: 1 * time.Second,
			Factor:   1.0,
			Jitter:   0.1,
		}
		isWebhookErr := func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "webhook")
		}
		require.NoError(t, retry.OnError(webhookRetry, isWebhookErr, func() error {
			return cluster.Create(ctx, defaultIssuer)
		}))
		require.NoError(t, retry.OnError(webhookRetry, isWebhookErr, func() error {
			return cluster.Create(ctx, externalIssuer)
		}))

>>>>>>> ca834466 (Parallelize acceptance and integration tests (#1407))
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

	return stashNodes(ctx, clusterName, vclusters)
}
