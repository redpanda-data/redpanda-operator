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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func getTestImage() string {
	// this is the latest nightly image that contains shadow links, once a release
	// with shadow links is actually cut, we can switch to the typical release
	// images
	return "redpandadata/redpanda-nightly:v0.0.0-20251023git2fede32"
}

func TestSyncer(t *testing.T) {
	syncRetryPeriod := 10 * time.Second

	// TODO: a current bug in duration parsing in core winds up considering duration intervals zigzag encoded
	// we explicitly choose 2 seconds as that represents the value 1s in zigzag whereas 1s decodes to -1s and
	// winds up with the maximum sync interval -- choosing 2s works around that to speed up tests
	// see https://github.com/redpanda-data/redpanda/pull/27941 which has been merged but is not yet available
	// in nightly builds.
	syncTime := ptr.To(metav1.Duration{Duration: 2 * time.Second})
	linkName := "link"
	topicName := "topic"
	topicTwo := "other"
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
		clusterOne.createTopic(t, ctx, topicTwo)
		clusterOne.publishSentinel(t, ctx, topicName)
		clusterOne.consumeSentinel(t, ctx, topicName, consumer)
	}

	link := &redpandav1alpha2.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      linkName,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.ShadowLinkSpec{
			ShadowCluster: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: "bogus",
				},
			},
			SourceCluster: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: "bogus",
				},
			},
			TopicMetadataSyncOptions: &redpandav1alpha2.ShadowLinkTopicMetadataSyncOptions{
				Interval: syncTime,
				AutoCreateShadowTopicFilters: []redpandav1alpha2.NameFilter{{
					Name:        topicName,
					FilterType:  redpandav1alpha2.FilterTypeInclude,
					PatternType: redpandav1alpha2.PatternTypeLiteral,
				}},
			},
			ConsumerOffsetSyncOptions: &redpandav1alpha2.ShadowLinkConsumerOffsetSyncOptions{
				Interval: syncTime,
				Enabled:  true,
				GroupFilters: []redpandav1alpha2.NameFilter{{
					Name:        consumer,
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

	syncer := NewSyncer(clusterTwo.client)
	defer syncer.Close()

	// ensure nothing exists
	require.False(t, clusterTwo.hasTopic(t, ctx, topicName))

	// create the cluster link
	status, err := syncer.Sync(ctx, link, clusterOne.remoteClusterSettings())
	require.NoError(t, err)

	// initial check that the link is active
	require.Equal(t, redpandav1alpha2.ShadowLinkStateActive, status.State)

	// wrap in a retry since the update of state is asynchronous
	require.Eventually(t, func() bool {
		return clusterTwo.hasActiveMirroredTopics(t, ctx, linkName, topicName)
	}, syncRetryPeriod, 1*time.Second, "shadow link never synchronized")

	// check all the data has been synced over
	require.Eventually(t, func() bool {
		return clusterTwo.hasACL(t, ctx, user)
	}, syncRetryPeriod, 1*time.Second, "cluster two never had ACL synchronized")

	var clusterOneOffset int64
	var clusterTwoOffset int64
	assert.Eventually(t, func() bool {
		clusterOneOffset = clusterOne.getConsumerOffset(t, ctx, topicName, consumer)
		clusterTwoOffset = clusterTwo.getConsumerOffset(t, ctx, topicName, consumer)
		t.Logf("checking cluster offsets, expected (cluster one): %d, actual (cluster two): %d", clusterOneOffset, clusterTwoOffset)
		return clusterOneOffset == clusterTwoOffset
	}, syncRetryPeriod, 1*time.Second, "cluster offsets not equal expected: %d, actual: %d", clusterOneOffset, clusterTwoOffset)

	if t.Failed() {
		clusterTwo.dumpLogs(t, ctx)
	}

	// Update
	link.Spec.TopicMetadataSyncOptions.AutoCreateShadowTopicFilters[0].Name = topicTwo
	_, err = syncer.Sync(ctx, link, clusterTwo.remoteClusterSettings())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return clusterTwo.hasActiveMirroredTopics(t, ctx, linkName, topicName, topicTwo)
	}, syncRetryPeriod, 1*time.Second, "topic %q never synced", topicTwo)

	require.NoError(t, syncer.Delete(ctx, link))
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

	require.NoError(t, rpadminClient.SetLogLevel(ctx, "cluster_link", "trace", 120))
	require.NoError(t, rpadminClient.SetLogLevel(ctx, "shadow_link_service", "trace", 120))

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
	}

	c.enableDevelopmentFeature(t, ctx, "enable_shadow_linking")

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
}

func (c *cluster) close() {
	c.kafkaClient.Close()
	c.client.Close()
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

func (c *cluster) hasActiveMirroredTopics(t *testing.T, ctx context.Context, linkName string, topicNames ...string) bool {
	t.Helper()

	response, err := c.client.ShadowLinkService().GetShadowLink(ctx, connect.NewRequest(&adminv2api.GetShadowLinkRequest{
		Name: linkName,
	}))
	require.NoError(t, err)

	if len(response.Msg.ShadowLink.Status.ShadowTopics) != len(topicNames) {
		return false
	}

	// check that we've marked all topics as syncing
	for _, topicName := range topicNames {
		found := false
		for _, topic := range response.Msg.ShadowLink.Status.ShadowTopics {
			if topic.Name == topicName {
				found = topic.Name == topicName && topic.Status.State == adminv2api.ShadowTopicState_SHADOW_TOPIC_STATE_ACTIVE
				break
			}
		}
		if !found {
			return false
		}
		if !c.hasTopic(t, ctx, topicName) {
			return false
		}
	}
	return true
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

func (c *cluster) getConsumerOffset(t *testing.T, ctx context.Context, name, consumer string) int64 {
	t.Helper()

	client, err := kgo.NewClient(c.kafkaClient.Opts()...)
	require.NoError(t, err)
	defer client.Close()

	adm := kadm.NewClient(client)

	lags, err := adm.Lag(ctx, consumer)
	require.NoError(t, err)
	require.NoError(t, lags.Error())

	for _, lag := range lags.Sorted() {
		if lag.Group == consumer {
			for _, l := range lag.Lag.Sorted() {
				if l.Topic == name {
					// assume we have one partition and one consumer
					return l.Commit.At
				}
			}
		}
	}

	t.Errorf("unable to find consumer lag for topic %q and group %q on cluster %q", name, consumer, c.name)

	// unreachable
	return 0
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
