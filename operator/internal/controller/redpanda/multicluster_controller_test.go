// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestMulticlusterController(t *testing.T) {
	testutil.SkipIfNotMulticluster(t)
	suite.Run(t, new(MulticlusterControllerSuite))
}

type MulticlusterControllerSuite struct {
	suite.Suite

	ctx context.Context
	mc  *testenv.MulticlusterEnv
}

var (
	_ suite.SetupAllSuite  = (*MulticlusterControllerSuite)(nil)
	_ suite.SetupTestSuite = (*MulticlusterControllerSuite)(nil)
)

func (s *MulticlusterControllerSuite) setup() (*testing.T, context.Context, context.CancelFunc, *testenv.MulticlusterTestNamespace) {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 5*time.Minute)
	ns := s.mc.CreateTestNamespace(t)
	return t, ctx, cancel, ns
}

func (s *MulticlusterControllerSuite) SetupTest() {
	prev := s.ctx
	s.ctx = trace.Test(s.T())
	s.T().Cleanup(func() {
		s.ctx = prev
	})
}

func (s *MulticlusterControllerSuite) SetupSuite() {
	t := s.T()
	s.ctx = trace.Test(t)

	cloudSecrets := lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false}
	redpandaImage := lifecycle.Image{
		Repository: os.Getenv("TEST_REDPANDA_REPO"),
		Tag:        os.Getenv("TEST_REDPANDA_VERSION"),
	}
	sidecarImage := lifecycle.Image{
		Repository: "localhost/redpanda-operator",
		Tag:        "dev",
	}

	s.mc = testenv.NewMulticlusterVind(t, s.ctx, testenv.MulticlusterOptions{
		Name:               "multicluster",
		ClusterSize:        3,
		Scheme:             controller.MulticlusterScheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(s.ctx),
		WatchAllNamespaces: true,
		InstallCertManager: true,
		SetupFn: func(mgr multicluster.Manager) error {
			return redpanda.SetupMulticlusterController(s.ctx, mgr, redpandaImage, sidecarImage, cloudSecrets, nil)
		},
	})
}

func (s *MulticlusterControllerSuite) TestManagesFinalizers() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	nn := types.NamespacedName{Name: "stretch", Namespace: ns.Name}

	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: nn.Name,
		},
	})

	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &cluster); err != nil {
				t.Logf("[TestManagesFinalizers] Get on env %d (%s): %v", i, env.Name, err)
				return false
			}
			t.Logf("[TestManagesFinalizers] Get on env %d (%s): finalizers=%v", i, env.Name, cluster.Finalizers)
			return slices.Contains(cluster.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d (%s) never contained finalizer", i, env.Name))
	}

	s.mc.DeleteAll(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{})

	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			err := cl.Get(ctx, nn, &cluster)
			return k8sapierrors.IsNotFound(err)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d was never deleted", i))
	}
}

func (s *MulticlusterControllerSuite) TestSpecConsistencyConditionSetOnDrift() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	nn := types.NamespacedName{Name: "spec-drift", Namespace: ns.Name}

	// Apply identical StretchCluster to all clusters.
	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: nn.Name,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			CommonLabels: map[string]string{"env": "prod"},
		},
	})

	// Wait for the reconciler to pick it up (finalizer added = reconciler ran).
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &sc); err != nil {
				return false
			}
			return slices.Contains(sc.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, fmt.Sprintf("cluster in env %d never got finalizer", i))
	}

	// With identical specs, SpecSynced should be True.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, 1*time.Minute, 1*time.Second, "SpecSynced=True condition never appeared")

	// Introduce drift: patch the spec on one cluster only.
	driftedEnv := s.mc.Envs[0]
	var sc redpandav1alpha2.StretchCluster
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := driftedEnv.Client().Get(ctx, nn, &sc); err != nil {
			return err
		}
		sc.Spec.CommonLabels = map[string]string{"env": "staging"}
		return driftedEnv.Client().Update(ctx, &sc)
	}))

	// The reconciler should detect drift and set the condition.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == "DriftDetected" {
				return true
			}
		}
		return false
	}, 1*time.Minute, 1*time.Second, "SpecSynced=False condition never appeared after drift")

	// Verify the condition message mentions the differing field.
	for _, env := range s.mc.Envs {
		var sc redpandav1alpha2.StretchCluster
		if err := env.Client().Get(ctx, nn, &sc); err != nil {
			continue
		}
		cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			require.Contains(t, cond.Message, "commonLabels", "condition message should mention the drifting field")
		}
	}

	// Fix the drift: align the spec back.
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := driftedEnv.Client().Get(ctx, nn, &sc); err != nil {
			return err
		}
		sc.Spec.CommonLabels = map[string]string{"env": "prod"}
		return driftedEnv.Client().Update(ctx, &sc)
	}))

	// The condition should go back to True.
	require.Eventually(t, func() bool {
		for _, env := range s.mc.Envs {
			var sc redpandav1alpha2.StretchCluster
			if err := env.Client().Get(ctx, nn, &sc); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterSpecSynced)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, 1*time.Minute, 1*time.Second, "SpecSynced condition never went back to True after fixing drift")
}

// TestIssuerRef verifies that when a user provides their own CA and Issuers
// (Option 1), the operator:
//   - Does NOT generate or distribute root-certificate secrets for certs with IssuerRef
//   - Creates cert-manager Certificate resources that reference the user's Issuer
//   - cert-manager issues leaf certs with ca.crt containing the user's CA
//   - All clusters end up with leaf certs signed by the same user-provided CA
func (s *MulticlusterControllerSuite) TestIssuerRef() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	const (
		caSecretName = "user-ca"
		issuerName   = "user-ca-issuer"
		scName       = "tls-issuer"
	)
	nn := types.NamespacedName{Name: scName, Namespace: ns.Name}

	// Step 1: Generate a CA that the "user" would create.
	ca, err := bootstrap.GenerateCA("test-org", "user-ca", nil)
	require.NoError(t, err)

	// Step 2: Create the CA Secret and a cert-manager CA Issuer in each cluster.
	// This simulates the user distributing their CA and creating Issuers.
	for i, env := range s.mc.Envs {
		cl := env.Client()

		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      caSecretName,
				Namespace: ns.Name,
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				corev1.TLSCertKey:       ca.Bytes(),
				corev1.TLSPrivateKeyKey: ca.PrivateKeyBytes(),
				"ca.crt":                ca.Bytes(),
			},
		}
		require.NoError(t, cl.Create(ctx, caSecret), "creating CA secret in cluster %d", i)

		issuer := &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: caSecretName,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, issuer), "creating Issuer in cluster %d", i)
	}

	// Step 3: Create a StretchCluster with IssuerRef pointing to the user's Issuer.
	// ApplyInternalDNSNames must be true so that cert-manager generates certs
	// with DNS SANs (cert-manager requires at least one subject identifier).
	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: scName,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			TLS: &redpandav1alpha2.TLS{
				Enabled: ptr.To(true),
				Certs: map[string]*redpandav1alpha2.Certificate{
					"default": {
						CAEnabled:             ptr.To(true),
						ApplyInternalDNSNames: ptr.To(true),
						IssuerRef: &redpandav1alpha2.IssuerRef{
							Name:  ptr.To(issuerName),
							Kind:  ptr.To("Issuer"),
							Group: ptr.To("cert-manager.io"),
						},
					},
				},
			},
		},
	})

	// Step 4: Wait for the reconciler to pick it up (finalizer added).
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &sc); err != nil {
				return false
			}
			return slices.Contains(sc.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, "cluster in env %d never got finalizer", i)
	}

	// Step 5: Verify that NO operator-managed root-certificate secret was created
	// for the "default" cert. syncCA should have skipped it.
	rootCertSecretName := rendermulticluster.CASecretName(scName, "default")
	for i, env := range s.mc.Envs {
		cl := env.Client()
		var secret corev1.Secret
		err := cl.Get(ctx, client.ObjectKey{
			Namespace: ns.Name,
			Name:      rootCertSecretName,
		}, &secret)
		require.True(t, k8sapierrors.IsNotFound(err),
			"root-certificate secret %q should NOT exist in cluster %d (IssuerRef is set), but got: %v",
			rootCertSecretName, i, err)
	}

	// Step 6: Wait for cert-manager to create the leaf cert secret in each cluster.
	// The reconciler creates Certificate resources; cert-manager then issues them.
	// We look for the server cert secret name that the renderer produces.
	defaultedSpec := redpandav1alpha2.StretchClusterSpec{
		TLS: &redpandav1alpha2.TLS{
			Enabled: ptr.To(true),
			Certs: map[string]*redpandav1alpha2.Certificate{
				"default": {
					CAEnabled:             ptr.To(true),
					ApplyInternalDNSNames: ptr.To(true),
					IssuerRef: &redpandav1alpha2.IssuerRef{
						Name:  ptr.To(issuerName),
						Kind:  ptr.To("Issuer"),
						Group: ptr.To("cert-manager.io"),
					},
				},
			},
		},
	}
	defaultedSpec.MergeDefaults()
	leafCertSecretName := defaultedSpec.TLS.CertServerSecretName(scName, "default")

	leafCACerts := make([][]byte, len(s.mc.Envs))
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var secret corev1.Secret
			if err := cl.Get(ctx, client.ObjectKey{
				Namespace: ns.Name,
				Name:      leafCertSecretName,
			}, &secret); err != nil {
				t.Logf("[TestIssuerRef] waiting for leaf cert secret %q in cluster %d: %v", leafCertSecretName, i, err)
				return false
			}
			// cert-manager must have populated tls.crt, tls.key, and ca.crt.
			if _, ok := secret.Data[corev1.TLSCertKey]; !ok {
				return false
			}
			if _, ok := secret.Data[corev1.TLSPrivateKeyKey]; !ok {
				return false
			}
			caCrt, ok := secret.Data["ca.crt"]
			if !ok || len(caCrt) == 0 {
				return false
			}
			leafCACerts[i] = caCrt
			return true
		}, 2*time.Minute, 2*time.Second, "leaf cert secret %q never appeared in cluster %d", leafCertSecretName, i)
	}

	// Step 7: Verify all clusters have the same CA in their leaf cert's ca.crt.
	// This confirms that the user's CA is consistently used across clusters.
	for i := 1; i < len(leafCACerts); i++ {
		require.True(t, bytes.Equal(leafCACerts[0], leafCACerts[i]),
			"ca.crt in leaf cert differs between cluster 0 and cluster %d", i)
	}

	// Verify the CA in the leaf cert matches the user-provided CA.
	require.True(t, bytes.Equal(ca.Bytes(), leafCACerts[0]),
		"ca.crt in leaf cert does not match the user-provided CA")
}

// TestUserProvidedCA verifies Option 3: the user pre-creates a CA secret with
// the well-known naming convention in a single cluster. The operator's syncCA
// should find it, distribute it to all other clusters, and create Issuers
// backed by it. cert-manager then issues leaf certs signed by the user's CA.
func (s *MulticlusterControllerSuite) TestUserProvidedCA() {
	t, ctx, cancel, ns := s.setup()
	defer cancel()

	const scName = "tls-userca"
	nn := types.NamespacedName{Name: scName, Namespace: ns.Name}

	// Step 1: Generate a CA that the "user" would create.
	ca, err := bootstrap.GenerateCA("user-org", "user-provided-ca", nil)
	require.NoError(t, err)

	// Step 2: Pre-create the CA Secret in just ONE cluster using the well-known
	// name that syncCA scans for: "{stretchClusterName}-{certName}-root-certificate".
	// The operator should find it and distribute it to the other clusters.
	caSecretName := rendermulticluster.CASecretName(scName, "default")
	t.Logf("pre-creating CA secret %q in cluster 0 only", caSecretName)

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: ns.Name,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       ca.Bytes(),
			corev1.TLSPrivateKeyKey: ca.PrivateKeyBytes(),
			"ca.crt":                ca.Bytes(),
		},
	}
	require.NoError(t, s.mc.Envs[0].Client().Create(ctx, caSecret))

	// Step 3: Create a StretchCluster with default TLS (no IssuerRef).
	// The operator should use the pre-existing CA secret rather than
	// generating a new one.
	s.mc.ApplyAllInNamespace(t, ctx, ns.Name, &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: scName,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			TLS: &redpandav1alpha2.TLS{
				Enabled: ptr.To(true),
				Certs: map[string]*redpandav1alpha2.Certificate{
					"default": {
						CAEnabled: ptr.To(true),
					},
				},
			},
		},
	})

	// Step 4: Wait for the reconciler to pick it up.
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := cl.Get(ctx, nn, &sc); err != nil {
				return false
			}
			return slices.Contains(sc.Finalizers, redpanda.FinalizerKey)
		}, 1*time.Minute, 1*time.Second, "cluster in env %d never got finalizer", i)
	}

	// Step 5: Verify the CA secret was distributed to ALL clusters by syncCA.
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var secret corev1.Secret
			if err := cl.Get(ctx, client.ObjectKey{
				Namespace: ns.Name,
				Name:      caSecretName,
			}, &secret); err != nil {
				t.Logf("[TestUserProvidedCA] waiting for CA secret %q in cluster %d: %v", caSecretName, i, err)
				return false
			}
			return len(secret.Data[corev1.TLSCertKey]) > 0
		}, 1*time.Minute, 1*time.Second, "CA secret %q never appeared in cluster %d", caSecretName, i)
	}

	// Step 6: Verify the distributed CA matches the user's original CA.
	for i, env := range s.mc.Envs {
		cl := env.Client()
		var secret corev1.Secret
		require.NoError(t, cl.Get(ctx, client.ObjectKey{
			Namespace: ns.Name,
			Name:      caSecretName,
		}, &secret))
		require.True(t, bytes.Equal(ca.Bytes(), secret.Data[corev1.TLSCertKey]),
			"CA secret tls.crt in cluster %d does not match the user-provided CA", i)
	}

	// Step 7: Verify cert-manager Issuers were created backed by the user's CA.
	// The renderer creates "{fullname}-default-root-issuer" per cluster.
	issuerName := fmt.Sprintf("%s-default-root-issuer", scName)
	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var issuer certmanagerv1.Issuer
			if err := cl.Get(ctx, client.ObjectKey{
				Namespace: ns.Name,
				Name:      issuerName,
			}, &issuer); err != nil {
				t.Logf("[TestUserProvidedCA] waiting for Issuer %q in cluster %d: %v", issuerName, i, err)
				return false
			}
			// Verify the Issuer references our CA secret.
			return issuer.Spec.CA != nil && issuer.Spec.CA.SecretName == caSecretName
		}, 2*time.Minute, 2*time.Second, "Issuer %q never appeared in cluster %d", issuerName, i)
	}

	// Step 8: Wait for cert-manager to issue leaf certs and verify they use the user's CA.
	defaultedSpec := redpandav1alpha2.StretchClusterSpec{
		TLS: &redpandav1alpha2.TLS{
			Enabled: ptr.To(true),
			Certs: map[string]*redpandav1alpha2.Certificate{
				"default": {CAEnabled: ptr.To(true)},
			},
		},
	}
	defaultedSpec.MergeDefaults()
	leafCertSecretName := defaultedSpec.TLS.CertServerSecretName(scName, "default")

	for i, env := range s.mc.Envs {
		cl := env.Client()
		require.Eventually(t, func() bool {
			var secret corev1.Secret
			if err := cl.Get(ctx, client.ObjectKey{
				Namespace: ns.Name,
				Name:      leafCertSecretName,
			}, &secret); err != nil {
				t.Logf("[TestUserProvidedCA] waiting for leaf cert secret %q in cluster %d: %v", leafCertSecretName, i, err)
				return false
			}
			caCrt, ok := secret.Data["ca.crt"]
			if !ok || len(caCrt) == 0 {
				return false
			}
			// The leaf cert's ca.crt should match the user's CA.
			return bytes.Equal(ca.Bytes(), caCrt)
		}, 2*time.Minute, 2*time.Second, "leaf cert secret %q never appeared with correct CA in cluster %d", leafCertSecretName, i)
	}
}
