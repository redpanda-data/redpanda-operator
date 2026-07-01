// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func (b *Broker) PodName() string {
	if b.Spec.ClusterRef.IsNodePool() {
		// TODO: fetch nodepool, then grab the cluster name from nodepool spec
		return fmt.Sprintf("%s-%s", b.Spec.ClusterRef.Name, ptr.Deref(b.Spec.ClusterRef.Namespace, ""))
	}
	return fmt.Sprintf("%s-%d", b.Spec.ClusterRef.Name, ptr.Deref(b.Spec.NetworkIndex, 0))
}

func (b *Broker) BuildPod(podName string) *corev1.Pod {
	pod := &corev1.Pod{
		Spec: b.Spec.PodTemplate.Spec,
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   b.Namespace,
			Annotations: b.Spec.PodTemplate.Annotations,
			Labels:      b.Spec.PodTemplate.Labels,
		},
	}
	return pod
}
