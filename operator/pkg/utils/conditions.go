// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// StatusConditionConfigs takes a set of existing conditions and conditions of a
// desired state and merges them idempotently to create a series of ConditionApplyConfiguration
// values that can be used in a server-side apply.
func StatusConditionConfigs(existing []metav1.Condition, generation int64, conditions []metav1.Condition) []*applymetav1.ConditionApplyConfiguration {
	now := metav1.Now()
	configurations := []*applymetav1.ConditionApplyConfiguration{}

	for _, condition := range conditions {
		existingCondition := apimeta.FindStatusCondition(existing, condition.Type)
		if existingCondition == nil {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Status != condition.Status {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Reason != condition.Reason {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Message != condition.Message {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.ObservedGeneration != generation {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		configurations = append(configurations, conditionToConfig(generation, existingCondition.LastTransitionTime, *existingCondition))
	}

	return configurations
}

func conditionToConfig(generation int64, now metav1.Time, condition metav1.Condition) *applymetav1.ConditionApplyConfiguration { //nolint:gocritic // passing a Condition without a pointer reference is fine here
	return applymetav1.Condition().
		WithType(condition.Type).
		WithStatus(condition.Status).
		WithReason(condition.Reason).
		WithObservedGeneration(generation).
		WithLastTransitionTime(now).
		WithMessage(condition.Message)
}
