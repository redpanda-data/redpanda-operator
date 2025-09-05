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
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow/adminv2"
)

func getTestImage() string {
	return "redpandadata/redpanda-nightly:v0.0.0-20250904git366e4b6"
}

func TestSyncer(t *testing.T) {
	syncTime := 1 * time.Second
	linkName := "link"
	topicName := "topic"
	user := "user"
	password := "password"

	consumer := "consumer"

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	clusterOne := runShadowLinkEnabledCluster(t, ctx, "one", user, password)
	clusterTwo := runShadowLinkEnabledCluster(t, ctx, "two", user, password)

	// setup cluster values to sync
	{
		clusterOne.createACL(t, ctx, user)
		clusterOne.createTopic(t, ctx, topicName)
		clusterOne.publishSentinel(t, ctx, topicName)
		clusterOne.consumeSentinel(t, ctx, topicName, consumer)
	}

	link := &redpandav1alpha2.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      linkName,
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
			TopicMetadataSyncOptions: &redpandav1alpha2.ShadowLinkTopicMetadataSyncOptions{
				Interval: syncTime,
				TopicFilters: []redpandav1alpha2.NameFilter{{
					Name:        topicName,
					FilterType:  redpandav1alpha2.FilterTypeInclude,
					PatternType: redpandav1alpha2.PatternTypeLiteral,
				}},
			},
			ConsumerOffsetSyncOptions: &redpandav1alpha2.ShadowLinkConsumerOffsetSyncOptions{
				Interval: syncTime,
				Enabled:  true,
				GroupFilters: []redpandav1alpha2.NameFilter{{
					Name:        "*",
					FilterType:  redpandav1alpha2.FilterTypeInclude,
					PatternType: redpandav1alpha2.PatternTypeLiteral,
				}},
			},
			SecuritySyncOptions: &redpandav1alpha2.ShadowLinkSecuritySettingsSyncOptions{
				Interval: syncTime,
				Enabled:  true,
				ACLFilters: []redpandav1alpha2.ACLFilter{{
					ResourceFilter: redpandav1alpha2.ACLResourceFilter{
						Name: "*",
					},
					AccessFilter: redpandav1alpha2.ACLAccessFilter{
						Host:      "*",
						Principal: "User:" + user,
					},
				}},
			},
		},
	}

	syncer := NewSyncer(clusterTwo.v2Client)
	defer syncer.Close()

	// ensure nothing exists
	require.False(t, clusterTwo.hasTopic(t, ctx, topicName))

	// create the cluster link
	status, err := syncer.Sync(ctx, link, clusterOne.remoteClusterSettings())
	require.NoError(t, err)

	// initial check that the link is active
	require.Equal(t, redpandav1alpha2.ShadowLinkStateActive, status.State)

	// wait for the out-of-band sync by the cluster
	time.Sleep(syncTime + 1*time.Second)

	response, err := clusterTwo.v2Client.ShadowLinks().GetShadowLink(ctx, connect.NewRequest(&adminv2api.GetShadowLinkRequest{
		Name: linkName,
	}))
	require.NoError(t, err)

	// check that we've marked this as syncing
	require.Len(t, response.Msg.ShadowLink.Status.ShadowTopicStatuses, 1)
	require.Equal(t, topicName, response.Msg.ShadowLink.Status.ShadowTopicStatuses[0].Name)
	require.Equal(t, adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_ACTIVE, response.Msg.ShadowLink.Status.ShadowTopicStatuses[0].State)

	// TODO: add expectations around syncing other info

	// check all the data has been synced over
	require.True(t, clusterTwo.hasTopic(t, ctx, topicName))

	// clusterTwo.dumpLogs(t, ctx)

	// TODO: currently doesn't work
	// TODO: check sonsumer offset data
	// require.True(t, clusterTwo.hasACL(t, ctx, user))

	// TODO: update and check statuses
	// status, err = syncer.Sync(ctx, link, clusterTwo.remoteClusterSettings())
	// require.NoError(t, err)

	// require.NoError(t, syncer.Delete(ctx, link))
}

func runShadowLinkEnabledCluster(t *testing.T, ctx context.Context, name, username, password string) *cluster {
	t.Helper()

	container, err := redpanda.Run(ctx, getTestImage(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers(username),
		redpanda.WithNewServiceAccount(username, password),
		redpanda.WithListener(fmt.Sprintf("%s:9094", name)),
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Name:           name,
				Networks:       []string{ensureNetwork(t, ctx)},
				NetworkAliases: map[string][]string{},
			},
		}),
	)
	require.NoError(t, err)

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

	kafkaClient, err := kgo.NewClient(kgo.SeedBrokers(kafkaAddress), kgo.SASL(scram.Auth{
		User: username,
		Pass: password,
	}.AsSha256Mechanism()))
	require.NoError(t, err)

	c := &cluster{
		name:        name,
		username:    username,
		password:    password,
		kafka:       kafkaAddress,
		admin:       adminAddress,
		container:   container,
		kafkaClient: kafkaClient,
		client:      rpadminClient,
		v2Client:    adminV2Client,
	}

	c.enableDevelopmentFeature(t, ctx, "development_enable_cluster_link")

	t.Cleanup(c.close)

	return c
}

type cluster struct {
	name     string
	username string
	password string
	kafka    string
	admin    string

	container   *redpanda.Container
	kafkaClient *kgo.Client
	client      *rpadmin.AdminAPI
	v2Client    *adminv2.Client
}

func (c *cluster) close() {
	c.kafkaClient.Close()
	c.client.Close()
	c.v2Client.Close()
	c.container.Terminate(context.Background())
}

func (c *cluster) remoteClusterSettings() RemoteClusterSettings {
	return RemoteClusterSettings{
		BootstrapServers: []string{fmt.Sprintf("%s:9094", c.name)},
		Authentication: &AuthenticationSettings{
			Username:  c.username,
			Password:  c.password,
			Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
		},
	}
}

func (c *cluster) createACL(t *testing.T, ctx context.Context, user string) {
	t.Helper()

	req := kmsg.NewPtrCreateACLsRequest()
	req.Creations = []kmsg.CreateACLsRequestCreation{
		{
			Principal:           "User:" + user,
			Host:                "*",
			ResourceName:        "*",
			ResourcePatternType: kmsg.ACLResourcePatternTypeLiteral,
			ResourceType:        kmsg.ACLResourceTypeTopic,
			Operation:           kmsg.ACLOperationAll,
			PermissionType:      kmsg.ACLPermissionTypeAllow,
		},
	}
	creation, err := req.RequestWith(ctx, c.kafkaClient)
	require.NoError(t, err)
	for _, result := range creation.Results {
		require.NoError(t, checkError(result.ErrorMessage, result.ErrorCode))
	}
}

func (c *cluster) hasACL(t *testing.T, ctx context.Context, user string) bool { //nolint: unused  // This is stubbed out and will eventually be used
	t.Helper()

	req := kmsg.NewPtrDescribeACLsRequest()
	req.Principal = ptr.To("User:" + user)
	req.Host = ptr.To("*")
	req.ResourcePatternType = kmsg.ACLResourcePatternTypeLiteral
	req.ResourceType = kmsg.ACLResourceTypeTopic
	req.Operation = kmsg.ACLOperationAll
	req.PermissionType = kmsg.ACLPermissionTypeAllow
	response, err := req.RequestWith(ctx, c.kafkaClient)
	require.NoError(t, err)
	require.NoError(t, checkError(response.ErrorMessage, response.ErrorCode))

	return len(response.Resources) > 0
}

func (c *cluster) createTopic(t *testing.T, ctx context.Context, name string) {
	t.Helper()

	topic := kmsg.NewPtrCreateTopicsRequest()
	topic.Topics = []kmsg.CreateTopicsRequestTopic{
		{
			Topic:             name,
			ReplicationFactor: 1,
			NumPartitions:     1,
		},
	}
	response, err := topic.RequestWith(ctx, c.kafkaClient)
	require.NoError(t, err)
	for _, topic := range response.Topics {
		require.NoError(t, checkError(topic.ErrorMessage, topic.ErrorCode))
	}
}

func (c *cluster) publishSentinel(t *testing.T, ctx context.Context, name string) {
	t.Helper()

	require.NoError(t, c.kafkaClient.ProduceSync(ctx, &kgo.Record{Topic: name, Value: []byte("1")}).FirstErr())
}

func (c *cluster) consumeSentinel(t *testing.T, ctx context.Context, name, consumer string) {
	t.Helper()

	consumerClient, err := kgo.NewClient(append(c.kafkaClient.Opts(),
		kgo.ConsumerGroup(consumer),
		kgo.ConsumeTopics(name),
	)...)
	require.NoError(t, err)
	defer consumerClient.Close()

	fetches := consumerClient.PollFetches(ctx)
	require.NoError(t, fetches.Err())
	records := fetches.Records()
	require.Len(t, records, 1)
	require.Equal(t, "1", string(records[0].Value))
	require.NoError(t, consumerClient.CommitUncommittedOffsets(ctx))
}

func (c *cluster) hasTopic(t *testing.T, ctx context.Context, name string) bool {
	t.Helper()

	metadata := kmsg.NewPtrMetadataRequest()
	topic := kmsg.NewMetadataRequestTopic()
	topic.Topic = kmsg.StringPtr(name)
	metadata.Topics = []kmsg.MetadataRequestTopic{topic}

	response, err := metadata.RequestWith(ctx, c.kafkaClient)
	require.NoError(t, err)

	for _, topic := range response.Topics {
		err := kerr.ErrorForCode(topic.ErrorCode)
		if err == kerr.UnknownTopicOrPartition {
			return false
		}
		require.NoError(t, err)
	}
	return len(response.Topics) > 0
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

func (c *cluster) dumpLogs(t *testing.T, ctx context.Context) { //nolint: unused  // This is stubbed out and will eventually be used
	logs, err := c.container.Logs(ctx)
	require.NoError(t, err)

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, logs)
	require.NoError(t, err)

	t.Log(buffer.String())
}

var (
	networkName string
	networkOnce sync.Once
)

func ensureNetwork(t *testing.T, ctx context.Context) string {
	t.Helper()
	networkOnce.Do(func() {
		net, err := network.New(ctx)
		require.NoError(t, err)

		t.Cleanup(func() {
			require.NoError(t, net.Remove(context.Background()))
		})
		networkName = net.Name
	})

	return networkName
}

func checkError(message *string, code int16) error {
	var errMessage string
	if message != nil {
		errMessage = "Error: " + *message + "; "
	}

	if code != 0 {
		return fmt.Errorf("%s%w", errMessage, kerr.ErrorForCode(code))
	}

	return nil
}
