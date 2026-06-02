// Copyright 2026 Redpanda Data, Inc.
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
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func pipelineIsSuccessfullyRunning(ctx context.Context, t framework.TestingT, name string) {
	var pipeline redpandav1alpha2.Pipeline
	require.NoError(t, t.Get(ctx, t.ResourceKey(name), &pipeline))

	waitForCondition(ctx, t, &pipeline, metav1.Condition{
		Type:   redpandav1alpha2.PipelineConditionReady,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.PipelineReasonRunning,
	}, func() []metav1.Condition {
		return pipeline.Status.Conditions
	})

	require.Equal(t, redpandav1alpha2.PipelinePhaseRunning, pipeline.Status.Phase)
}

func pipelineIsStopped(ctx context.Context, t framework.TestingT, name string) {
	var pipeline redpandav1alpha2.Pipeline
	require.NoError(t, t.Get(ctx, t.ResourceKey(name), &pipeline))

	waitForCondition(ctx, t, &pipeline, metav1.Condition{
		Type:   redpandav1alpha2.PipelineConditionReady,
		Status: metav1.ConditionTrue,
		Reason: redpandav1alpha2.PipelineReasonPaused,
	}, func() []metav1.Condition {
		return pipeline.Status.Conditions
	})

	require.Equal(t, redpandav1alpha2.PipelinePhaseStopped, pipeline.Status.Phase)
}

func iDeleteTheCRDPipeline(ctx context.Context, t framework.TestingT, name string) {
	var pipeline redpandav1alpha2.Pipeline

	t.Logf("Deleting pipeline %q", name)
	err := t.Get(ctx, t.ResourceKey(name), &pipeline)
	if err != nil {
		if apierrors.IsNotFound(err) {
			t.Logf("Pipeline %q already deleted", name)
			return
		}
		t.Fatalf("Error getting pipeline %q for deletion: %v", name, err)
	}

	t.Logf("Found pipeline %q, deleting it", name)
	require.NoError(t, t.Delete(ctx, &pipeline))
	t.Logf("Successfully deleted pipeline %q CRD", name)
}

func pipelineDoesNotExist(ctx context.Context, t framework.TestingT, name string) {
	var pipeline redpandav1alpha2.Pipeline
	require.Eventually(t, func() bool {
		err := t.Get(ctx, t.ResourceKey(name), &pipeline)
		return apierrors.IsNotFound(err)
	}, 2*time.Minute, 2*time.Second, "Pipeline %q should not exist", name)
}

func pipelineHasInvalidConfig(ctx context.Context, t framework.TestingT, name string) {
	var pipeline redpandav1alpha2.Pipeline
	require.NoError(t, t.Get(ctx, t.ResourceKey(name), &pipeline))

	waitForCondition(ctx, t, &pipeline, metav1.Condition{
		Type:   redpandav1alpha2.PipelineConditionConfigValid,
		Status: metav1.ConditionFalse,
		Reason: redpandav1alpha2.PipelineReasonConfigInvalid,
	}, func() []metav1.Condition {
		return pipeline.Status.Conditions
	})
}

func topicHasMessagesInCluster(ctx context.Context, t framework.TestingT, topic, cluster string) {
	clients := clientsForCluster(ctx, cluster)
	clients.ExpectTopic(ctx, topic)

	kafkaClient := clients.Kafka(ctx)
	defer kafkaClient.Close()

	consumerClient, err := kgo.NewClient(append(kafkaClient.Opts(),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)...)
	require.NoError(t, err)
	defer consumerClient.Close()

	t.Logf("Polling records from topic %q in cluster %q", topic, cluster)
	require.Eventually(t, func() bool {
		fetches := consumerClient.PollRecords(ctx, 1)
		return len(fetches.Records()) > 0
	}, 2*time.Minute, 2*time.Second, "Topic %q in cluster %q should have messages", topic, cluster)
	t.Logf("Found messages in topic %q", topic)
}

func iProduceMessagesToTopicInCluster(ctx context.Context, t framework.TestingT, topic, cluster string) {
	clients := clientsForCluster(ctx, cluster)
	clients.ExpectTopic(ctx, topic)

	kafkaClient := clients.Kafka(ctx)
	defer kafkaClient.Close()

	t.Logf("Producing test messages to topic %q in cluster %q", topic, cluster)
	for i := range 5 {
		require.NoError(t, kafkaClient.ProduceSync(ctx, &kgo.Record{
			Topic: topic,
			Value: []byte("test-message-" + string(rune('0'+i))),
		}).FirstErr())
	}
	t.Logf("Produced 5 messages to topic %q", topic)
}
