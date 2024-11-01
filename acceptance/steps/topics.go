// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func topicIsSuccessfullySynced(ctx context.Context, t framework.TestingT, topic string) {
	var topicObject redpandav1alpha2.Topic
	require.NoError(t, t.Get(ctx, t.ResourceKey(topic), &topicObject))

	// make sure the resource is stable
	checkStableResource(ctx, t, &topicObject)

	// make sure it's synchronized
	t.RequireCondition(metav1.Condition{
		Type:   redpandav1alpha2.ReadyCondition,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.SucceededReason,
	}, topicObject.Status.Conditions)
}

func thereIsNoTopic(ctx context.Context, topic, cluster string) {
	clientsForCluster(ctx, cluster).ExpectNoTopic(ctx, topic)
}

func iShouldBeAbleToProduceAndConsumeFrom(ctx context.Context, t framework.TestingT, topic, cluster string) {
	payload := []byte("test")

	clients := clientsForCluster(ctx, cluster)
	clients.ExpectTopic(ctx, topic)

	kafkaClient := clients.Kafka(ctx)

	// send a record
	t.Logf("Producing record for topic %q", topic)
	require.NoError(t, kafkaClient.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: payload}).FirstErr())
	t.Logf("Wrote record to topic %q", topic)

	// receive a record
	consumerClient, err := kgo.NewClient(append(kafkaClient.Opts(),
		kgo.ConsumerGroup("test"),
		kgo.ConsumeTopics(topic),
	)...)
	require.NoError(t, err)

	t.Logf("Polling records from topic %q", topic)
	fetches := consumerClient.PollFetches(ctx)
	t.Logf("Polled records from topic %q", topic)
	require.NoError(t, fetches.Err())
	records := fetches.Records()
	require.Len(t, records, 1)
	require.Equal(t, string(payload), string(records[0].Value))
	kafkaClient.Close()
	consumerClient.Close()
}
