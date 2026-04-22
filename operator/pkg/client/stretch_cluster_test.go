// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/twmb/franz-go/pkg/kadm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestMulticlusterStretchClusterFactory(t *testing.T) {
	testutil.SkipIfNotMulticluster(t)
	suite.Run(t, new(StretchClusterFactorySuite))
}

type StretchClusterFactorySuite struct {
	suite.Suite

	ctx           context.Context
	mc            *testenv.MulticlusterEnv
	factory       *internalclient.Factory
	redpandaImage lifecycle.Image
	sidecarImage  lifecycle.Image
}

var _ suite.SetupAllSuite = (*StretchClusterFactorySuite)(nil)

func (s *StretchClusterFactorySuite) SetupSuite() {
	t := s.T()
	ctx := trace.Test(t)
	s.ctx = ctx

	cloudSecrets := lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false}
	s.redpandaImage = lifecycle.Image{
		Repository: os.Getenv("TEST_REDPANDA_REPO"),
		Tag:        os.Getenv("TEST_REDPANDA_VERSION"),
	}
	redpandaImage := s.redpandaImage
	s.sidecarImage = lifecycle.Image{
		Repository: "localhost/redpanda-operator",
		Tag:        "dev",
	}
	sidecarImage := s.sidecarImage

	// We need the multicluster dialer for both the operator's factory (inside
	// SetupFn) and the test's factory. Since NewMulticluster hasn't returned
	// yet when SetupFn runs, we capture the pointer and set it after.
	var mc *testenv.MulticlusterEnv
	s.mc = testenv.NewMulticlusterVind(t, s.ctx, testenv.MulticlusterOptions{
		Name:               "sc-factory",
		ClusterSize:        3,
		Scheme:             controller.MulticlusterScheme,
		CRDs:               crds.All(),
		Namespace:          "sc-factory",
		Logger:             log.FromContext(ctx),
		WatchAllNamespaces: true,
		InstallCertManager: true,
		ImportImages: []string{
			redpandaImage.Repository + ":" + redpandaImage.Tag,
			sidecarImage.Repository + ":" + sidecarImage.Tag,
			"quay.io/jetstack/cert-manager-controller:v1.17.2",
			"quay.io/jetstack/cert-manager-cainjector:v1.17.2",
			"quay.io/jetstack/cert-manager-webhook:v1.17.2",
		},
		SetupFn: func(mgr multicluster.Manager) error {
			// Use a dialer that delegates to mc.DialContext once mc is set.
			// The factory is created now but the dialer is only called later
			// during reconciliation, by which time mc is populated.
			factory := internalclient.NewFactory(mgr, nil).WithDialer(
				func(ctx context.Context, network, address string) (net.Conn, error) {
					return mc.DialContext(ctx, network, address)
				},
			)
			return redpandacontrollers.SetupMulticlusterController(ctx, mgr, redpandaImage, sidecarImage, cloudSecrets, factory)
		},
	})
	mc = s.mc

	mgr := s.mc.PrimaryManager()
	s.factory = internalclient.NewFactory(mgr, nil).WithDialer(s.mc.DialContext)
}

// applyNodePools creates one NodePool per cluster referencing the given StretchCluster.
func (s *StretchClusterFactorySuite) applyNodePools(t *testing.T, ctx context.Context, scName, ns, poolPrefix string) {
	t.Helper()
	for i, env := range s.mc.Envs {
		pool := &redpandav1alpha2.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", poolPrefix, i),
				Namespace: ns,
			},
			Spec: redpandav1alpha2.NodePoolSpec{
				EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
					Replicas: ptr.To(int32(1)),
				},
				ClusterRef: redpandav1alpha2.ClusterRef{
					Name:  scName,
					Group: ptr.To("cluster.redpanda.com"),
					Kind:  ptr.To("StretchCluster"),
				},
			},
		}

		gvk, err := env.Client().GroupVersionKindFor(pool)
		require.NoError(t, err)
		pool.GetObjectKind().SetGroupVersionKind(gvk)
		pool.SetManagedFields(nil)
		require.NoError(t, env.Client().Patch(ctx, pool, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO
	}
}

// waitForFinalizer waits until the StretchCluster has the operator finalizer.
func (s *StretchClusterFactorySuite) waitForFinalizer(t *testing.T, ctx context.Context, nn types.NamespacedName) {
	t.Helper()
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		return slices.Contains(cluster.Finalizers, redpandacontrollers.FinalizerKey)
	}, 2*time.Minute, 1*time.Second, "StretchCluster never received finalizer")
}

// waitForBrokerReady waits until at least one broker has ReadyReplicas > 0.
func (s *StretchClusterFactorySuite) waitForBrokerReady(t *testing.T, ctx context.Context, nn types.NamespacedName) {
	t.Helper()
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		for _, pool := range cluster.Status.NodePools {
			if pool.ReadyReplicas > 0 {
				return true
			}
		}
		return false
	}, 5*time.Minute, 5*time.Second, "no brokers became ready")
}

func (s *StretchClusterFactorySuite) TestFactoryClients() {
	t := s.T()
	t.Parallel()
	ctx := trace.Test(t)
	tn := s.mc.CreateTestNamespace(t)
	ns := tn.Name
	nn := types.NamespacedName{Name: "factory-test", Namespace: ns}

	// Deploy a StretchCluster with flat networking across all clusters.
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Networking: &redpandav1alpha2.Networking{
				CrossClusterMode: ptr.To(redpandav1alpha2.CrossClusterModeFlat),
			},
		},
	}
	s.mc.ApplyAll(t, ctx, sc)

	s.applyNodePools(t, ctx, nn.Name, ns, "pool")
	s.waitForFinalizer(t, ctx, nn)
	s.waitForBrokerReady(t, ctx, nn)

	// Re-fetch the StretchCluster for the factory.
	require.NoError(t, s.mc.Envs[0].Client().Get(ctx, nn, sc))

	t.Run("AdminClient", func(t *testing.T) {
		require.Eventually(t, func() bool {
			adminClient, err := s.factory.RedpandaAdminClient(ctx, sc)
			if err != nil {
				t.Logf("AdminClient error (will retry): %v", err)
				return false
			}
			defer adminClient.Close()

			brokers, err := adminClient.Brokers(context.Background())
			if err != nil {
				t.Logf("Brokers error (will retry): %v", err)
				return false
			}
			if len(brokers) == 0 {
				return false
			}
			for _, b := range brokers {
				if b.MembershipStatus != rpadmin.MembershipStatusActive {
					return false
				}
			}
			return true
		}, 2*time.Minute, 5*time.Second, "admin client never connected")
	})

	t.Run("KafkaClient", func(t *testing.T) {
		require.Eventually(t, func() bool {
			kafkaClient, err := s.factory.KafkaClient(ctx, sc)
			if err != nil {
				t.Logf("KafkaClient error (will retry): %v", err)
				return false
			}
			defer kafkaClient.Close()

			metadata, err := kadm.NewClient(kafkaClient).BrokerMetadata(context.Background())
			if err != nil {
				t.Logf("BrokerMetadata error (will retry): %v", err)
				return false
			}
			return len(metadata.Brokers.NodeIDs()) > 0
		}, 2*time.Minute, 5*time.Second, "kafka client never connected")
	})

	t.Run("SchemaRegistryClient", func(t *testing.T) {
		require.Eventually(t, func() bool {
			srClient, err := s.factory.SchemaRegistryClient(ctx, sc)
			if err != nil {
				t.Logf("SchemaRegistryClient error (will retry): %v", err)
				return false
			}
			_, err = srClient.SupportedTypes(context.Background())
			if err != nil {
				t.Logf("schema registry not ready: %v", err)
				return false
			}
			return true
		}, 2*time.Minute, 5*time.Second, "schema registry client never connected")
	})
}

func (s *StretchClusterFactorySuite) TestClusterConfigSync() {
	t := s.T()
	t.Parallel()
	ctx := trace.Test(t)
	tn := s.mc.CreateTestNamespace(t)
	ns := tn.Name
	nn := types.NamespacedName{Name: "config-sync", Namespace: ns}

	// Deploy a StretchCluster with SASL auth, initial cluster config, and
	// the restart-on-config-change annotation. External disabled to avoid
	// NodePort collisions with parallel tests.
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Annotations: map[string]string{
				"operator.redpanda.com/restart-cluster-on-config-change": "true",
			},
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Networking: &redpandav1alpha2.Networking{
				CrossClusterMode: ptr.To(redpandav1alpha2.CrossClusterModeFlat),
			},
			External: &redpandav1alpha2.External{
				Enabled: ptr.To(false),
			},
			RBAC: &redpandav1alpha2.RBAC{
				Enabled: ptr.To(true),
			},
			Auth: &redpandav1alpha2.Auth{
				SASL: &redpandav1alpha2.SASL{
					Enabled:   ptr.To(true),
					SecretRef: ptr.To("sasl-users"),
					Users: []redpandav1alpha2.UsersItems{
						{Name: ptr.To("admin"), Password: ptr.To("changeme")},
					},
				},
			},
			Config: &redpandav1alpha2.Config{
				Cluster: &runtime.RawExtension{
					Raw: []byte(`{"auto_create_topics_enabled":true}`),
				},
			},
		},
	}
	s.mc.ApplyAll(t, ctx, sc)

	s.applyNodePools(t, ctx, nn.Name, ns, "cfg-pool")
	s.waitForFinalizer(t, ctx, nn)
	s.waitForBrokerReady(t, ctx, nn)

	// Wait for ConfigurationApplied=True.
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(cluster.Status.Conditions, statuses.StretchClusterConfigurationApplied)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, 3*time.Minute, 5*time.Second, "ConfigurationApplied=True never appeared")

	// Wait for Stable=True.
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(cluster.Status.Conditions, statuses.StretchClusterStable)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, 3*time.Minute, 5*time.Second, "Stable=True never appeared")

	// Record the initial config version.
	var initialConfigVersion string
	{
		var cluster redpandav1alpha2.StretchCluster
		require.NoError(t, s.mc.Envs[0].Client().Get(ctx, nn, &cluster))
		initialConfigVersion = cluster.Status.ConfigVersion
	}
	require.NotEmpty(t, initialConfigVersion, "expected non-empty initial config version")

	// Mutate: add a restart-requiring property (append_chunk_size).
	// Must apply to ALL clusters to pass spec consistency check.
	mutatedSC := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Annotations: map[string]string{
				"operator.redpanda.com/restart-cluster-on-config-change": "true",
			},
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Networking: &redpandav1alpha2.Networking{
				CrossClusterMode: ptr.To(redpandav1alpha2.CrossClusterModeFlat),
			},
			External: &redpandav1alpha2.External{
				Enabled: ptr.To(false),
			},
			RBAC: &redpandav1alpha2.RBAC{
				Enabled: ptr.To(true),
			},
			Auth: &redpandav1alpha2.Auth{
				SASL: &redpandav1alpha2.SASL{
					Enabled:   ptr.To(true),
					SecretRef: ptr.To("sasl-users"),
					Users: []redpandav1alpha2.UsersItems{
						{Name: ptr.To("admin"), Password: ptr.To("changeme")},
					},
				},
			},
			Config: &redpandav1alpha2.Config{
				Cluster: &runtime.RawExtension{
					Raw: []byte(`{"auto_create_topics_enabled":true,"append_chunk_size":32768}`),
				},
			},
		},
	}
	s.mc.ApplyAll(t, ctx, mutatedSC)

	// Verify ConfigurationApplied returns to True and the config version changed.
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(cluster.Status.Conditions, statuses.StretchClusterConfigurationApplied)
		if cond == nil || cond.Status != metav1.ConditionTrue {
			return false
		}
		return cluster.Status.ConfigVersion != "" && cluster.Status.ConfigVersion != initialConfigVersion
	}, 3*time.Minute, 5*time.Second, "ConfigurationApplied=True with new config version never appeared after config change")
}

// TestResourceCleanup verifies that owned resources are properly cleaned up
// after NodePool and StretchCluster deletion. It creates a StretchCluster with
// 3 NodePools, waits for the cluster to become healthy, then runs sequential
// subtests that exercise different deletion scenarios.
func (s *StretchClusterFactorySuite) TestResourceCleanup() {
	t := s.T()
	t.Parallel()
	ctx := trace.Test(t)
	tn := s.mc.CreateTestNamespace(t)
	ns := tn.Name
	nn := types.NamespacedName{Name: "cleanup-test", Namespace: ns}

	ownerLabels := client.MatchingLabels{
		"cluster.redpanda.com/namespace": ns,
		"cluster.redpanda.com/owner":     nn.Name,
	}

	// Deploy a StretchCluster with flat networking across all clusters.
	// External access is explicitly disabled to avoid NodePort collisions
	// with parallel tests.
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Networking: &redpandav1alpha2.Networking{
				CrossClusterMode: ptr.To(redpandav1alpha2.CrossClusterModeFlat),
			},
			External: &redpandav1alpha2.External{
				Enabled: ptr.To(false),
			},
		},
	}
	s.mc.ApplyAll(t, ctx, sc)

	s.applyNodePools(t, ctx, nn.Name, ns, "cleanup-pool")
	s.waitForFinalizer(t, ctx, nn)

	// Wait for all 3 brokers to become ready.
	require.Eventually(t, func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
			return false
		}
		readyCount := int32(0)
		for _, pool := range cluster.Status.NodePools {
			readyCount += pool.ReadyReplicas
		}
		return readyCount >= 3
	}, 5*time.Minute, 5*time.Second, "not all brokers became ready")

	// Verify that owned resources exist before we start deleting.
	stsCounts := s.countOwnedResourcesAcrossClusters(t, ctx, &appsv1.StatefulSetList{}, ownerLabels)
	require.Equal(t, 3, stsCounts, "expected 3 StatefulSets (one per NodePool) before cleanup")

	svcCounts := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.ServiceList{}, ownerLabels, client.InNamespace(ns))
	require.Greater(t, svcCounts, 0, "expected Services to exist before cleanup")

	cmCounts := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.ConfigMapList{}, ownerLabels, client.InNamespace(ns))
	require.Greater(t, cmCounts, 0, "expected ConfigMaps to exist before cleanup")

	t.Run("SingleNodePoolDeletion", func(t *testing.T) {
		// Delete the NodePool from cluster 2 only.
		poolToDelete := "cleanup-pool-2"
		pool := &redpandav1alpha2.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolToDelete,
				Namespace: ns,
			},
		}
		require.NoError(t, s.mc.Envs[2].Client().Delete(ctx, pool))

		// The StretchCluster reconciler should decommission the broker and
		// delete the StatefulSet for the removed pool.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &appsv1.StatefulSetList{}, ownerLabels)
			t.Logf("StatefulSet count: %d (want 2)", count)
			return count == 2
		}, 5*time.Minute, 5*time.Second, "StatefulSet for deleted NodePool was not cleaned up")

		// The other two pools should remain healthy.
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
				return false
			}
			readyCount := int32(0)
			for _, pool := range cluster.Status.NodePools {
				readyCount += pool.ReadyReplicas
			}
			t.Logf("ready brokers: %d (want >= 2)", readyCount)
			return readyCount >= 2
		}, 3*time.Minute, 5*time.Second, "remaining pools did not stay healthy after single NodePool deletion")

		// Per-pod Services for the deleted pool's broker should be cleaned up.
		// The syncer GC removes Services that are no longer rendered.
		require.Eventually(t, func() bool {
			var svcs corev1.ServiceList
			if err := s.mc.Envs[2].Client().List(ctx, &svcs, ownerLabels, client.InNamespace(ns)); err != nil {
				return false
			}
			for _, svc := range svcs.Items {
				// Per-pod services are named with the cluster prefix: {clusterName}-{poolName}-{ordinal}.
				expectedSvcName := fmt.Sprintf("%s-%s-%d", nn.Name, poolToDelete, 0)
				if svc.Name == expectedSvcName {
					t.Logf("per-pod Service %s still exists on cluster 2", svc.Name)
					return false
				}
			}
			return true
		}, 3*time.Minute, 5*time.Second, "per-pod Service for deleted NodePool was not cleaned up")
	})

	t.Run("PartialStretchClusterDeletion", func(t *testing.T) {
		// Delete the StretchCluster from cluster 1 only (keep on clusters 0 and 2).
		// The partial deletion guard should block cleanup and preserve resources.
		scToDelete := &redpandav1alpha2.StretchCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}
		require.NoError(t, s.mc.Envs[1].Client().Delete(ctx, scToDelete))

		// The guard should block deletion — the SC stays in Terminating state
		// with its finalizer intact. Verify via ResourcesSynced condition.
		require.Eventually(t, func() bool {
			var sc redpandav1alpha2.StretchCluster
			if err := s.mc.Envs[1].Client().Get(ctx, nn, &sc); err != nil {
				return false
			}
			if sc.DeletionTimestamp.IsZero() {
				return false
			}
			cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterResourcesSynced)
			if cond == nil {
				return false
			}
			t.Logf("ResourcesSynced on cluster 1: status=%s reason=%s msg=%s", cond.Status, cond.Reason, cond.Message)
			return cond.Status == metav1.ConditionFalse
		}, 2*time.Minute, 5*time.Second, "partial deletion guard did not activate on cluster 1")

		// The SC on cluster 1 should still have its finalizer (deletion blocked).
		var guardedSC redpandav1alpha2.StretchCluster
		require.NoError(t, s.mc.Envs[1].Client().Get(ctx, nn, &guardedSC))
		require.True(t, slices.Contains(guardedSC.Finalizers, redpandacontrollers.FinalizerKey),
			"finalizer should be preserved by the partial deletion guard")

		// Resources on cluster 1 should still exist (no cleanup ran).
		stsCount := s.countOwnedResourcesAcrossClusters(t, ctx, &appsv1.StatefulSetList{}, ownerLabels)
		require.GreaterOrEqual(t, stsCount, 2, "StatefulSets should be preserved during guarded partial deletion")

		// The surviving cluster should remain healthy while the guard holds.
		require.Eventually(t, func() bool {
			var cluster redpandav1alpha2.StretchCluster
			if err := s.mc.Envs[0].Client().Get(ctx, nn, &cluster); err != nil {
				return false
			}
			for _, pool := range cluster.Status.NodePools {
				if pool.ReadyReplicas > 0 {
					return true
				}
			}
			return false
		}, 1*time.Minute, 5*time.Second, "surviving cluster lost all healthy brokers during guarded partial deletion")
	})

	t.Run("FullDeletion", func(t *testing.T) {
		// Delete all remaining NodePools across all clusters.
		s.mc.DeleteAll(t, ctx, ns, &redpandav1alpha2.NodePool{})
		// Delete the StretchCluster from all clusters.
		s.mc.DeleteAll(t, ctx, ns, &redpandav1alpha2.StretchCluster{})

		// Wait for the StretchCluster to be fully deleted on all clusters.
		// The SC on cluster 1 was already Terminating (from partial deletion);
		// once clusters 0 and 2 also start deleting, the guard passes and
		// cleanup proceeds on all clusters.
		for i, env := range s.mc.Envs {
			env := env
			i := i
			require.Eventually(t, func() bool {
				var list redpandav1alpha2.StretchClusterList
				if err := env.Client().List(ctx, &list, client.InNamespace(ns)); err != nil {
					return false
				}
				return len(list.Items) == 0
			}, 3*time.Minute, 1*time.Second, "StretchCluster was not fully deleted on cluster %d", i)
		}

		// Verify ALL owned StatefulSets are deleted across all clusters.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &appsv1.StatefulSetList{}, ownerLabels)
			t.Logf("StatefulSets remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "StatefulSets were not cleaned up after full deletion")

		// Verify owned Services are deleted.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.ServiceList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("Services remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "Services were not cleaned up after full deletion")

		// Verify owned ConfigMaps are deleted.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.ConfigMapList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("ConfigMaps remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "ConfigMaps were not cleaned up after full deletion")

		// Verify owned Secrets are deleted.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.SecretList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("Secrets remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "Secrets were not cleaned up after full deletion")

		// Verify owned PodDisruptionBudgets are deleted.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &policyv1.PodDisruptionBudgetList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("PDBs remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "PodDisruptionBudgets were not cleaned up after full deletion")

		// Verify owned ServiceAccounts are deleted.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.ServiceAccountList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("ServiceAccounts remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "ServiceAccounts were not cleaned up after full deletion")

		// Verify no Pods remain for the deleted cluster.
		require.Eventually(t, func() bool {
			count := s.countOwnedResourcesAcrossClusters(t, ctx, &corev1.PodList{}, ownerLabels, client.InNamespace(ns))
			t.Logf("Pods remaining: %d", count)
			return count == 0
		}, 2*time.Minute, 5*time.Second, "Pods were not cleaned up after full deletion")
	})
}

// countOwnedResourcesAcrossClusters counts resources matching the given options
// across all multicluster envs.
func (s *StretchClusterFactorySuite) countOwnedResourcesAcrossClusters(t *testing.T, ctx context.Context, list client.ObjectList, opts ...client.ListOption) int {
	t.Helper()
	total := 0
	for _, env := range s.mc.Envs {
		freshList := list.DeepCopyObject().(client.ObjectList)
		if err := env.Client().List(ctx, freshList, opts...); err != nil {
			t.Logf("error listing %T on %s: %v", list, env.Name, err)
			continue
		}
		items, err := apimeta.ExtractList(freshList)
		if err != nil {
			t.Logf("error extracting list items: %v", err)
			continue
		}
		total += len(items)
	}
	return total
}
