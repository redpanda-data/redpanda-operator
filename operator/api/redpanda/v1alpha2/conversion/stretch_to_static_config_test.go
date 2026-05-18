// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

func TestConvertStretchClusterToStaticConfig_NilInput(t *testing.T) {
	require.Nil(t, ConvertStretchClusterToStaticConfig(nil, nil))
	require.Nil(t, ConvertStretchClusterToStaticConfig(&redpandav1alpha2.StretchCluster{}, nil))
}

func TestConvertStretchClusterToStaticConfig(t *testing.T) {
	const (
		scName   = "redpanda"
		scNs     = "redpanda-ns"
		expHost  = "redpanda.redpanda-ns.svc.cluster.local"
		userName = "kubernetes-controller"
	)

	tests := []struct {
		name   string
		mutate func(*redpandav1alpha2.StretchCluster, *redpandav1alpha2.EmbeddedNodePoolSpec)
		// Optional per-case assertions; the common assertions on host /
		// ports are always applied.
		assert func(*testing.T, *ir.StaticConfigurationSource)
	}{
		{
			// MergeDefaults defaults TLS to enabled across all listeners, so a
			// "no opinion" spec yields TLS-on URLs. This matches the Helm
			// chart's historical defaulting.
			name:   "default spec (TLS auto-enabled, no SASL)",
			mutate: func(_ *redpandav1alpha2.StretchCluster, _ *redpandav1alpha2.EmbeddedNodePoolSpec) {},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.NotNil(t, cfg.Kafka.TLS)
				require.Nil(t, cfg.Kafka.SASL)
				require.NotNil(t, cfg.Admin.TLS)
				require.Nil(t, cfg.Admin.Auth)
				require.NotNil(t, cfg.SchemaRegistry.TLS)
				require.Nil(t, cfg.SchemaRegistry.SASL)
				require.Equal(t, []string{"https://" + expHost + ":9644"}, cfg.Admin.URLs)
				require.Equal(t, []string{"https://" + expHost + ":8081"}, cfg.SchemaRegistry.URLs)
			},
		},
		{
			name: "TLS explicitly disabled, no SASL",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{Enabled: ptr.To(false)}
				// Per-listener TLS overrides global; if the listener
				// hasn't been touched MergeDefaults still gives it a
				// nil-Enabled TLS struct, which falls back to globalTLS.
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.Nil(t, cfg.Kafka.TLS)
				require.Nil(t, cfg.Admin.TLS)
				require.Nil(t, cfg.SchemaRegistry.TLS)
				require.Equal(t, []string{"http://" + expHost + ":9644"}, cfg.Admin.URLs)
				require.Equal(t, []string{"http://" + expHost + ":8081"}, cfg.SchemaRegistry.URLs)
				require.Equal(t, []string{expHost + ":9093"}, cfg.Kafka.Brokers)
			},
		},
		{
			name: "TLS enabled with default cert, no SASL",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{Enabled: ptr.To(true)}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.NotNil(t, cfg.Kafka.TLS)
				require.NotNil(t, cfg.Kafka.TLS.CaCert.SecretKeyRef)
				require.Equal(t, scName+"-default-root-certificate", cfg.Kafka.TLS.CaCert.SecretKeyRef.Name)
				require.Equal(t, corev1.TLSCertKey, cfg.Kafka.TLS.CaCert.SecretKeyRef.Key)
				require.Equal(t, []string{"https://" + expHost + ":9644"}, cfg.Admin.URLs)
				require.Equal(t, []string{"https://" + expHost + ":8081"}, cfg.SchemaRegistry.URLs)
			},
		},
		{
			name: "TLS enabled with user-provided SecretRef",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						"default": {
							SecretRef: &redpandav1alpha2.SecretRef{
								Name: ptr.To("user-cert"),
							},
						},
					},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.Equal(t, "user-cert", cfg.Kafka.TLS.CaCert.SecretKeyRef.Name)
				require.Equal(t, corev1.TLSCertKey, cfg.Kafka.TLS.CaCert.SecretKeyRef.Key)
			},
		},
		{
			name: "TLS enabled with IssuerRef (ca.crt key in leaf secret)",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						"default": {
							IssuerRef: &redpandav1alpha2.IssuerRef{
								Name: ptr.To("my-issuer"),
							},
						},
					},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				// Leaf cert secret is <fullname>-<certName>-cert; CA chain
				// lives under the ca.crt key.
				require.Equal(t, scName+"-default-cert", cfg.Kafka.TLS.CaCert.SecretKeyRef.Name)
				require.Equal(t, "ca.crt", cfg.Kafka.TLS.CaCert.SecretKeyRef.Key)
			},
		},
		{
			name: "SASL enabled, default mechanism",
			mutate: func(sc *redpandav1alpha2.StretchCluster, _ *redpandav1alpha2.EmbeddedNodePoolSpec) {
				sc.Spec.Auth = &redpandav1alpha2.Auth{
					SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.NotNil(t, cfg.Kafka.SASL)
				require.Equal(t, userName, cfg.Kafka.SASL.Username)
				require.Equal(t, ir.SASLMechanism("SCRAM-SHA-512"), cfg.Kafka.SASL.Mechanism)
				require.Equal(t, scName+"-bootstrap-user", cfg.Kafka.SASL.Password.SecretKeyRef.Name)
				require.Equal(t, "password", cfg.Kafka.SASL.Password.SecretKeyRef.Key)
				require.Equal(t, userName, cfg.Admin.Auth.Username)
				require.Equal(t, scName+"-bootstrap-user", cfg.Admin.Auth.Password.SecretKeyRef.Name)
				require.Equal(t, userName, cfg.SchemaRegistry.SASL.Username)
			},
		},
		{
			name: "SASL enabled, explicit mechanism",
			mutate: func(sc *redpandav1alpha2.StretchCluster, _ *redpandav1alpha2.EmbeddedNodePoolSpec) {
				sc.Spec.Auth = &redpandav1alpha2.Auth{
					SASL: &redpandav1alpha2.SASL{
						Enabled:   ptr.To(true),
						Mechanism: ptr.To("SCRAM-SHA-256"),
					},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.Equal(t, ir.SASLMechanism("SCRAM-SHA-256"), cfg.Kafka.SASL.Mechanism)
			},
		},
		{
			name: "TLS + SASL together",
			mutate: func(sc *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{Enabled: ptr.To(true)}
				sc.Spec.Auth = &redpandav1alpha2.Auth{
					SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.NotNil(t, cfg.Kafka.TLS)
				require.NotNil(t, cfg.Kafka.SASL)
				require.NotNil(t, cfg.Admin.TLS)
				require.NotNil(t, cfg.Admin.Auth)
				require.NotNil(t, cfg.SchemaRegistry.TLS)
				require.NotNil(t, cfg.SchemaRegistry.SASL)
			},
		},
		{
			name: "custom cluster domain (TLS disabled)",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.ClusterDomain = ptr.To("example.internal")
				p.TLS = &redpandav1alpha2.TLS{Enabled: ptr.To(false)}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				expected := "redpanda.redpanda-ns.svc.example.internal"
				require.Equal(t, []string{expected + ":9093"}, cfg.Kafka.Brokers)
				require.Equal(t, []string{"http://" + expected + ":9644"}, cfg.Admin.URLs)
				require.Equal(t, []string{"http://" + expected + ":8081"}, cfg.SchemaRegistry.URLs)
			},
		},
		{
			// TLS is globally disabled, but the kafka listener is
			// explicitly turned back on. Other listeners stay plaintext.
			name: "per-listener TLS opt-in over disabled global",
			mutate: func(_ *redpandav1alpha2.StretchCluster, p *redpandav1alpha2.EmbeddedNodePoolSpec) {
				p.TLS = &redpandav1alpha2.TLS{Enabled: ptr.To(false)}
				p.Listeners = &redpandav1alpha2.StretchListeners{
					Kafka: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{
							TLS: &redpandav1alpha2.StretchListenerTLS{Enabled: ptr.To(true)},
						},
					},
				}
			},
			assert: func(t *testing.T, cfg *ir.StaticConfigurationSource) {
				require.NotNil(t, cfg.Kafka.TLS, "kafka should have TLS")
				require.Nil(t, cfg.Admin.TLS, "admin should not have TLS")
				require.Nil(t, cfg.SchemaRegistry.TLS, "schema registry should not have TLS")
				require.Equal(t, []string{"http://" + expHost + ":9644"}, cfg.Admin.URLs)
				require.Equal(t, []string{"http://" + expHost + ":8081"}, cfg.SchemaRegistry.URLs)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := &redpandav1alpha2.StretchCluster{
				ObjectMeta: metav1.ObjectMeta{Name: scName, Namespace: scNs},
			}
			poolSpec := &redpandav1alpha2.EmbeddedNodePoolSpec{}
			tc.mutate(sc, poolSpec)

			// Default the pool the same way callers (e.g. console controller,
			// stretch client factory) do before invoking the converter.
			defaultedClusterSpec := *sc.Spec.DeepCopy()
			defaultedClusterSpec.MergeDefaults()
			poolSpec.MergeDefaultsFrom(&defaultedClusterSpec)

			cfg := ConvertStretchClusterToStaticConfig(sc, poolSpec)
			require.NotNil(t, cfg)
			require.NotNil(t, cfg.Kafka)
			require.NotNil(t, cfg.Admin)
			require.NotNil(t, cfg.SchemaRegistry)

			// Default headless-service host + canonical default ports.
			if poolSpec.ClusterDomain == nil {
				require.Equal(t, []string{expHost + ":9093"}, cfg.Kafka.Brokers)
			}

			if tc.assert != nil {
				tc.assert(t, cfg)
			}
		})
	}
}
