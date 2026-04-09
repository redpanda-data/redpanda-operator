// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

const (
	operatorChartPath = "../../../../../operator/chart"
	licenseEnvVar     = "REDPANDA_SAMPLE_LICENSE"
)

func TestMulticlusterBootstrapAndStatus(t *testing.T) {
	testutil.SkipIfNotMulticluster(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	opts := vcluster.MulticlusterOptions{
		Size:              3,
		OperatorChartPath: operatorChartPath,
	}

	// Create 3 vclusters.
	mc := vcluster.NewMulticluster(t, ctx, opts)
	require.Len(t, mc.Nodes, 3)

	// Build connections for the Config structs.
	conns := connectionsFromNodes(t, mc.Nodes)

	// Build DNS overrides from node external IPs.
	var dnsOverrides []string
	for _, node := range mc.Nodes {
		dnsOverrides = append(dnsOverrides, fmt.Sprintf("%s=%s", node.Name(), node.ExternalIP()))
	}

	// Run the bootstrap command and verify its results.
	t.Run("bootstrap", func(t *testing.T) {
		var out bytes.Buffer
		cfg := multicluster.BootstrapConfig{
			Connection: multicluster.ConnectionConfig{
				Namespace:   opts.Namespace,
				ServiceName: "redpanda-operator-multicluster",
				Connections: conns,
			},
			Organization: "Redpanda",
			DNSOverrides: dnsOverrides,
			TLS:          true,
			Kubeconfigs:  true,
			CreateNS:     true,
		}
		require.NoError(t, cfg.Run(ctx, &out), "bootstrap failed: %s", out.String())
		t.Logf("bootstrap output: %s", out.String())

		t.Run("creates_tls_secrets", func(t *testing.T) {
			t.Parallel()
			for _, node := range mc.Nodes {
				var secrets corev1.SecretList
				require.NoError(t, node.Ctl().List(ctx, opts.Namespace, &secrets))

				var found bool
				for _, sec := range secrets.Items {
					if sec.Name == "redpanda-operator-multicluster-certificates" {
						found = true
						assert.NotEmpty(t, sec.Data["ca.crt"])
						assert.NotEmpty(t, sec.Data["tls.crt"])
						assert.NotEmpty(t, sec.Data["tls.key"])
						break
					}
				}
				assert.True(t, found, "TLS secret not found in vcluster %s", node.Name())
			}
		})

		t.Run("ca_is_consistent", func(t *testing.T) {
			t.Parallel()
			var cas [][]byte
			for _, node := range mc.Nodes {
				var sec corev1.Secret
				require.NoError(t, node.Ctl().Get(ctx, kube.ObjectKey{
					Name: "redpanda-operator-multicluster-certificates", Namespace: opts.Namespace,
				}, &sec))
				cas = append(cas, sec.Data["ca.crt"])
			}
			for i := 1; i < len(cas); i++ {
				assert.Equal(t, string(cas[0]), string(cas[i]),
					"CA mismatch between cluster 0 and cluster %d", i)
			}
		})

		t.Run("certs_are_valid", func(t *testing.T) {
			t.Parallel()
			for _, node := range mc.Nodes {
				var sec corev1.Secret
				require.NoError(t, node.Ctl().Get(ctx, kube.ObjectKey{
					Name: "redpanda-operator-multicluster-certificates", Namespace: opts.Namespace,
				}, &sec))

				caBlock, _ := pem.Decode(sec.Data["ca.crt"])
				require.NotNil(t, caBlock)
				caCert, err := x509.ParseCertificate(caBlock.Bytes)
				require.NoError(t, err)
				assert.True(t, caCert.IsCA)

				certBlock, _ := pem.Decode(sec.Data["tls.crt"])
				require.NotNil(t, certBlock)
				cert, err := x509.ParseCertificate(certBlock.Bytes)
				require.NoError(t, err)

				pool := x509.NewCertPool()
				pool.AddCert(caCert)
				_, err = cert.Verify(x509.VerifyOptions{
					Roots:     pool,
					KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
				})
				assert.NoError(t, err, "cert chain verification failed on %s", node.Name())
				assert.True(t, time.Now().Before(cert.NotAfter),
					"cert expired on %s in %s", cert.NotAfter, node.Name())
			}
		})
	})

	// Deploy operators (requires license).
	license := os.Getenv(licenseEnvVar)
	if license == "" {
		t.Log("REDPANDA_SAMPLE_LICENSE not set, skipping operator deployment tests")
		return
	}
	mc.DeployOperators(t, ctx, opts, peerValues(mc.Nodes), license)

	t.Run("deploy", func(t *testing.T) {
		t.Run("operators_become_ready", func(t *testing.T) {
			t.Parallel()
			for _, node := range mc.Nodes {
				require.Eventually(t, func() bool {
					var pods corev1.PodList
					if err := node.Ctl().List(ctx, opts.Namespace, &pods, client.MatchingLabels{
						"app.kubernetes.io/name": "operator",
					}); err != nil || len(pods.Items) == 0 {
						return false
					}
					for _, cond := range pods.Items[0].Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							return true
						}
					}
					return false
				}, 5*time.Minute, 5*time.Second,
					"operator pod never became ready in %s", node.Name())
			}
		})

		t.Run("status", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			cfg := multicluster.StatusConfig{
				Connection: multicluster.ConnectionConfig{
					Namespace:   opts.Namespace,
					ServiceName: "operator",
					Connections: conns,
				},
			}
			result, err := cfg.Run(ctx, &out)
			require.NoError(t, err, "status failed: %s", out.String())
			t.Logf("status output:\n%s", out.String())

			// Verify all clusters appear in the output.
			for _, node := range mc.Nodes {
				assert.Contains(t, out.String(), node.Name())
			}

			// Pod and deployment checks should pass.
			for i, rs := range result.ClusterResults {
				for _, r := range rs {
					if r.Name == "pod" || r.Name == "deployment" {
						assert.True(t, r.OK, "[%s] check %s failed: %s",
							result.Contexts[i].Context, r.Name, r.Message)
					}
				}
			}

			// CA consistency should pass after bootstrap.
			for _, r := range result.CrossResults {
				if r.Name == "ca-consistency" || r.Name == "unique-names" {
					assert.True(t, r.OK, "cross-cluster check %s failed: %s", r.Name, r.Message)
				}
			}
		})
	})
}

func connectionsFromNodes(t *testing.T, nodes []*vcluster.MulticlusterNode) []multicluster.ClusterConnection {
	t.Helper()
	conns := make([]multicluster.ClusterConnection, len(nodes))
	for i, node := range nodes {
		conns[i] = multicluster.ClusterConnection{
			Name: node.Name(),
			Ctl:  node.Ctl(),
		}
	}
	return conns
}

func peerValues(nodes []*vcluster.MulticlusterNode) []map[string]any {
	peers := make([]map[string]any, len(nodes))
	for i, node := range nodes {
		peers[i] = map[string]any{
			"name":    node.Name(),
			"address": node.ExternalIP(),
		}
	}
	return peers
}
