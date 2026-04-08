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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
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

var (
	_ suite.SetupAllSuite  = (*StretchClusterFactorySuite)(nil)
	_ suite.SetupTestSuite = (*StretchClusterFactorySuite)(nil)
)

func (s *StretchClusterFactorySuite) SetupTest() {
	prev := s.ctx
	s.ctx = trace.Test(s.T())
	s.T().Cleanup(func() {
		s.ctx = prev
	})
}

func (s *StretchClusterFactorySuite) SetupSuite() {
	t := s.T()
	s.ctx = trace.Test(t)

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
	s.mc = testenv.NewMulticluster(t, s.ctx, testenv.MulticlusterOptions{
		Name:               "sc-factory",
		ClusterSize:        3,
		Scheme:             controller.MulticlusterScheme,
		CRDs:               crds.All(),
		Namespace:          "sc-factory",
		Logger:             log.FromContext(s.ctx),
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
			return redpandacontrollers.SetupMulticlusterController(s.ctx, mgr, redpandaImage, sidecarImage, cloudSecrets, factory)
		},
	})
	mc = s.mc

	mgr := s.mc.PrimaryManager()
	s.factory = internalclient.NewFactory(mgr, nil).WithDialer(s.mc.DialContext)
}

func (s *StretchClusterFactorySuite) TestFactoryClients() {
	t := s.T()
	ns := "sc-factory"
	nn := types.NamespacedName{Name: "factory-test", Namespace: ns}

	// Deploy a StretchCluster with flat networking across all clusters.
	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Networking: &redpandav1alpha2.Networking{
				CrossClusterMode: ptr.To("flat"),
			},
		},
	}
	s.mc.ApplyAll(t, s.ctx, sc)

	// Deploy NodePools on each cluster.
	for i, env := range s.mc.Envs {
		pool := &redpandav1alpha2.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pool-%d", i),
				Namespace: ns,
			},
			Spec: redpandav1alpha2.NodePoolSpec{
				EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
					Replicas: ptr.To(int32(1)),
				},
				ClusterRef: redpandav1alpha2.ClusterRef{
					Name:  nn.Name,
					Group: ptr.To("cluster.redpanda.com"),
					Kind:  ptr.To("StretchCluster"),
				},
			},
		}

		gvk, err := env.Client().GroupVersionKindFor(pool)
		require.NoError(t, err)
		pool.GetObjectKind().SetGroupVersionKind(gvk)
		pool.SetManagedFields(nil)
		require.NoError(t, env.Client().Patch(s.ctx, pool, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO
	}

	// Wait for the reconciler to process the cluster (finalizer present).
	s.Require().Eventually(func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(s.ctx, nn, &cluster); err != nil {
			return false
		}
		return slices.Contains(cluster.Finalizers, redpandacontrollers.FinalizerKey)
	}, 2*time.Minute, 1*time.Second, "StretchCluster never received finalizer")

	// Wait for at least one broker to become ready.
	// In flat network mode, the controller manages EndpointSlices for remote
	// per-pod Services, so brokers can discover each other without manual fixup.
	s.Require().Eventually(func() bool {
		var cluster redpandav1alpha2.StretchCluster
		if err := s.mc.Envs[0].Client().Get(s.ctx, nn, &cluster); err != nil {
			return false
		}
		for _, pool := range cluster.Status.NodePools {
			if pool.ReadyReplicas > 0 {
				return true
			}
		}
		return false
	}, 5*time.Minute, 5*time.Second, "no brokers became ready")

	// Re-fetch the StretchCluster for the factory.
	require.NoError(t, s.mc.Envs[0].Client().Get(s.ctx, nn, sc))

	t.Run("AdminClient", func(t *testing.T) {
		require.Eventually(t, func() bool {
			adminClient, err := s.factory.RedpandaAdminClient(s.ctx, sc)
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
			kafkaClient, err := s.factory.KafkaClient(s.ctx, sc)
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
			srClient, err := s.factory.SchemaRegistryClient(s.ctx, sc)
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
