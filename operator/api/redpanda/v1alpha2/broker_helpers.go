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
	"crypto/sha256"
	"encoding/json"
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

// RotationAnnotations are the pod annotations that carry a Broker pod's
// rotation identity: they mean "what this pod was created from", are stamped
// at pod creation (or backfilled at adoption) and are compared by
// PodOutdated. They are the one set of template annotations that the
// in-place metadata sync must NEVER copy onto a live pod — overwriting them
// with desired values would mark a stale pod current and silently swallow a
// pending rotation. Keep PodOutdated and the metadata sync in lockstep by
// always going through this list.
var RotationAnnotations = []string{
	BrokerPodTemplateHashAnnotation,
	BrokerConfigChecksumAnnotation,
	BrokerClusterConfigVersionAnnotation,
}

// PodOutdated reports whether the live pod has drifted from the Broker's
// desired pod template on the keys that demand a rotation: the pod SPEC hash
// (see BrokerPodTemplateHashAnnotation), the config checksum, and the
// restart-requiring cluster-config version — three orthogonal triggers.
// Template labels and non-rotation annotations are deliberately absent: they
// are mutable on live pods and converge in place without a restart. Pods
// inherit all three keys at creation, so a recreated pod never reports
// drift.
func (b *Broker) PodOutdated(pod *corev1.Pod) bool {
	for _, key := range RotationAnnotations {
		if desired := b.Spec.PodTemplate.Annotations[key]; desired != "" && pod.Annotations[key] != desired {
			return true
		}
	}
	return false
}

// IsDiskLostTicket reports whether this Broker is a dead incarnation: its
// disk was lost with its node and the CR remains only as the decommission
// record for its node_id (see BrokerStatus.DiskLost).
func (b *Broker) IsDiskLostTicket() bool {
	return b.Status.DiskLost != nil
}

// DiskLostReleased reports whether a dead incarnation has released its
// network index: its pod and PVCs are confirmed gone, so a replacement
// Broker may safely be created under the same pod and PVC names.
func (b *Broker) DiskLostReleased() bool {
	return b.Status.DiskLost != nil && b.Status.DiskLost.ResourcesReleased
}

// Hash returns a deterministic hash of the pod template's SPEC — the
// rotation identity. Template labels and annotations are excluded: metadata
// is mutable on live pods and is synced in place by the Broker controller,
// so it must not force a pod recreation. The two metadata keys that DO
// demand a restart (config checksum and restart marker) are compared as
// their own PodOutdated keys instead of feeding the hash.
func (t *BrokerPodTemplate) Hash() string {
	// API structs marshal without error; a deterministic output is
	// guaranteed because encoding/json sorts map keys.
	serialized, err := json.Marshal(t.Spec)
	if err != nil {
		// Unreachable for plain API structs; an empty hash degrades to the
		// pre-hash behavior (PodOutdated skips unset desired keys).
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(serialized))
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
