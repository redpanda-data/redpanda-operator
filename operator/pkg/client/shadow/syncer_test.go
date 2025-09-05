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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow/adminv2"
)

func getTestImage() string {
	return "redpandadata/redpanda-nightly:v0.0.0-20250904git366e4b6"
}

func TestSyncer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	clusterOne := runShadowLinkEnabledCluster(t, ctx, "user", "password")
	clusterTwo := runShadowLinkEnabledCluster(t, ctx, "user", "password")

	syncer := NewSyncer(clusterOne.v2Client)
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

	tasks, topics, err := syncer.Sync(ctx, link, clusterTwo.remoteClusterSettings())

	require.NoError(t, err)

	fmt.Println(tasks, topics)
	// TODO: add in expectations

	// require.NoError(t, syncer.Delete(ctx, link))
}

func runShadowLinkEnabledCluster(t *testing.T, ctx context.Context, username, password string) *cluster {
	t.Helper()

	container, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers(username),
		redpanda.WithNewServiceAccount(username, password),
	)

	adminAddress, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	kafkaAddress, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	rpadminClient, err := rpadmin.NewAdminAPI([]string{adminAddress}, &rpadmin.BasicAuth{
		Username: username,
		Password: password,
	}, nil)
	require.NoError(t, err)

	adminV2Client, err := adminv2.NewClientBuilder(adminAddress).WithBasicAuth(username, password).Build()
	require.NoError(t, err)

	c := &cluster{
		username: username,
		password: password,
		kafka:    kafkaAddress,
		admin:    adminAddress,
		client:   rpadminClient,
		v2Client: adminV2Client,
	}

	c.enableDevelopmentFeature(t, ctx, "development_enable_cluster_link")
	return c
}

type cluster struct {
	username string
	password string
	kafka    string
	admin    string

	client   *rpadmin.AdminAPI
	v2Client *adminv2.Client
}

func (c *cluster) remoteClusterSettings() RemoteClusterSettings {
	return RemoteClusterSettings{
		BootstrapServers: []string{c.kafka},
		Authentication: &AuthenticationSettings{
			Username:  c.username,
			Password:  c.password,
			Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
		},
	}
}

func (c *cluster) enableDevelopmentFeature(t *testing.T, ctx context.Context, feature string) {
	t.Helper()

	// Enable experimental feature support.
	//
	// The key must be equal to the current broker time expressed as unix epoch
	// in seconds, and be within 1 hour.
	_, err := c.client.PatchClusterConfig(ctx, map[string]any{
		"enable_developmental_unrecoverable_data_corrupting_features": time.Now().Unix(),
	}, []string{})
	require.NoError(t, err)

	// now enable the feature
	_, err = c.client.PatchClusterConfig(ctx, map[string]any{
		feature: true,
	}, []string{})
	require.NoError(t, err)
}
