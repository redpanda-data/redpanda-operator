// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// listenerWithCert returns a StretchAPIListener whose embedded
// StretchListener has TLS set to the given cert name.
func listenerWithCert(certName string) *redpandav1alpha2.StretchAPIListener {
	return &redpandav1alpha2.StretchAPIListener{
		StretchListener: redpandav1alpha2.StretchListener{
			TLS: &redpandav1alpha2.StretchListenerTLS{
				Cert: ptr.To(certName),
			},
		},
	}
}

func TestManagedCertNamesForBootstrap_NoPoolsReturnsWellKnownFallback(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	names := ManagedCertNamesForBootstrap(sc, nil)
	require.Equal(t,
		[]string{redpandav1alpha2.DefaultCertName, redpandav1alpha2.ExternalCertName},
		names,
		"with no pools, the syncCA fallback must cover the well-known default+external CAs so the operator can pre-bootstrap them",
	)
}

func TestManagedCertNamesForBootstrap_DefaultPoolYieldsDefaultAndExternal(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				External: &redpandav1alpha2.External{
					Enabled: ptr.To(true),
					Type:    ptr.To("NodePort"),
				},
			},
		},
	}

	names := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})
	require.Equal(t,
		[]string{redpandav1alpha2.DefaultCertName, redpandav1alpha2.ExternalCertName},
		names,
		"a defaulted pool with external enabled requires both default (internal listeners) and external (external listeners) operator-managed CAs",
	)
}

func TestManagedCertNamesForBootstrap_CustomOperatorManagedCertIsIncluded(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						// "custom" has neither SecretRef nor IssuerRef →
						// operator-managed, so syncCA must bootstrap its CA.
						"custom": {Enabled: ptr.To(true)},
					},
				},
				Listeners: &redpandav1alpha2.StretchListeners{
					Kafka: listenerWithCert("custom"),
				},
			},
		},
	}

	names := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})
	// Defaulting adds {default, external} for the other listeners and the
	// default external (which inherits external cert name) — "custom" sits
	// alongside both because Kafka explicitly points at it.
	require.Equal(t,
		[]string{"custom", redpandav1alpha2.DefaultCertName, redpandav1alpha2.ExternalCertName},
		names,
		"custom operator-managed cert names must end up in the syncCA set alongside the defaulted internal/external CAs",
	)
}

func TestManagedCertNamesForBootstrap_IssuerRefCertIsExcluded(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						// User-owned issuer: cert-manager creates the leaf
						// directly from this Issuer, so the operator must
						// NOT bootstrap a CA Secret for it.
						redpandav1alpha2.DefaultCertName: {
							Enabled: ptr.To(true),
							IssuerRef: &redpandav1alpha2.IssuerRef{
								Name: ptr.To("user-issuer"),
								Kind: ptr.To("ClusterIssuer"),
							},
						},
						redpandav1alpha2.ExternalCertName: {
							Enabled: ptr.To(true),
							IssuerRef: &redpandav1alpha2.IssuerRef{
								Name: ptr.To("user-issuer"),
								Kind: ptr.To("ClusterIssuer"),
							},
						},
					},
				},
				External: &redpandav1alpha2.External{
					Enabled: ptr.To(true),
					Type:    ptr.To("NodePort"),
				},
			},
		},
	}

	names := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})
	require.Empty(t, names,
		"when all cert names use IssuerRef, syncCA must create no CA Secrets — Codex flagged the unused-Secret leak in the review",
	)
}

func TestManagedCertNamesForBootstrap_SecretRefCertIsExcluded(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						redpandav1alpha2.DefaultCertName: {
							Enabled:   ptr.To(true),
							SecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("user-secret")},
						},
					},
				},
			},
		},
	}

	names := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})
	// `default` is SecretRef'd → excluded. The defaulted external listener
	// still references the operator-managed `external` cert, so the
	// remaining set is exactly that — and nothing more.
	require.Equal(t,
		[]string{redpandav1alpha2.ExternalCertName},
		names,
		"SecretRef on `default` must drop only `default` from the bootstrap set, not the unrelated `external` CA",
	)
}

func TestManagedCertNamesForBootstrap_UnionAcrossPools(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	poolA := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						"only-on-a": {Enabled: ptr.To(true)},
					},
				},
				Listeners: &redpandav1alpha2.StretchListeners{
					Kafka: listenerWithCert("only-on-a"),
				},
			},
		},
	}
	poolB := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						"only-on-b": {Enabled: ptr.To(true)},
					},
				},
				Listeners: &redpandav1alpha2.StretchListeners{
					Admin: listenerWithCert("only-on-b"),
				},
			},
		},
	}

	names := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{poolA, poolB})
	// Both pools' custom cert names plus the defaulted {default, external}
	// from each pool's listeners must be present, deduplicated and sorted.
	require.Equal(t,
		[]string{redpandav1alpha2.DefaultCertName, redpandav1alpha2.ExternalCertName, "only-on-a", "only-on-b"},
		names,
		"union must contain every cert name referenced across all pools, including defaulted ones",
	)
}

func TestManagedCertNamesForBootstrap_DoesNotMutateInputs(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	}
	scBefore := sc.DeepCopy()
	poolBefore := pool.DeepCopy()

	_ = ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})

	require.Equal(t, scBefore, sc, "input StretchCluster must not be mutated")
	require.Equal(t, poolBefore, pool, "input NodePool must not be mutated")
}

// Regression guard: syncCA and certIssuers must agree on the cert names
// they handle, otherwise either Issuers reference missing CA Secrets or CA
// Secrets are created for cert names no Issuer is rendered for. Walk both
// paths from the same NodePool input and assert their cert-name outputs
// match exactly.
func TestManagedCertNamesForBootstrap_AgreesWithCertIssuers(t *testing.T) {
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				TLS: &redpandav1alpha2.TLS{
					Enabled: ptr.To(true),
					Certs: map[string]*redpandav1alpha2.Certificate{
						"custom-managed": {Enabled: ptr.To(true)},
						"with-issuer": {
							Enabled:   ptr.To(true),
							IssuerRef: &redpandav1alpha2.IssuerRef{Name: ptr.To("x")},
						},
						redpandav1alpha2.DefaultCertName:  {Enabled: ptr.To(true)},
						redpandav1alpha2.ExternalCertName: {Enabled: ptr.To(true)},
					},
				},
				Listeners: &redpandav1alpha2.StretchListeners{
					Kafka: listenerWithCert("custom-managed"),
					Admin: listenerWithCert("with-issuer"),
				},
				External: &redpandav1alpha2.External{
					Enabled: ptr.To(true),
					Type:    ptr.To("NodePort"),
				},
			},
		},
	}

	syncCANames := ManagedCertNamesForBootstrap(sc, []*redpandav1alpha2.NodePool{pool})

	state, err := NewRenderState(nil, sc, []*redpandav1alpha2.NodePool{pool}, []*redpandav1alpha2.NodePool{pool}, "test")
	require.NoError(t, err)
	issuers := certIssuers(state)
	issuerCertNames := make([]string, 0, len(issuers))
	for _, p := range state.inClusterPools {
		for _, bc := range bootstrappedCerts(state.PoolSpec(p)) {
			issuerCertNames = append(issuerCertNames, bc.name)
		}
	}
	sort.Strings(issuerCertNames)
	sort.Strings(syncCANames)

	require.Equal(t, len(issuerCertNames), len(issuers),
		"bookkeeping drift in the renderer: certIssuers and bootstrappedCerts disagree on how many CAs to render",
	)
	require.Equal(t, issuerCertNames, syncCANames,
		"syncCA and certIssuers must produce the same cert-name set; any divergence means either cert-manager stalls on a missing CA Secret or the operator creates an unused CA Secret",
	)
}
