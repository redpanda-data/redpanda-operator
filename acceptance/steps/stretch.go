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
	"io"
	"os"
	"slices"
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
	rbacv1 "k8s.io/api/rbac/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	apirand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
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

		// Dump StretchCluster objects.
		var scList redpandav1alpha2.StretchClusterList
		if err := node.List(diagCtx, &scList); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list StretchClusters: %v", err)
		} else {
			for _, sc := range scList.Items {
				t.Logf("[multicluster-diagnostics] StretchCluster %s/%s: finalizers=%v, conditions=%d, generation=%d",
					sc.Namespace, sc.Name, sc.Finalizers, len(sc.Status.Conditions), sc.Generation)
				for _, cond := range sc.Status.Conditions {
					t.Logf("[multicluster-diagnostics]   condition %s=%s reason=%s: %s", cond.Type, cond.Status, cond.Reason, cond.Message)
				}
			}
		}

		// Dump NodePools.
		var npList redpandav1alpha2.NodePoolList
		if err := node.List(diagCtx, &npList); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list NodePools: %v", err)
		} else {
			for _, np := range npList.Items {
				t.Logf("[multicluster-diagnostics] NodePool %s/%s: replicas=%d, conditions=%d",
					np.Namespace, np.Name, ptr.Deref(np.Spec.Replicas, 0), len(np.Status.Conditions))
			}
		}

		// Dump ClusterRoles.
		var crList rbacv1.ClusterRoleList
		if err := node.List(diagCtx, &crList); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list ClusterRoles: %v", err)
		} else {
			for _, cr := range crList.Items {
				t.Logf("[multicluster-diagnostics] ClusterRole %s: rules=%d", cr.Name, len(cr.Rules))
				for _, rule := range cr.Rules {
					t.Logf("[multicluster-diagnostics]   groups=%v resources=%v verbs=%v", rule.APIGroups, rule.Resources, rule.Verbs)
				}
			}
		}

		// Dump ClusterRoleBindings.
		var crbList rbacv1.ClusterRoleBindingList
		if err := node.List(diagCtx, &crbList); err != nil {
			t.Logf("[multicluster-diagnostics] failed to list ClusterRoleBindings: %v", err)
		} else {
			for _, crb := range crbList.Items {
				subjects := make([]string, len(crb.Subjects))
				for i, s := range crb.Subjects {
					subjects[i] = fmt.Sprintf("%s/%s", s.Namespace, s.Name)
				}
				t.Logf("[multicluster-diagnostics] ClusterRoleBinding %s: role=%s subjects=%v", crb.Name, crb.RoleRef.Name, subjects)
			}
		}

		// Dump operator pod logs (last 100 lines).
		k8sClient, err := kubernetes.NewForConfig(node.RESTConfig())
		if err != nil {
			t.Logf("[multicluster-diagnostics] failed to create k8s client for logs: %v", err)
			continue
		}
		for _, pod := range vcPods.Items {
			if !strings.Contains(pod.Name, "operator") {
				continue
			}
			tailLines := int64(100)
			req := k8sClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: &tailLines})
			logStream, err := req.Stream(diagCtx)
			if err != nil {
				t.Logf("[multicluster-diagnostics] failed to get logs for %s: %v", pod.Name, err)
				continue
			}
			logBytes, _ := io.ReadAll(logStream)
			_ = logStream.Close()
			t.Logf("[multicluster-diagnostics] === operator logs %s (last 100 lines) ===\n%s", pod.Name, string(logBytes))
		}
	}
}

var nameMap = map[string]string{
	"vc-0": "first",
	"vc-1": "second",
	"vc-2": "third",
}

func (v vclusterNodes) ApplyAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		if node.offline {
			t.Logf("skipping offline node %q for manifest apply", node.Name())
			continue
		}
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
		fullManifest := nodepoolManifest(nameMap[node.logicalName], manifest)
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, fullManifest))
	}
}

func (v vclusterNodes) DeleteNodepools(ctx context.Context, manifest *godog.DocString) {
	t := framework.T(ctx)
	for _, node := range v {
		fullManifest := nodepoolManifest(nameMap[node.logicalName], manifest)
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
	apiServer string
	// externalIP is the ClusterIP of the operator service in this vcluster,
	// used by peer vclusters to reach this one's operator over gRPC.
	externalIP string
	// logicalName is the stable region identifier used in feature files and
	// step arguments (e.g. "vc-0", "vc-1", "vc-2"). It is independent of the
	// actual vcluster name, which includes a per-test-run unique suffix to
	// prevent namespace collisions when tests run in parallel.
	logicalName string
	// offline is set to true when the region is intentionally taken offline for
	// disaster-recovery tests. ApplyAll and similar helpers skip offline nodes.
	offline bool
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
	assignOperatorServiceIPs(ctx, t, vclusters, namespace)
	peers := bootstrapTLS(ctx, t, vclusters, namespace)
	deployOperators(ctx, t, vclusters, namespace, redpandaLicense, peers)
	// Register dumpDiagnostics AFTER deployOperators so it fires before the
	// HelmUninstall cleanup (t.Cleanup is LIFO), ensuring ClusterRoles and
	// other resources are still present when diagnostics are collected.
	t.Cleanup(func(ctx context.Context) {
		vclusterNodes(vclusters).dumpDiagnostics(ctx, t)
	})

	return stashNodes(ctx, clusterName, vclusters)
}

func createVClusters(ctx context.Context, t framework.TestingT, clusters int32) []*vclusterNode {
	t.Logf("creating %d vclusters", clusters)

	// Generate a unique per-test suffix so that vcluster host namespaces never
	// collide when tests run in parallel or back-to-back (a terminating namespace
	// from a previous run would block reuse of the same name).
	suffix := apirand.String(8)

	nodes := make([]*vclusterNode, clusters)
	var wg sync.WaitGroup

	// Register cert-manager types into the shared scheme once, before launching
	// parallel goroutines. AddToScheme mutates the scheme's internal maps and is
	// not safe to call concurrently.
	require.NoError(t, certmanagerv1.AddToScheme(t.Scheme()))

	for i := range clusters {
		wg.Add(1)
		go func(i int32) {
			defer wg.Done()

			logicalName := fmt.Sprintf("vc-%d", i)
			// Prefix with "vc-" so the name is a valid DNS-1035 label (must start
			// with a letter); apirand.String can return strings starting with digits.
			actualName := fmt.Sprintf("vc-%s-%d", suffix, i)

			vClusterValues := vcluster.DefaultValues + networkingValues(i, clusters, suffix)
			cluster, err := vcluster.New(ctx, t.RestConfig(), vcluster.WithName(actualName), vcluster.WithValues(helm.RawYAML(vClusterValues)))
			require.NoError(t, err)
			scheme := t.Scheme()
			cluster.SetScheme(scheme)

			t.Logf("finished creating vcluster %d (logical: %q, actual: %q)", i+1, logicalName, cluster.Name())

			cleanupWrapper(t, func(ctx context.Context) {
				if err := cluster.Delete(); err != nil {
					t.Logf("error deleting cluster %s: %v", cluster.Name(), err)
				}
			})
			c, err := cluster.Client(client.Options{Scheme: t.Scheme()})
			require.NoError(t, err)

			// Use the vCluster's host-namespace ClusterIP service as the API server
			// address. This service ({actualName}/{actualName}) is replicated into peer
			// vClusters via networkingValues, making it reachable from synced operator
			// pods under https://{actualName}.default. Using the vcluster-internal
			// "kubernetes" ClusterIP would be wrong — it routes to the LOCAL cluster's
			// API server from any vcluster, so taking a remote vcluster down would
			// never cause connection failures.
			nodes[i] = &vclusterNode{
				Client:      c,
				Cluster:     cluster,
				apiServer:   fmt.Sprintf("https://%s", actualName),
				logicalName: logicalName,
			}
		}(i)
	}

	wg.Wait()
	return nodes
}

// stretchClusterResourceName is the StretchCluster resource name used in all
// multicluster acceptance tests. Per-pod service names are prefixed with this
// value (e.g. "cluster-first-0").
const stretchClusterResourceName = "cluster"

// networkingValues generates the vCluster networking YAML for cross-cluster
// service replication. Each vCluster needs services from all OTHER vClusters
// replicated into it. This includes:
//   - Per-pod broker services (for Redpanda seed server resolution)
//   - The vCluster API server service (so operators can reach peer k8s API servers)
func networkingValues(index, total int32, suffix string) string {
	var entries []string
	for j := range total {
		if j == index {
			continue
		}
		name := nameMap[fmt.Sprintf("vc-%d", j)]
		vcName := fmt.Sprintf("vc-%s-%d", suffix, j)
		// Per-pod services are named "{clusterName}-{poolName}-{ordinal}" after the
		// cluster-prefixed naming convention; include the StretchCluster name prefix.
		svcName := fmt.Sprintf("%s-%s-0", stretchClusterResourceName, name)
		entries = append(entries, fmt.Sprintf("    - from: %s/%s-x-default-x-%s\n      to: default/%s", vcName, svcName, vcName, svcName))
		// Replicate the peer vCluster's API server service (named the same as its
		// release/namespace) so operators inside this vCluster can reach the peer's
		// k8s API server at https://{vcName}.default rather than the vcluster-internal
		// kubernetes ClusterIP which only routes to the local cluster's API server.
		entries = append(entries, fmt.Sprintf("    - from: %s/%s\n      to: default/%s", vcName, vcName, vcName))
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
	}
	peers := []any{}
	for _, cluster := range vclusters {
		bootstrapConfig.RemoteClusters = append(bootstrapConfig.RemoteClusters, bootstrap.RemoteConfiguration{
			KubeConfig:     cluster.RESTConfig(),
			APIServer:      cluster.APIServer(),
			ServiceAddress: cluster.ExternalIP(),
			Name:           "redpanda-operator", // Fullname for helm release "redpanda" with the operator chart
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
	// "issuer-managed" cert: user provides CA secret + Issuer, StretchCluster
	// uses IssuerRef. Used by admin, http, schemaRegistry, and rpc listeners.
	issuerManagedSecret, err := generateCASecret("cluster", "issuer-managed", namespace)
	require.NoError(t, err)

	// "user-provided" cert: user provides a pre-signed server cert secret
	// directly, StretchCluster uses SecretRef (no cert-manager involvement).
	// Used by the kafka listener.
	userProvidedCASecret, err := generateCASecret("cluster", "user-provided", namespace)
	require.NoError(t, err)
	userProvidedCA, err := bootstrap.LoadCA(
		userProvidedCASecret.Data[corev1.TLSCertKey],
		userProvidedCASecret.Data[corev1.TLSPrivateKeyKey],
		&bootstrap.CAConfiguration{CALifetime: caLifetime, CertificateLifetime: 24 * time.Hour},
	)
	require.NoError(t, err)
	// Sign a wildcard leaf cert. Include localhost (rpk connects locally)
	// and wildcard SANs for cross-pod service resolution.
	userProvidedLeafCert, err := userProvidedCA.Sign(
		"localhost",
		"*.default.svc.cluster.local",
		"*.cluster.default.svc.cluster.local",
		"*.default.svc",
		"*.default",
	)
	require.NoError(t, err)
	userProvidedServerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-user-provided-cert",
			Namespace: namespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       userProvidedLeafCert.Bytes(),
			corev1.TLSPrivateKeyKey: userProvidedLeafCert.PrivateKeyBytes(),
			"ca.crt":                userProvidedCA.Bytes(),
		},
	}

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

		// Create the CA secret + Issuer for "issuer-managed" cert (IssuerRef flow).
		t.Logf("creating TLS secrets and issuers in %q", cluster.Name())
		require.NoError(t, cluster.Create(ctx, issuerManagedSecret.DeepCopy()))

		issuerManagedIssuer := newCAIssuer("cluster", "issuer-managed", namespace)
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
			return cluster.Create(ctx, issuerManagedIssuer)
		}))

		// Create the pre-signed server cert secret for "user-provided" cert (SecretRef flow).
		// No Issuer needed — the operator uses this secret directly.
		require.NoError(t, cluster.Create(ctx, userProvidedServerSecret.DeepCopy()))

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
			// The cluster may be offline if the test took it down and then
			// failed before restoring it. Skip the uninstall in that case —
			// the vcluster itself will be deleted, taking everything with it.
			if cluster.offline {
				t.Logf("skipping helm uninstall for offline cluster %s", cluster.Name())
				return
			}
			require.NoError(t, cluster.HelmUninstall(ctx, rel))
		})
	}
}

const cleanupTimeout = 2 * time.Minute

func cleanupWrapper(t framework.TestingT, f func(ctx context.Context)) {
	if testutil.MultiClusterSetupOnly() {
		// skip cleanup
		return
	}
	t.Cleanup(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
		defer cancel()
		f(ctx)
	})
}

// generateCASecret generates a self-signed CA certificate using bootstrap.GenerateCA
// and returns it as a kubernetes.io/tls Secret matching the naming convention used
// by the Redpanda helm chart: {clusterName}-{listener}-root-certificate.
func generateCASecret(clusterName, listener, namespace string) (*corev1.Secret, error) {
	secretName := fmt.Sprintf("%s-%s-ca-provided-by-user", clusterName, listener)

	ca, err := bootstrap.GenerateCA("redpanda", secretName, &bootstrap.CAConfiguration{
		CALifetime: caLifetime,
	})
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
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
func newCAIssuer(clusterName, certName, namespace string) *certmanagerv1.Issuer {
	secretName := fmt.Sprintf("%s-%s-ca-provided-by-user", clusterName, certName)
	issuerName := fmt.Sprintf("custom-%s-issuer", certName)

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
				t.Logf("error executing %q in %s: %v \n output: %s", command, cfg.node.Name(), err, healthOut.String())
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
//	0     cluster-first-0.default   33145  -     1      active      true      25.2.1   8a0511ca-...
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
