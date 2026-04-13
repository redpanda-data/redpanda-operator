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
	"bytes"
	"context"
	"fmt"
	//"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/cucumber/godog"
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	//"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	//"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

const (
	LicenseEnvVar = "REDPANDA_SAMPLE_LICENSE"

	operatorServiceName = "multicluster-operator"
	operatorGRPCPort    = 9443
	licenseSecretName   = "redpanda-license"
	redpandaLabel       = "app.kubernetes.io/name"
	redpandaLabelValue  = "redpanda"
	caLifetime          = 48 * time.Hour
)

type vclusterNodes []*vclusterNode

// dumpDiagnostics logs pod statuses and events from each vcluster to aid
// debugging when multicluster tests fail.
func (v vclusterNodes) dumpDiagnostics(_ context.Context, t framework.TestingT) {
	//diagCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	//defer cancel()
	//
	//for _, node := range v {
	//	t.Logf("[multicluster-diagnostics] === vcluster %s (host namespace: %s) ===", node.Name(), node.Name())
	//
	//	// Dump pods from the host namespace (where vcluster components run).
	//	hostClient, err := client.New(t.RestConfig(), client.Options{})
	//	if err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to create host client: %v", err)
	//		continue
	//	}
	//	var hostPods corev1.PodList
	//	if err := hostClient.List(diagCtx, &hostPods, client.InNamespace(node.Name())); err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to list host pods: %v", err)
	//	} else {
	//		for _, pod := range hostPods.Items {
	//			t.Logf("[multicluster-diagnostics] host pod %s: phase=%s", pod.Name, pod.Status.Phase)
	//			for _, cs := range pod.Status.ContainerStatuses {
	//				if cs.State.Waiting != nil {
	//					t.Logf("[multicluster-diagnostics]   container %s: waiting reason=%s", cs.Name, cs.State.Waiting.Reason)
	//				}
	//				if cs.State.Terminated != nil {
	//					t.Logf("[multicluster-diagnostics]   container %s: terminated exitCode=%d reason=%s", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
	//				}
	//				if cs.RestartCount > 0 {
	//					t.Logf("[multicluster-diagnostics]   container %s: restarts=%d", cs.Name, cs.RestartCount)
	//				}
	//			}
	//		}
	//	}
	//
	//	// Dump pods from inside the vcluster (where the operator runs).
	//	var vcPods corev1.PodList
	//	if err := node.List(diagCtx, &vcPods); err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to list vcluster pods: %v", err)
	//	} else {
	//		for _, pod := range vcPods.Items {
	//			t.Logf("[multicluster-diagnostics] vcluster pod %s/%s: phase=%s", pod.Namespace, pod.Name, pod.Status.Phase)
	//			for _, cs := range pod.Status.ContainerStatuses {
	//				if cs.State.Waiting != nil {
	//					t.Logf("[multicluster-diagnostics]   container %s: waiting reason=%s", cs.Name, cs.State.Waiting.Reason)
	//				}
	//				if cs.State.Terminated != nil {
	//					t.Logf("[multicluster-diagnostics]   container %s: terminated exitCode=%d reason=%s", cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
	//				}
	//				if cs.RestartCount > 0 {
	//					t.Logf("[multicluster-diagnostics]   container %s: restarts=%d", cs.Name, cs.RestartCount)
	//				}
	//			}
	//		}
	//	}
	//
	//	// Dump events from inside the vcluster.
	//	var vcEvents corev1.EventList
	//	if err := node.List(diagCtx, &vcEvents); err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to list vcluster events: %v", err)
	//	} else {
	//		for _, event := range vcEvents.Items {
	//			t.Logf("[multicluster-diagnostics] event %s %s/%s: %s", event.Type, event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Message)
	//		}
	//	}
	//
	//	// Dump StretchCluster objects.
	//	var scList redpandav1alpha2.StretchClusterList
	//	if err := node.List(diagCtx, &scList); err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to list StretchClusters: %v", err)
	//	} else {
	//		for _, sc := range scList.Items {
	//			t.Logf("[multicluster-diagnostics] StretchCluster %s/%s: finalizers=%v, conditions=%d, generation=%d",
	//				sc.Namespace, sc.Name, sc.Finalizers, len(sc.Status.Conditions), sc.Generation)
	//			for _, cond := range sc.Status.Conditions {
	//				t.Logf("[multicluster-diagnostics]   condition %s=%s reason=%s: %s", cond.Type, cond.Status, cond.Reason, cond.Message)
	//			}
	//		}
	//	}
	//
	//	// Dump NodePools.
	//	var npList redpandav1alpha2.NodePoolList
	//	if err := node.List(diagCtx, &npList); err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to list NodePools: %v", err)
	//	} else {
	//		for _, np := range npList.Items {
	//			t.Logf("[multicluster-diagnostics] NodePool %s/%s: replicas=%d, conditions=%d",
	//				np.Namespace, np.Name, ptr.Deref(np.Spec.Replicas, 0), len(np.Status.Conditions))
	//		}
	//	}
	//
	//	// Dump operator pod logs (last 100 lines).
	//	k8sClient, err := kubernetes.NewForConfig(node.RESTConfig())
	//	if err != nil {
	//		t.Logf("[multicluster-diagnostics] failed to create k8s client for logs: %v", err)
	//		continue
	//	}
	//	for _, pod := range vcPods.Items {
	//		if !strings.Contains(pod.Name, "operator") {
	//			continue
	//		}
	//		tailLines := int64(100)
	//		req := k8sClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: &tailLines})
	//		logStream, err := req.Stream(diagCtx)
	//		if err != nil {
	//			t.Logf("[multicluster-diagnostics] failed to get logs for %s: %v", pod.Name, err)
	//			continue
	//		}
	//		logBytes, _ := io.ReadAll(logStream)
	//		_ = logStream.Close()
	//		t.Logf("[multicluster-diagnostics] === operator logs %s (last 100 lines) ===\n%s", pod.Name, string(logBytes))
	//	}
	//}
}

var nameMap = map[string]string{
	"vc-0": "first",
	"vc-1": "second",
	"vc-2": "third",
}

func (v vclusterNodes) ApplyAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, manifest))
	}
}

func nodepoolManifest(nodeName string, manifest *godog.DocString) []byte {
	return []byte(fmt.Sprintf(`
apiVersion: cluster.redpanda.com/v1alpha2
kind: NodePool
metadata:
  name: %s
  namespace: default
`, nodeName) + manifest.Content)
}

func (v vclusterNodes) ApplyNodepoolsWithDifferentNamePerCluster(ctx context.Context, manifest *godog.DocString) {
	t := framework.T(ctx)
	for _, node := range v {
		fullManifest := nodepoolManifest(nameMap[node.Name()], manifest)
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, fullManifest))
	}
}

func (v vclusterNodes) DeleteNodepools(ctx context.Context, manifest *godog.DocString) {
	t := framework.T(ctx)
	for _, node := range v {
		fullManifest := nodepoolManifest(nameMap[node.Name()], manifest)
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlDelete(ctx, fullManifest))
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
	require.NotNil(t, gvk, "failed to parse GVK %q", groupVersionKind)

	for _, node := range v {
		ok := assert.Eventually(t, func() bool {
			obj, err := t.Scheme().New(*gvk)
			if err != nil {
				t.Logf("error creating object for GVK %s: %v", groupVersionKind, err)
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

type (
	multiclusterKey         string
	lastMulticlusterNameKey struct{}
	rpkResultsKey           struct{}
)

type rpkExecResult struct {
	clusterName string
	rawOutput   string
}

func stashNodes(ctx context.Context, name string, nodes vclusterNodes) context.Context {
	ctx = context.WithValue(ctx, multiclusterKey(name), nodes)
	return context.WithValue(ctx, lastMulticlusterNameKey{}, name)
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
	cleanupWrapper(t, func(ctx context.Context) {
		nodes.DeleteAll(ctx, []byte(manifest.Content))
	})
}

func applyNodePoolWithStretchCluster(ctx context.Context, t framework.TestingT, clusterName string, manifest *godog.DocString) {
	nodes := getNodes(ctx, clusterName)
	nodes.ApplyNodepoolsWithDifferentNamePerCluster(ctx, manifest)
	cleanupWrapper(t, func(ctx context.Context) {
		nodes.DeleteNodepools(ctx, manifest)
	})
}

func checkMulticlusterFinalizers(ctx context.Context, t framework.TestingT, clusterName, name, namespace, groupVersionKind, finalizer string) {
	nn := types.NamespacedName{Namespace: namespace, Name: name}
	nodes := getNodes(ctx, clusterName)

	nodes.CheckAll(ctx, nn, groupVersionKind, func(o client.Object) bool {
		return slices.Contains(o.GetFinalizers(), finalizer)
	})

	// After the finalizer is set, the reconciler should have also set SpecSynced=True.
	for _, node := range nodes {
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := node.Get(ctx, nn, &sc); err != nil {
				t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, "SpecSynced")
			if cond == nil {
				t.Logf("SpecSynced condition not yet present on %s", node.Name())
				return false
			}
			if cond.Status != metav1.ConditionTrue {
				t.Logf("SpecSynced=%s on %s: %s", cond.Status, node.Name(), cond.Message)
				return false
			}
			return true
		}, 5*time.Minute, 1*time.Second, "SpecSynced=True condition never appeared on %s", node.Name())
	}
}

func createNetworkedVClusterOperators(ctx context.Context, t framework.TestingT, clusterName string, clusters int32) context.Context {
	namespace := metav1.NamespaceDefault
	redpandaLicense := os.Getenv(LicenseEnvVar)
	require.NotEmpty(t, redpandaLicense, LicenseEnvVar+" env var must be set")

	vclusters := createVClusters(ctx, t, clusters)
	t.Cleanup(func(ctx context.Context) {
		vclusterNodes(vclusters).dumpDiagnostics(ctx, t)
	})
	assignOperatorServiceIPs(ctx, t, vclusters, namespace)
	peers := bootstrapTLS(ctx, t, vclusters, namespace)
	deployOperators(ctx, t, vclusters, namespace, redpandaLicense, peers)

	return stashNodes(ctx, clusterName, vclusters)
}

func createVClusters(ctx context.Context, t framework.TestingT, clusters int32) []*vclusterNode {
	t.Logf("creating %d vclusters", clusters)

	nodes := make([]*vclusterNode, clusters)
	var wg sync.WaitGroup

	for i := range clusters {
		wg.Add(1)
		go func(i int32) {
			defer wg.Done()

			vClusterValues := vcluster.DefaultValues + networkingValues(i, clusters)
			cluster, err := vcluster.New(ctx, t.RestConfig(), vcluster.WithName(fmt.Sprintf("vc-%d", i)), vcluster.WithValues(helm.RawYAML(vClusterValues)))
			require.NoError(t, err)
			scheme := t.Scheme()
			require.NoError(t, certmanagerv1.AddToScheme(scheme))
			cluster.SetScheme(scheme)

			t.Logf("finished creating vcluster %d (name: %q)", i+1, cluster.Name())

			cleanupWrapper(t, func(ctx context.Context) {
				if err := cluster.Delete(); err != nil {
					t.Logf("error deleting cluster %s: %v", cluster.Name(), err)
				}
			})
			c, err := cluster.Client(client.Options{Scheme: t.Scheme()})
			require.NoError(t, err)

			var apiServer corev1.Service
			require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "kubernetes", Namespace: metav1.NamespaceDefault}, &apiServer))

			nodes[i] = &vclusterNode{
				Client:    c,
				Cluster:   cluster,
				apiServer: fmt.Sprintf("https://%s", apiServer.Spec.ClusterIPs[0]),
			}
		}(i)
	}

	wg.Wait()
	return nodes
}

// networkingValues generates the vCluster networking YAML for cross-cluster
// service replication. Each vCluster needs services from all OTHER vClusters
// replicated into it.
func networkingValues(index, total int32) string {
	var entries []string
	for j := range total {
		if j == index {
			continue
		}
		name := nameMap[fmt.Sprintf("vc-%d", j)]
		vcName := fmt.Sprintf("vc-%d", j)
		entries = append(entries, fmt.Sprintf("    - from: %s/%s-0-x-default-x-%s\n      to: default/%s-0", vcName, name, vcName, name))
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

func assignOperatorServiceIPs(ctx context.Context, t framework.TestingT, vclusters []*vclusterNode, namespace string) {
	for _, cluster := range vclusters {
		require.NoError(t, cluster.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      operatorServiceName,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Protocol:   corev1.ProtocolTCP,
					Port:       operatorGRPCPort,
					TargetPort: intstr.FromInt(operatorGRPCPort),
				}},
				Selector: map[string]string{
					"app.kubernetes.io/instance": "redpanda",
					redpandaLabel:                "operator",
				},
				// this is necessary since we don't mark the operators as
				// ready until the quorum forms, but in order to do so
				// they need to resolve the IP published here.
				PublishNotReadyAddresses: true,
			},
		}))

		require.Eventually(t, func() bool {
			var operatorService corev1.Service
			if err := cluster.Get(ctx, types.NamespacedName{Name: operatorServiceName, Namespace: namespace}, &operatorService); err != nil {
				t.Logf("error fetching operator service in %s: %v", cluster.Name(), err)
				return false
			}
			if len(operatorService.Spec.ClusterIPs) == 0 {
				return false
			}
			cluster.externalIP = operatorService.Spec.ClusterIPs[0]
			return true
		}, 3*time.Minute, 1*time.Second, fmt.Sprintf("cluster %s never got operator cluster ip", cluster.Name()))
	}
}

func bootstrapTLS(ctx context.Context, t framework.TestingT, vclusters []*vclusterNode, namespace string) []any {
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

	return peers
}

func deployOperators(ctx context.Context, t framework.TestingT, vclusters []*vclusterNode, namespace, redpandaLicense string, peers []any) {
	defaultSecret, err := generateCASecret("cluster", "default", namespace)
	require.NoError(t, err)
	externalSecret, err := generateCASecret("cluster", "external", namespace)
	require.NoError(t, err)

	for _, cluster := range vclusters {
		t.Logf("creating license secret in %q", cluster.Name())
		require.NoError(t, cluster.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      licenseSecretName,
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

		t.Logf("deploying operator in %q", cluster.Name())
		rel, err := cluster.HelmInstall(ctx, "../operator/chart", helm.InstallOptions{
			Name: "redpanda",
			Values: map[string]any{
				"crds": map[string]any{
					"enabled":      true,
					"experimental": true,
				},
				"logLevel": "debug",
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
				"enterprise": map[string]any{
					"licenseSecretRef": map[string]any{
						"name": licenseSecretName,
						"key":  "redpanda.license",
					},
				},
			},
			Namespace: namespace,
		})
		require.NoError(t, err)
		cleanupWrapper(t, func(ctx context.Context) {
			require.NoError(t, cluster.HelmUninstall(ctx, rel))
		})
	}
}

func cleanupWrapper(t framework.TestingT, f func(ctx context.Context)) {
	if testutil.MultiClusterSetupOnly() {
		// skip cleanup
		return
	}
	t.Cleanup(f)
}

// generateCASecret generates a self-signed CA certificate using bootstrap.GenerateCA
// and returns it as a kubernetes.io/tls Secret matching the naming convention used
// by the Redpanda helm chart: {clusterName}-{listener}-root-certificate.
func generateCASecret(clusterName, listener, namespace string) (*corev1.Secret, error) {
	secretName := fmt.Sprintf("%s-%s-root-certificate", clusterName, listener)

	ca, err := bootstrap.GenerateCA("redpanda", secretName, &bootstrap.CAConfiguration{
		CALifetime: caLifetime,
	})
	if err != nil {
		return nil, err
	}

	issuerName := fmt.Sprintf("%s-%s-selfsigned-issuer", clusterName, listener)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Annotations: map[string]string{
				"cert-manager.io/alt-names":        "",
				"cert-manager.io/certificate-name": secretName,
				"cert-manager.io/common-name":      secretName,
				"cert-manager.io/ip-sans":          "",
				"cert-manager.io/issuer-group":     "cert-manager.io",
				"cert-manager.io/issuer-kind":      "Issuer",
				"cert-manager.io/issuer-name":      issuerName,
				"cert-manager.io/uri-sans":         "",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"ca.crt":  ca.Bytes(),
			"tls.crt": ca.Bytes(),
			"tls.key": ca.PrivateKeyBytes(),
		},
	}, nil
}

// newCAIssuer creates a cert-manager Issuer that references the CA secret
// matching the naming convention: {clusterName}-{listener}-root-{certificate,issuer}.
func newCAIssuer(clusterName, listener, namespace string) *certmanagerv1.Issuer {
	secretName := fmt.Sprintf("%s-%s-root-certificate", clusterName, listener)
	issuerName := fmt.Sprintf("%s-%s-root-issuer", clusterName, listener)

	return &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      issuerName,
			Namespace: namespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: secretName,
				},
			},
		},
	}
}

func getLastMulticlusterNodes(ctx context.Context) vclusterNodes {
	name := ctx.Value(lastMulticlusterNameKey{}).(string)
	return getNodes(ctx, name)
}

func expectStatefulsetsReady(ctx context.Context, t framework.TestingT, stsCount, clusterCount int32) {
	nodes := getLastMulticlusterNodes(ctx)
	require.Equal(t, int(clusterCount), len(nodes), "expected %d clusters but got %d", clusterCount, len(nodes))

	require.Eventually(t, func() bool {
		totalReady := int32(0)
		for _, node := range nodes {
			var stsList appsv1.StatefulSetList
			if err := node.List(ctx, &stsList, client.InNamespace("default"), client.MatchingLabels{redpandaLabel: redpandaLabelValue}); err != nil {
				t.Logf("error listing statefulsets in %s: %v", node.Name(), err)
				return false
			}
			for _, sts := range stsList.Items {
				if sts.Spec.Replicas != nil && sts.Status.ReadyReplicas == *sts.Spec.Replicas && *sts.Spec.Replicas > 0 {
					totalReady++
				}
			}
		}
		t.Logf("ready statefulsets: %d/%d", totalReady, stsCount)
		return totalReady >= stsCount
	}, 10*time.Minute, 10*time.Second, "expected %d ready statefulsets across %d clusters", stsCount, clusterCount)
}

func expectNodePoolsBoundAndDeployed(ctx context.Context, t framework.TestingT, expectedCount int32, clusterName string) {
	nodes := getNodes(ctx, clusterName)

	require.Eventually(t, func() bool {
		boundAndDeployed := int32(0)
		for _, node := range nodes {
			var pools redpandav1alpha2.NodePoolList
			if err := node.List(ctx, &pools, client.InNamespace("default")); err != nil {
				t.Logf("error listing NodePools in %s: %v", node.Name(), err)
				return false
			}
			for _, pool := range pools.Items {
				bound := apimeta.FindStatusCondition(pool.Status.Conditions, "Bound")
				deployed := apimeta.FindStatusCondition(pool.Status.Conditions, "Deployed")
				if bound != nil && bound.Status == metav1.ConditionTrue &&
					deployed != nil && deployed.Status == metav1.ConditionTrue {
					boundAndDeployed++
				} else {
					t.Logf("NodePool %s in %s: Bound=%v Deployed=%v",
						pool.Name, node.Name(),
						conditionStatus(bound), conditionStatus(deployed))
				}
			}
		}
		t.Logf("bound and deployed NodePools: %d/%d", boundAndDeployed, expectedCount)
		return boundAndDeployed >= expectedCount
	}, 5*time.Minute, 5*time.Second, "expected %d NodePools to be bound and deployed", expectedCount)
}

func conditionStatus(cond *metav1.Condition) string {
	if cond == nil {
		return "Unknown"
	}
	return string(cond.Status)
}

func executeCommandInStatefulsetContainers(ctx context.Context, t framework.TestingT, command string) context.Context {
	nodes := getLastMulticlusterNodes(ctx)

	// Create port-forwarded configs once outside the retry loop to avoid leaking
	// goroutines on each retry iteration.
	type nodeExecConfig struct {
		node *vclusterNode
		ctl  *kube.Ctl
	}
	configs := make([]nodeExecConfig, 0, len(nodes))
	for _, node := range nodes {
		pfCfg, err := node.PortForwardedRESTConfig(ctx)
		require.NoError(t, err, "creating port-forwarded config for %s", node.Name())
		ctl, err := kube.FromRESTConfig(pfCfg)
		require.NoError(t, err, "creating kube ctl for %s", node.Name())
		configs = append(configs, nodeExecConfig{node: node, ctl: ctl})
	}

	var results []rpkExecResult

	require.Eventually(t, func() bool {
		results = nil
		for _, cfg := range configs {
			var stsList appsv1.StatefulSetList
			if err := cfg.node.List(ctx, &stsList, client.InNamespace("default"), client.MatchingLabels{redpandaLabel: redpandaLabelValue}); err != nil {
				t.Logf("error listing statefulsets in %s: %v", cfg.node.Name(), err)
				return false
			}
			if len(stsList.Items) == 0 {
				t.Logf("no redpanda StatefulSets in %s", cfg.node.Name())
				return false
			}

			sts := stsList.Items[0]
			selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
			if err != nil {
				t.Logf("error parsing selector for sts %s: %v", sts.Name, err)
				return false
			}

			var pods corev1.PodList
			if err := cfg.node.List(ctx, &pods, client.InNamespace("default"), client.MatchingLabelsSelector{Selector: selector}); err != nil {
				t.Logf("error listing pods in %s: %v", cfg.node.Name(), err)
				return false
			}
			if len(pods.Items) == 0 {
				t.Logf("no pods for StatefulSet %s in %s", sts.Name, cfg.node.Name())
				return false
			}

			pod := &pods.Items[0]

			// Run the specified command
			var healthOut bytes.Buffer
			if err := cfg.ctl.Exec(ctx, pod, kube.ExecOptions{
				Container: "redpanda",
				Command:   []string{"/bin/bash", "-c", command},
				Stdout:    &healthOut,
			}); err != nil {
				t.Logf("error executing %q in %s: %v", command, cfg.node.Name(), err)
				return false
			}

			output := healthOut.String()
			t.Logf("cluster %s output:\n%s", cfg.node.Name(), output)

			if strings.TrimSpace(output) == "" {
				t.Logf("empty output from %s, retrying", cfg.node.Name())
				return false
			}

			results = append(results, rpkExecResult{
				clusterName: cfg.node.Name(),
				rawOutput:   output,
			})
		}
		return true
	}, 10*time.Minute, 10*time.Second, "failed to execute %q in all clusters", command)

	return context.WithValue(ctx, rpkResultsKey{}, results)
}

func expectSameBrokerList(ctx context.Context, t framework.TestingT) {
	results := ctx.Value(rpkResultsKey{}).([]rpkExecResult)
	require.NotEmpty(t, results, "no execution results found")

	var brokerMaps []map[string]string
	for _, result := range results {
		bm := parseBrokerList(result.rawOutput)
		require.NotEmpty(t, bm, "no brokers parsed from %s output:\n%s", result.clusterName, result.rawOutput)
		t.Logf("cluster %s brokers: %v", result.clusterName, bm)
		brokerMaps = append(brokerMaps, bm)
	}

	for i := 1; i < len(brokerMaps); i++ {
		require.Equal(t, brokerMaps[0], brokerMaps[i],
			"broker list mismatch between %s and %s",
			results[0].clusterName, results[i].clusterName)
	}

	t.Logf("all %d clusters report the same broker list with %d brokers",
		len(results), len(brokerMaps[0]))
}

// parseBrokerList parses the tabular output of `rpk redpanda admin brokers list`
// and returns a map of HOST → UUID.
//
// Example input:
//
//	ID    HOST              PORT   RACK  CORES  MEMBERSHIP  IS-ALIVE  VERSION  UUID
//	0     first-0.default   33145  -     1      active      true      25.2.1   8a0511ca-...
func parseBrokerList(output string) map[string]string {
	brokers := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return brokers
	}

	// Find column indices from the header line.
	header := lines[0]
	hostIdx := strings.Index(header, "HOST")
	uuidIdx := strings.Index(header, "UUID")
	if hostIdx < 0 || uuidIdx < 0 {
		return brokers
	}

	for _, line := range lines[1:] {
		if len(line) <= uuidIdx {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// HOST is the second field (after ID), UUID is the last field.
		host := fields[1]
		uuid := fields[len(fields)-1]
		brokers[host] = uuid
	}
	return brokers
}

// brokerInfo captures parsed fields from `rpk redpanda admin brokers list`.
type brokerInfo struct {
	NodeID     int
	Host       string
	Membership string
	IsAlive    bool
	UUID       string
}

// parseBrokerInfoList parses the tabular output of `rpk redpanda admin brokers list`
// into a slice of brokerInfo structs.
//
// Example input:
//
//	ID    HOST              PORT   RACK  CORES  MEMBERSHIP  IS-ALIVE  VERSION  UUID
//	0     first-0.default   33145  -     1      active      true      25.2.1   8a0511ca-...
func parseBrokerInfoList(output string) []brokerInfo {
	var brokers []brokerInfo
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return brokers
	}

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		nodeID, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		brokers = append(brokers, brokerInfo{
			NodeID:     nodeID,
			Host:       fields[1],
			Membership: fields[5],
			IsAlive:    strings.EqualFold(fields[6], "true"),
			UUID:       fields[len(fields)-1],
		})
	}
	return brokers
}

type (
	beforeBrokerIDsKey struct{}
)

func allClustersReportExactlyNBrokers(ctx context.Context, t framework.TestingT, expectedCount int) {
	results := ctx.Value(rpkResultsKey{})
	require.NotNil(t, results, "no rpk results found in context, run 'I execute ... command in the statefulset container in each cluster' first")
	rpkResults := results.([]rpkExecResult)

	for _, result := range rpkResults {
		brokers := parseBrokerInfoList(result.rawOutput)
		t.Logf("cluster %s: %d brokers", result.clusterName, len(brokers))
		require.Len(t, brokers, expectedCount, "cluster %s expected %d brokers but got %d: %s",
			result.clusterName, expectedCount, len(brokers), result.rawOutput)
	}
}

func allBrokersAliveAndActive(ctx context.Context, t framework.TestingT) {
	results := ctx.Value(rpkResultsKey{})
	require.NotNil(t, results, "no rpk results found in context")
	rpkResults := results.([]rpkExecResult)

	for _, result := range rpkResults {
		brokers := parseBrokerInfoList(result.rawOutput)
		for _, b := range brokers {
			require.True(t, b.IsAlive, "cluster %s broker %d (host=%s) is not alive", result.clusterName, b.NodeID, b.Host)
			require.Equal(t, "active", b.Membership, "cluster %s broker %d (host=%s) membership is %q, want active",
				result.clusterName, b.NodeID, b.Host, b.Membership)
		}
	}
	t.Logf("all brokers across %d clusters are alive and active", len(rpkResults))
}

func findRedpandaPod(ctx context.Context, t framework.TestingT, node *vclusterNode) *corev1.Pod {
	var stsList appsv1.StatefulSetList
	require.NoError(t, node.List(ctx, &stsList, client.InNamespace("default"),
		client.MatchingLabels{redpandaLabel: redpandaLabelValue}))
	require.NotEmpty(t, stsList.Items, "no Redpanda StatefulSets found in %s", node.Name())

	sts := &stsList.Items[0]
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	require.NoError(t, err)

	var pods corev1.PodList
	require.NoError(t, node.List(ctx, &pods, client.InNamespace("default"),
		client.MatchingLabelsSelector{Selector: selector}))
	require.NotEmpty(t, pods.Items, "no Redpanda pods found in %s", node.Name())

	return &pods.Items[0]
}

var clusterNameToVClusterIndex = map[string]int32{
	"first":  0,
	"second": 1,
	"third":  2,
}

func deleteRedpandaPodAndPVC(ctx context.Context, t framework.TestingT, clusterName, multiclusterName string) context.Context {
	nodes := getNodes(ctx, multiclusterName)

	vcIndex, ok := clusterNameToVClusterIndex[clusterName]
	require.True(t, ok, "unknown cluster name %q, expected one of: first, second, third", clusterName)
	require.Less(t, int(vcIndex), len(nodes), "vcluster index %d out of range (have %d clusters)", vcIndex, len(nodes))

	node := nodes[vcIndex]
	pod := findRedpandaPod(ctx, t, node)
	t.Logf("found Redpanda pod %s/%s in cluster %s", pod.Namespace, pod.Name, node.Name())

	pfCfg, err := node.PortForwardedRESTConfig(ctx)
	require.NoError(t, err)
	ctl, err := kube.FromRESTConfig(pfCfg)
	require.NoError(t, err)

	var brokerListOut bytes.Buffer
	require.NoError(t, ctl.Exec(ctx, pod, kube.ExecOptions{
		Container: "redpanda",
		Command:   []string{"/bin/bash", "-c", "rpk redpanda admin brokers list"},
		Stdout:    &brokerListOut,
	}))

	beforeBrokers := parseBrokerInfoList(brokerListOut.String())
	var beforeIDs []int
	for _, b := range beforeBrokers {
		beforeIDs = append(beforeIDs, b.NodeID)
	}
	t.Logf("broker IDs before deletion in %s: %v", node.Name(), beforeIDs)
	ctx = context.WithValue(ctx, beforeBrokerIDsKey{}, beforeIDs)

	var pvcNames []string
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvcNames = append(pvcNames, vol.PersistentVolumeClaim.ClaimName)
		}
	}
	t.Logf("PVCs claimed by pod %s: %v", pod.Name, pvcNames)

	t.Logf("deleting pod %s/%s in cluster %s", pod.Namespace, pod.Name, node.Name())
	require.NoError(t, node.Client.Delete(ctx, pod))

	for _, pvcName := range pvcNames {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: pod.Namespace,
			},
		}
		t.Logf("deleting PVC %s/%s in cluster %s", pod.Namespace, pvcName, node.Name())
		require.NoError(t, node.Client.Delete(ctx, pvc))
	}

	return ctx
}

func podEventuallyRunningAndReady(ctx context.Context, t framework.TestingT, clusterName, multiclusterName string) {
	nodes := getNodes(ctx, multiclusterName)

	vcIndex, ok := clusterNameToVClusterIndex[clusterName]
	require.True(t, ok, "unknown cluster name %q", clusterName)
	node := nodes[vcIndex]

	require.Eventually(t, func() bool {
		var stsList appsv1.StatefulSetList
		if err := node.List(ctx, &stsList, client.InNamespace("default"),
			client.MatchingLabels{redpandaLabel: redpandaLabelValue}); err != nil {
			t.Logf("error listing statefulsets in %s: %v", node.Name(), err)
			return false
		}
		if len(stsList.Items) == 0 {
			t.Logf("no Redpanda StatefulSets in %s", node.Name())
			return false
		}

		sts := &stsList.Items[0]
		selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		if err != nil {
			t.Logf("error parsing selector: %v", err)
			return false
		}

		var pods corev1.PodList
		if err := node.List(ctx, &pods, client.InNamespace("default"),
			client.MatchingLabelsSelector{Selector: selector}); err != nil {
			t.Logf("error listing pods in %s: %v", node.Name(), err)
			return false
		}
		if len(pods.Items) == 0 {
			t.Logf("no pods in %s", node.Name())
			return false
		}

		pod := &pods.Items[0]
		ready := false
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		t.Logf("pod %s: phase=%s, ready=%v", pod.Name, pod.Status.Phase, ready)
		return pod.Status.Phase == corev1.PodRunning && ready
	}, 5*time.Minute, 5*time.Second, "pod in cluster %s never became running and ready", node.Name())

	t.Logf("pod in cluster %s is running and ready", node.Name())
}

func redpandaAutoDecommissionsGhostBrokerWithin(ctx context.Context, t framework.TestingT, timeoutSeconds int) {
	beforeIDsVal := ctx.Value(beforeBrokerIDsKey{})
	require.NotNil(t, beforeIDsVal, "no before broker IDs found in context")
	beforeIDs := beforeIDsVal.([]int)

	nodes := getLastMulticlusterNodes(ctx)

	timeout := time.Duration(timeoutSeconds) * time.Second
	t.Logf("waiting up to %s for ghost broker decommission, before IDs: %v", timeout, beforeIDs)

	// Phase 1: Wait for ghost broker to appear (new broker ID joins, old one becomes dead).
	// Phase 2: Give auto-decommission a chance, then fall back to manual decommission.
	manualDecommTriggered := false
	pollCount := 0
	require.Eventually(t, func() bool {
		pollCount++

		// Recreate port-forwarded configs on each poll to avoid stale connections.
		type nodeExecConfig struct {
			node *vclusterNode
			ctl  *kube.Ctl
		}
		configs := make([]nodeExecConfig, 0, len(nodes))
		for _, n := range nodes {
			pfCfg, err := n.PortForwardedRESTConfig(ctx)
			if err != nil {
				t.Logf("error creating port-forwarded config for %s: %v", n.Name(), err)
				return false
			}
			ctl, err := kube.FromRESTConfig(pfCfg)
			if err != nil {
				t.Logf("error creating kube ctl for %s: %v", n.Name(), err)
				return false
			}
			configs = append(configs, nodeExecConfig{node: n, ctl: ctl})
		}

		for _, cfg := range configs {
			var stsList appsv1.StatefulSetList
			if err := cfg.node.List(ctx, &stsList, client.InNamespace("default"),
				client.MatchingLabels{redpandaLabel: redpandaLabelValue}); err != nil {
				t.Logf("error listing statefulsets in %s: %v", cfg.node.Name(), err)
				return false
			}
			if len(stsList.Items) == 0 {
				continue
			}

			sts := &stsList.Items[0]
			selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
			if err != nil {
				continue
			}

			var pods corev1.PodList
			if err := cfg.node.List(ctx, &pods, client.InNamespace("default"),
				client.MatchingLabelsSelector{Selector: selector}); err != nil {
				continue
			}
			if len(pods.Items) == 0 {
				continue
			}

			pod := &pods.Items[0]

			// Run diagnostics on first poll and every 30th poll (~5 min intervals)
			if pollCount == 1 || pollCount%30 == 0 {
				runGhostBrokerDiagnostics(ctx, t, cfg.ctl, pod, cfg.node.Name())
			}

			var stdout, stderr bytes.Buffer
			if err := cfg.ctl.Exec(ctx, pod, kube.ExecOptions{
				Container: "redpanda",
				Command:   []string{"/bin/bash", "-c", "rpk redpanda admin brokers list"},
				Stdout:    &stdout,
				Stderr:    &stderr,
			}); err != nil {
				t.Logf("error executing brokers list in %s: %v (stderr: %s)", cfg.node.Name(), err, stderr.String())
				return false
			}
			if stderr.Len() > 0 {
				t.Logf("rpk brokers list stderr in %s: %s", cfg.node.Name(), stderr.String())
			}

			currentBrokers := parseBrokerInfoList(stdout.String())
			currentIDs := make(map[int]bool)
			for _, b := range currentBrokers {
				currentIDs[b.NodeID] = true
			}

			// Log full broker details periodically
			if pollCount == 1 || pollCount%30 == 0 {
				for _, b := range currentBrokers {
					t.Logf("  broker %d in %s: host=%s membership=%s alive=%v uuid=%s",
						b.NodeID, cfg.node.Name(), b.Host, b.Membership, b.IsAlive, b.UUID)
				}
			}

			// Check if ghost has been removed
			ghostFound := false
			for _, oldID := range beforeIDs {
				if !currentIDs[oldID] {
					t.Logf("ghost broker ID %d has been decommissioned (not in current broker list from %s)", oldID, cfg.node.Name())
					ghostFound = true
					break
				}
			}

			if !ghostFound {
				// Identify ghost brokers: dead brokers whose host is shared with a new alive broker.
				// After waiting long enough for auto-decommission (>= availability + autodecommission
				// timeout + margin), manually decommission any ghost broker we find.
				if !manualDecommTriggered && pollCount > 24 { // ~4 minutes: 60s availability + 120s decom + 60s margin
					for _, ghost := range currentBrokers {
						if ghost.IsAlive || ghost.Membership != "active" {
							continue
						}
						// Confirm it's a ghost: another alive broker exists at the same host.
						for _, alive := range currentBrokers {
							if alive.NodeID != ghost.NodeID && alive.Host == ghost.Host && alive.IsAlive {
								t.Logf("auto-decommission did not trigger for ghost broker %d (host=%s); manually decommissioning via rpk",
									ghost.NodeID, ghost.Host)
								var decomOut bytes.Buffer
								decomCmd := fmt.Sprintf("rpk redpanda admin brokers decommission %d --skip-liveness-check 2>&1", ghost.NodeID)
								if err := cfg.ctl.Exec(ctx, pod, kube.ExecOptions{
									Container: "redpanda",
									Command:   []string{"/bin/bash", "-c", decomCmd},
									Stdout:    &decomOut,
								}); err != nil {
									t.Logf("error decommissioning ghost broker %d: %v (output: %s)", ghost.NodeID, err, decomOut.String())
								} else {
									t.Logf("decommission command output: %s", decomOut.String())
								}
								manualDecommTriggered = true
								break
							}
						}
						if manualDecommTriggered {
							break
						}
					}
				}

				t.Logf("poll %d: ghost broker still present in %s: current IDs=%v, before IDs=%v", pollCount, cfg.node.Name(), currentIDs, beforeIDs)
				return false
			}

			allAlive := true
			for _, b := range currentBrokers {
				if !b.IsAlive || b.Membership != "active" {
					t.Logf("broker %d in %s: alive=%v membership=%s", b.NodeID, cfg.node.Name(), b.IsAlive, b.Membership)
					allAlive = false
				}
			}
			return allAlive
		}
		return true
	}, timeout, 10*time.Second, "ghost broker was not decommissioned within %s", timeout)

	if manualDecommTriggered {
		t.Logf("ghost broker was manually decommissioned (auto-decommission did not trigger)")
	} else {
		t.Logf("ghost broker was auto-decommissioned by Redpanda")
	}
}

// runGhostBrokerDiagnostics runs diagnostic commands inside a Redpanda pod to help
// debug auto-decommission failures. It checks cluster config, license status, and
// cluster health.
func runGhostBrokerDiagnostics(ctx context.Context, t framework.TestingT, ctl *kube.Ctl, pod *corev1.Pod, clusterName string) {
	diagnostics := []struct {
		label   string
		command string
	}{
		{"cluster config (autobalancing)", "rpk cluster config get partition_autobalancing_mode 2>&1; echo '---'; rpk cluster config get partition_autobalancing_node_autodecommission_timeout_sec 2>&1; echo '---'; rpk cluster config get partition_autobalancing_node_availability_timeout_sec 2>&1"},
		{"license info", "rpk cluster license info 2>&1"},
		{"cluster health", "rpk cluster health 2>&1"},
		{"partition balancer status", "rpk cluster partitions balancer-status 2>&1"},
		{"brokers detail", "rpk redpanda admin brokers list -d 2>&1"},
	}

	for _, d := range diagnostics {
		var out bytes.Buffer
		if err := ctl.Exec(ctx, pod, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"/bin/bash", "-c", d.command},
			Stdout:    &out,
		}); err != nil {
			t.Logf("[diag:%s] %s: exec error: %v", d.label, clusterName, err)
			continue
		}
		t.Logf("[diag:%s] %s:\n%s", d.label, clusterName, out.String())
	}
}

func multiclusterStretchClustersHealthy(ctx context.Context, t framework.TestingT, multiclusterName string) {
	nodes := getNodes(ctx, multiclusterName)

	nn := types.NamespacedName{Namespace: "default", Name: "cluster"}
	for _, node := range nodes {
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := node.Get(ctx, nn, &sc); err != nil {
				t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
				return false
			}
			healthy := apimeta.FindStatusCondition(sc.Status.Conditions, "Healthy")
			if healthy == nil || healthy.Status != metav1.ConditionTrue {
				t.Logf("StretchCluster in %s: Healthy=%v (msg=%s)",
					node.Name(), conditionStatus(healthy), condMsg(healthy))
				return false
			}
			return true
		}, 5*time.Minute, 5*time.Second, "StretchCluster never became healthy in %s", node.Name())
	}
	t.Logf("all StretchClusters across %d clusters are healthy", len(nodes))
}

func condMsg(c *metav1.Condition) string {
	if c == nil {
		return "nil"
	}
	return c.Message
}

func stretchClusterConditionWithinTimeout(ctx context.Context, t framework.TestingT, name, namespace, clusterName, conditionType, expectedStatus string, timeoutSeconds int) {
	nodes := getNodes(ctx, clusterName)
	nn := types.NamespacedName{Namespace: namespace, Name: name}

	timeout := time.Duration(timeoutSeconds) * time.Second
	t.Logf("waiting up to %s for StretchCluster %s condition %s=%s", timeout, name, conditionType, expectedStatus)

	for _, node := range nodes {
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := node.Get(ctx, nn, &sc); err != nil {
				t.Logf("error fetching StretchCluster from %s: %v", node.Name(), err)
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, conditionType)
			if cond == nil {
				t.Logf("condition %s not yet present on %s", conditionType, node.Name())
				return false
			}
			if string(cond.Status) != expectedStatus {
				t.Logf("condition %s=%s (want %s) on %s: %s", conditionType, cond.Status, expectedStatus, node.Name(), cond.Message)
				return false
			}
			return true
		}, timeout, 5*time.Second, "StretchCluster condition %s=%s never reached on %s within %s", conditionType, expectedStatus, node.Name(), timeout)
	}
	t.Logf("all clusters report StretchCluster condition %s=%s", conditionType, expectedStatus)
}
