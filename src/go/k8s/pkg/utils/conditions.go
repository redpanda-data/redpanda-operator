package utils

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

// StatusConditionConfigs takes a set of existing conditions and conditions of a
// desired state and merges them idempotently to create a series of ConditionApplyConfiguration
// values that can be used in a server-side apply.
func StatusConditionConfigs(existing []metav1.Condition, generation int64, conditions []metav1.Condition) []*metav1ac.ConditionApplyConfiguration {
	now := metav1.Now()
	configurations := []*metav1ac.ConditionApplyConfiguration{}

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

func conditionToConfig(generation int64, now metav1.Time, condition metav1.Condition) *metav1ac.ConditionApplyConfiguration { //nolint:gocritic // passing a Condition without a pointer reference is fine here
	return metav1ac.Condition().
		WithType(condition.Type).
		WithStatus(condition.Status).
		WithReason(condition.Reason).
		WithObservedGeneration(generation).
		WithLastTransitionTime(now).
		WithMessage(condition.Message)
}
