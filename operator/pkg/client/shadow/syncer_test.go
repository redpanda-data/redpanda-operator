// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package shadow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow/adminv2"
)

// const redpandaTestContainerImage = "docker.redpanda.com/redpandadata/redpanda:"

func getTestImage() string {
	// containerTag := os.Getenv("TEST_REDPANDA_VERSION")
	// return redpandaTestContainerImage + containerTag
	return "redpandadata/redpanda-nightly:v0.0.0-20250904git366e4b6"
}

func TestSyncer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	c, err := client.New(cfg, client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	containerOne, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	containerTwo, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	adminOne, err := containerOne.AdminAPIAddress(ctx)
	require.NoError(t, err)

	adminTwo, err := containerTwo.AdminAPIAddress(ctx)
	require.NoError(t, err)

	rpadminClientOne, err := adminv2.NewClientBuilder(adminOne).WithBasicAuth("user", "password").Build()
	require.NoError(t, err)

	syncer := NewSyncer(rpadminClientOne)
	defer syncer.Close()

	link := &redpandav1alpha2.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "link",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.ShadowLinkSpec{
			SourceCluster: redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: "bogus",
				},
			},
			DestinationCluster: redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: "bogus",
				},
			},
			TopicMetadataSyncOptions:  &redpandav1alpha2.ShadowLinkTopicMetadataSyncOptions{},
			ConsumerOffsetSyncOptions: &redpandav1alpha2.ShadowLinkConsumerOffsetSyncOptions{},
			SecuritySyncOptions:       &redpandav1alpha2.ShadowLinkSecuritySettingsSyncOptions{},
		},
	}

	require.NoError(t, c.Create(ctx, link))
	_, _, err = syncer.Sync(ctx, link, RemoteClusterSettings{
		BootstrapServers: []string{adminTwo},
		Authentication: &AuthenticationSettings{
			Username:  "user",
			Password:  "password",
			Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
		},
	})
	require.NoError(t, err)

	// TODO: add in expectations

	// require.NoError(t, syncer.Delete(ctx, link))
}
