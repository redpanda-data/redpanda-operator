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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// PodName returns the name of the pod this Broker manages. Pods keep their
// StatefulSet ordinal names: `<cluster>-<ordinal>` for the default node pool
// and `<cluster>-<pool>-<ordinal>` for pools declared in the cluster's
// nodePools — matching the per-pool StatefulSet naming. The owning pool is
// read from the Broker's node pool label.
func (b *Broker) PodName() string {
	if b.Spec.ClusterRef.IsNodePool() {
		// for nodepools, ClusterRef.Name is a NodePool name,
		// so we need to rely on the cluster name in the label
		return fmt.Sprintf("%s-%s-%d", b.Labels[ClusterNameLabel], b.Spec.ClusterRef.Name, ptr.Deref(b.Spec.NetworkIndex, 0))
	}
	name := b.Spec.ClusterRef.Name
	if pool := b.Labels[NodePoolLabel]; pool != "" && !strings.EqualFold(pool, DefaultNodePoolName) {
		name = fmt.Sprintf("%s-%s", name, pool)
	}
	return fmt.Sprintf("%s-%d", name, ptr.Deref(b.Spec.NetworkIndex, 0))
}

// PodOutdated reports whether the live pod has drifted from the Broker's
// desired pod template on the keys that demand a rotation: the config
// checksum and the restart-requiring cluster-config version. Pods inherit
// both at creation, so a recreated pod never reports drift.
func (b *Broker) PodOutdated(pod *corev1.Pod) bool {
	for _, key := range []string{BrokerConfigChecksumAnnotation, BrokerClusterConfigVersionAnnotation} {
		if desired := b.Spec.PodTemplate.Annotations[key]; desired != "" && pod.Annotations[key] != desired {
			return true
		}
	}
	return false
}

func (b *Broker) BuildPod(podName string) *corev1.Pod {
	// Deep-copy the template: its PodSpec, labels and annotations otherwise
	// share memory with the Broker object (often an informer-cache copy) —
	// any caller mutating the returned pod would corrupt the CR.
	tpl := b.Spec.PodTemplate.DeepCopy()
	pod := &corev1.Pod{
		Spec: tpl.Spec,
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   b.Namespace,
			Annotations: tpl.Annotations,
			Labels:      tpl.Labels,
		},
	}
	return pod
}
