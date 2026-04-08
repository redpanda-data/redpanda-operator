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

	"github.com/stretchr/testify/require"
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
