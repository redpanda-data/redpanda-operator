// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pod

// Some functions in this file Copyright 2018 The Kubernetes Authors under the Apache License, Version 2.0.
// see: https://github.com/kubernetes/kubernetes/blob/49ffd6192f1608e0a2980548390b2c5431d6af8a/pkg/scheduler/util/utils.go#L103-L134
// and: https://github.com/kubernetes/kubernetes/blob/49ffd6192f1608e0a2980548390b2c5431d6af8a/pkg/api/v1/pod/util.go#L347-L368

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
)

// PatchPodStatus patches pod status. It returns true and avoids an update if the patch contains no changes.
func PatchPodStatus(ctx context.Context, c clientset.Interface, namespace, name string, uid types.UID, oldPodStatus, newPodStatus v1.PodStatus) (*v1.Pod, []byte, bool, error) {
	patchBytes, unchanged, err := preparePatchBytesForPodStatus(namespace, name, uid, oldPodStatus, newPodStatus)
	if err != nil {
		return nil, nil, false, err
	}
	if unchanged {
		return nil, patchBytes, true, nil
	}

	updatedPod, err := c.CoreV1().Pods(namespace).Patch(ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to patch status %q for pod %q/%q: %v", patchBytes, namespace, name, err)
	}
	return updatedPod, patchBytes, false, nil
}

func preparePatchBytesForPodStatus(namespace, name string, uid types.UID, oldPodStatus, newPodStatus v1.PodStatus) ([]byte, bool, error) {
	oldData, err := json.Marshal(v1.Pod{
		Status: oldPodStatus,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to Marshal oldData for pod %q/%q: %v", namespace, name, err)
	}

	newData, err := json.Marshal(v1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: uid}, // only put the uid in the new object to ensure it appears in the patch as a precondition
		Status:     newPodStatus,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to Marshal newData for pod %q/%q: %v", namespace, name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Pod{})
	if err != nil {
		return nil, false, fmt.Errorf("failed to CreateTwoWayMergePatch for pod %q/%q: %v", namespace, name, err)
	}
	return patchBytes, bytes.Equal(patchBytes, []byte(fmt.Sprintf(`{"metadata":{"uid":%q}}`, uid))), nil
}

// ReplaceOrAppendPodCondition replaces the first pod condition with equal type or appends if there is none
func ReplaceOrAppendPodCondition(conditions []v1.PodCondition, condition *v1.PodCondition) []v1.PodCondition {
	if i, _ := GetPodConditionFromList(conditions, condition.Type); i >= 0 {
		conditions[i] = *condition
	} else {
		conditions = append(conditions, *condition)
	}
	return conditions
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []v1.PodCondition, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}
