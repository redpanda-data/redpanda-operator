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
	"os"
	"slices"
	"strings"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type vclusterNodes []*vclusterNode

func (v vclusterNodes) ApplyAll(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)
	for _, node := range v {
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, manifest))
	}
}

func (v vclusterNodes) ApplyAllWithModifiedName(ctx context.Context, manifest *godog.DocString) {
	t := framework.T(ctx)
	nameMap := map[string]string{
		"vc-0": "first",
		"vc-1": "second",
		"vc-2": "third",
	}

	for _, node := range v {
		nodepoolName := nameMap[node.Name()]
		fullManifest := fmt.Sprintf(`
apiVersion: cluster.redpanda.com/v1alpha2
kind: NodePool
metadata:
  name: %s
  namespace: default
`, nodepoolName) + manifest.Content
		t.Logf("applying manifest to %q", node.Name())
		require.NoError(t, node.KubectlApply(ctx, []byte(fullManifest)))
	}
}

func (v vclusterNodes) ApplyInFirst(ctx context.Context, manifest []byte) {
	t := framework.T(ctx)

	for _, node := range v {
		if node.Name() != "vc-0" {
			continue
		}
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
		require.Eventually(t, func() bool {
			obj, err := t.Scheme().New(*gvk)
			require.NoError(t, err)

			o := obj.(client.Object)
			t.Logf("fetching (%T) object: %q", o, namespacedName.String())
			if err := node.Get(ctx, namespacedName, o); err != nil {
				t.Logf("error fetching %q: %v", namespacedName.String(), err)
				return false
			}
			return fn(o)
		}, 1*time.Minute, 1*time.Second, "condition not met")
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
	//nodes.ApplyInFirst(ctx, []byte(manifest.Content))
	nodes.ApplyAll(ctx, []byte(manifest.Content))
	cleanupWrapper(t, func(ctx context.Context) {
		nodes.DeleteAll(ctx, []byte(manifest.Content))
	})
}

func applyNodePoolWithStretchCluster(ctx context.Context, t framework.TestingT, clusterName string, manifest *godog.DocString) {
	nodes := getNodes(ctx, clusterName)
	nodes.ApplyAllWithModifiedName(ctx, manifest)
	cleanupWrapper(t, func(ctx context.Context) {
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
	redpandaLicense := os.Getenv("REDPANDA_LICENSE_KEY")
	require.NotEmpty(t, redpandaLicense, "REDPANDA_LICENSE_KEY env var must be set")
	// create license secret in the k3s cluster
	vclusters := []*vclusterNode{}
	t.Logf("creating %d vclusters", clusters)
	for i := range clusters {
		t.Logf("creating vcluster %d", i+1)
		vClusterValues := vcluster.DefaultValues
		switch i {
		case 0:
			vClusterValues += `
networking:
  replicateServices:
    fromHost:
    - from: vc-1/second-0-x-default-x-vc-1
      to: default/second-0
    - from: vc-2/third-0-x-default-x-vc-2
      to: default/third-0
`
		case 1:
			vClusterValues += `
networking:
  replicateServices:
    fromHost:
    - from: vc-0/first-0-x-default-x-vc-0
      to: default/first-0
    - from: vc-2/third-0-x-default-x-vc-2
      to: default/third-0
`
		case 2:
			vClusterValues += `
networking:
  replicateServices:
    fromHost:
    - from: vc-0/first-0-x-default-x-vc-0
      to: default/first-0
    - from: vc-1/second-0-x-default-x-vc-1
      to: default/second-0
`
		}
		cluster, err := vcluster.New(ctx, t.RestConfig(), vcluster.WithName(fmt.Sprintf("vc-%d", i)), vcluster.WithValues(helm.RawYAML(vClusterValues)))
		require.NoError(t, err)
		scheme := t.Scheme()
		require.NoError(t, certmanagerv1.AddToScheme(scheme))
		cluster.SetScheme(scheme)

		t.Logf("finished creating vcluster %d (name: %q)", i+1, cluster.Name())

		cleanupWrapper(t, func(ctx context.Context) {
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
			require.NoError(t, cluster.Get(ctx, types.NamespacedName{Name: "multicluster-operator", Namespace: metav1.NamespaceDefault}, &operatorService))
			if len(operatorService.Spec.ClusterIPs) == 0 {
				return false
			}
			cluster.externalIP = operatorService.Spec.ClusterIPs[0]
			return true
		}, 3*time.Minute, 1*time.Second, fmt.Sprintf("cluster %s never got operator cluster ip", cluster.Name()))
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

		t.Logf("creating cluster-external secret for redpanda RPC TLS in %q", cluster.Name())
		rootCertSecretData := `
apiVersion: v1
data:
  ca.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJsVENDQVR5Z0F3SUJBZ0lRYjNRRUM0Y0d0bFA4R3l3WnQ3b3lFakFLQmdncWhrak9QUVFEQWpBck1Ta3cKSndZRFZRUURFeUJqYkhWemRHVnlMV1JsWm1GMWJIUXRjbTl2ZEMxalpYSjBhV1pwWTJGMFpUQWVGdzB5TmpBegpNVGd4TWpFMU16RmFGdzB6TVRBek1UY3hNakUxTXpGYU1Dc3hLVEFuQmdOVkJBTVRJR05zZFhOMFpYSXRaR1ZtCllYVnNkQzF5YjI5MExXTmxjblJwWm1sallYUmxNRmt3RXdZSEtvWkl6ajBDQVFZSUtvWkl6ajBEQVFjRFFnQUUKNDhic3FIVnhlSFdjSzBIT0NRTXY0dk16OXhEQTFIY0dWVnBwTXNBL3ZRSSt0Mnp2QlJOY0d3d2F0RGVJT3VidApSQlFybzYrK2ZhcUZVR1lVVng5Y0thTkNNRUF3RGdZRFZSMFBBUUgvQkFRREFnS2tNQThHQTFVZEV3RUIvd1FGCk1BTUJBZjh3SFFZRFZSME9CQllFRkVjd215Tk5nWGRCSS83NEhyS1JSWDQ1TzNpb01Bb0dDQ3FHU000OUJBTUMKQTBjQU1FUUNJQXI3UHFRK2NVLzlmWG1hSW4wWnJWWjZzcjR5MVo1MkZBeHc2VWxod0FkUUFpQjJCT0szNlozcgpQK1IrWkN5ZU9RQlhqQkpWd3RiUkxuRSs0OUxKcWdYelJnPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
  tls.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJsVENDQVR5Z0F3SUJBZ0lRYjNRRUM0Y0d0bFA4R3l3WnQ3b3lFakFLQmdncWhrak9QUVFEQWpBck1Ta3cKSndZRFZRUURFeUJqYkhWemRHVnlMV1JsWm1GMWJIUXRjbTl2ZEMxalpYSjBhV1pwWTJGMFpUQWVGdzB5TmpBegpNVGd4TWpFMU16RmFGdzB6TVRBek1UY3hNakUxTXpGYU1Dc3hLVEFuQmdOVkJBTVRJR05zZFhOMFpYSXRaR1ZtCllYVnNkQzF5YjI5MExXTmxjblJwWm1sallYUmxNRmt3RXdZSEtvWkl6ajBDQVFZSUtvWkl6ajBEQVFjRFFnQUUKNDhic3FIVnhlSFdjSzBIT0NRTXY0dk16OXhEQTFIY0dWVnBwTXNBL3ZRSSt0Mnp2QlJOY0d3d2F0RGVJT3VidApSQlFybzYrK2ZhcUZVR1lVVng5Y0thTkNNRUF3RGdZRFZSMFBBUUgvQkFRREFnS2tNQThHQTFVZEV3RUIvd1FGCk1BTUJBZjh3SFFZRFZSME9CQllFRkVjd215Tk5nWGRCSS83NEhyS1JSWDQ1TzNpb01Bb0dDQ3FHU000OUJBTUMKQTBjQU1FUUNJQXI3UHFRK2NVLzlmWG1hSW4wWnJWWjZzcjR5MVo1MkZBeHc2VWxod0FkUUFpQjJCT0szNlozcgpQK1IrWkN5ZU9RQlhqQkpWd3RiUkxuRSs0OUxKcWdYelJnPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
  tls.key: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0tCk1IY0NBUUVFSUJPR2Mwek9ZM3pTZUl5MVJFa3EwTmpXQ3plQWd6djdSLy9FTVFwL2huNG9vQW9HQ0NxR1NNNDkKQXdFSG9VUURRZ0FFNDhic3FIVnhlSFdjSzBIT0NRTXY0dk16OXhEQTFIY0dWVnBwTXNBL3ZRSSt0Mnp2QlJOYwpHd3dhdERlSU91YnRSQlFybzYrK2ZhcUZVR1lVVng5Y0tRPT0KLS0tLS1FTkQgRUMgUFJJVkFURSBLRVktLS0tLQo=
kind: Secret
metadata:
  annotations:
    cert-manager.io/alt-names: ""
    cert-manager.io/certificate-name: cluster-default-root-certificate
    cert-manager.io/common-name: cluster-default-root-certificate
    cert-manager.io/ip-sans: ""
    cert-manager.io/issuer-group: cert-manager.io
    cert-manager.io/issuer-kind: Issuer
    cert-manager.io/issuer-name: cluster-default-selfsigned-issuer
    cert-manager.io/uri-sans: ""
  name: cluster-default-root-certificate
  namespace: default
type: kubernetes.io/tls

`
		externalCertSecretData := `
apiVersion: v1
data:
  ca.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJtRENDQVQ2Z0F3SUJBZ0lRWkR5ZlYvalBSUkxHdHJScWRKSDZkekFLQmdncWhrak9QUVFEQWpBc01Tb3cKS0FZRFZRUURFeUZqYkhWemRHVnlMV1Y0ZEdWeWJtRnNMWEp2YjNRdFkyVnlkR2xtYVdOaGRHVXdIaGNOTWpZdwpNekU0TVRJeE5UTXhXaGNOTXpFd016RTNNVEl4TlRNeFdqQXNNU293S0FZRFZRUURFeUZqYkhWemRHVnlMV1Y0CmRHVnlibUZzTFhKdmIzUXRZMlZ5ZEdsbWFXTmhkR1V3V1RBVEJnY3Foa2pPUFFJQkJnZ3Foa2pPUFFNQkJ3TkMKQUFTWVhNc0VSamJNY1hkNVJQOTVlKzFQYng5MGlhL2RWYm1rU241UlhlVm5XemVLWWM4TEJvcjFaYTNzNS92WApqNHlUOG44eFFwY3FLVklvOWhnSzBDQVRvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvCkJBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVUdwc21abmNrSTZYejBqVzkvbk5RaG15aVpGVXdDZ1lJS29aSXpqMEUKQXdJRFNBQXdSUUlnVDRTMldUNXRXOWsreWZVUUx4NE9oNUJWZk9TbkJKRjFtRHZxSjV3OW5mRUNJUURYRklBdAoxeFpvcEoxa1ZVdE1ITXVHTzIzN2tydXFVYXNhNXZrbGZPdkNkdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
  tls.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJtRENDQVQ2Z0F3SUJBZ0lRWkR5ZlYvalBSUkxHdHJScWRKSDZkekFLQmdncWhrak9QUVFEQWpBc01Tb3cKS0FZRFZRUURFeUZqYkhWemRHVnlMV1Y0ZEdWeWJtRnNMWEp2YjNRdFkyVnlkR2xtYVdOaGRHVXdIaGNOTWpZdwpNekU0TVRJeE5UTXhXaGNOTXpFd016RTNNVEl4TlRNeFdqQXNNU293S0FZRFZRUURFeUZqYkhWemRHVnlMV1Y0CmRHVnlibUZzTFhKdmIzUXRZMlZ5ZEdsbWFXTmhkR1V3V1RBVEJnY3Foa2pPUFFJQkJnZ3Foa2pPUFFNQkJ3TkMKQUFTWVhNc0VSamJNY1hkNVJQOTVlKzFQYng5MGlhL2RWYm1rU241UlhlVm5XemVLWWM4TEJvcjFaYTNzNS92WApqNHlUOG44eFFwY3FLVklvOWhnSzBDQVRvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvCkJBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVUdwc21abmNrSTZYejBqVzkvbk5RaG15aVpGVXdDZ1lJS29aSXpqMEUKQXdJRFNBQXdSUUlnVDRTMldUNXRXOWsreWZVUUx4NE9oNUJWZk9TbkJKRjFtRHZxSjV3OW5mRUNJUURYRklBdAoxeFpvcEoxa1ZVdE1ITXVHTzIzN2tydXFVYXNhNXZrbGZPdkNkdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
  tls.key: LS0tLS1CRUdJTiBFQyBQUklWQVRFIEtFWS0tLS0tCk1IY0NBUUVFSUlhRlpCQ1Fia3ZJTVZ0eGpUbTBPRWxPTWRjRnBxZVorUWM0SkVmZDV5dHFvQW9HQ0NxR1NNNDkKQXdFSG9VUURRZ0FFbUZ6TEJFWTJ6SEYzZVVUL2VYdnRUMjhmZEltdjNWVzVwRXArVVYzbFoxczNpbUhQQ3dhSwo5V1d0N09mNzE0K01rL0ovTVVLWEtpbFNLUFlZQ3RBZ0V3PT0KLS0tLS1FTkQgRUMgUFJJVkFURSBLRVktLS0tLQo=
kind: Secret
metadata:
  annotations:
    cert-manager.io/alt-names: ""
    cert-manager.io/certificate-name: cluster-external-root-certificate
    cert-manager.io/common-name: cluster-external-root-certificate
    cert-manager.io/ip-sans: ""
    cert-manager.io/issuer-group: cert-manager.io
    cert-manager.io/issuer-kind: Issuer
    cert-manager.io/issuer-name: cluster-external-selfsigned-issuer
    cert-manager.io/uri-sans: ""
  name: cluster-external-root-certificate
  namespace: default
type: kubernetes.io/tls

`
		rootIssuerData := `
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  labels:
    app.kubernetes.io/component: redpanda
    app.kubernetes.io/instance: cluster
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: redpanda
    cluster.redpanda.com/namespace: default
    cluster.redpanda.com/operator: v2
    cluster.redpanda.com/owner: cluster
    helm.sh/chart: redpanda-25.3.1
  name: cluster-default-root-issuer
  namespace: default
spec:
  ca:
    secretName: cluster-default-root-certificate


`
		externalIssuerData := `
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  labels:
    app.kubernetes.io/component: redpanda
    app.kubernetes.io/instance: cluster
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: redpanda
    cluster.redpanda.com/namespace: default
    cluster.redpanda.com/operator: v2
    cluster.redpanda.com/owner: cluster
    helm.sh/chart: redpanda-25.3.1
  name: cluster-external-root-issuer
  namespace: default
spec:
  ca:
    secretName: cluster-external-root-certificate

`
		rootSecret := unmarshalSecret(t, rootCertSecretData)
		externalSecret := unmarshalSecret(t, externalCertSecretData)
		require.NoError(t, cluster.Create(ctx, rootSecret))
		require.NoError(t, cluster.Create(ctx, externalSecret))

		rootIssuer := unmarshalIssuer(t, rootIssuerData)
		externalIssuer := unmarshalIssuer(t, externalIssuerData)
		require.NoError(t, retry.OnError(wait.Backoff{
			Steps:    100,
			Duration: 1 * time.Second,
			Factor:   1.0,
			Jitter:   0.1,
		}, func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "webhook")
		}, func() error {
			return cluster.Create(ctx, rootIssuer)
		}))
		require.NoError(t, cluster.Create(ctx, externalIssuer))

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
						"name": "redpanda-license",
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

	return stashNodes(ctx, clusterName, vclusters)
}

func cleanupWrapper(t framework.TestingT, f func(ctx context.Context)) {
	if testutil.MultiClusterSetupOnly() {
		// skip cleanup
		return
	}
	t.Cleanup(f)
}

func unmarshalSecret(t framework.TestingT, data string) *corev1.Secret {
	secret := &corev1.Secret{}
	require.NoError(t, yaml.Unmarshal([]byte(data), secret))
	return secret
}

func unmarshalIssuer(t framework.TestingT, data string) *certmanagerv1.Issuer {
	issuer := &certmanagerv1.Issuer{}
	require.NoError(t, yaml.Unmarshal([]byte(data), issuer))
	return issuer
}
