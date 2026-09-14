// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestFindUnboundVolumeClaims pins the orphan classification to the pods a
// StatefulSet owns rather than to its current pod template. Every pod below
// carries the labels of a previous template revision, as every not-yet-rolled
// pod does during an OnDelete rolling restart, so each case regresses if pods
// are ever matched through spec.template.labels again.
func TestFindUnboundVolumeClaims(t *testing.T) {
	const sts = "redpanda"

	previous := podLabels(sts, "redpanda-5.9.0")
	current := podLabels(sts, "redpanda-5.9.1")

	for _, tc := range []struct {
		name    string
		set     *appsv1.StatefulSet
		objs    []client.Object
		unbound []string
	}{
		{
			name: "rolling restart: pods still on the previous template labels",
			set:  statefulSet(sts, 3, current),
			objs: []client.Object{
				brokerPod(sts, 0, previous), brokerPod(sts, 1, previous), brokerPod(sts, 2, previous),
				datadirClaim(sts, 0), datadirClaim(sts, 1), datadirClaim(sts, 2),
			},
		},
		{
			name: "scale down: last pod still present on the previous template labels",
			set:  statefulSet(sts, 2, current),
			objs: []client.Object{
				brokerPod(sts, 0, previous), brokerPod(sts, 1, previous), brokerPod(sts, 2, previous),
				datadirClaim(sts, 0), datadirClaim(sts, 1), datadirClaim(sts, 2),
			},
		},
		{
			name: "scale down: last pod gone",
			set:  statefulSet(sts, 2, current),
			objs: []client.Object{
				brokerPod(sts, 0, previous), brokerPod(sts, 1, previous),
				datadirClaim(sts, 0), datadirClaim(sts, 1), datadirClaim(sts, 2),
			},
			unbound: []string{"datadir-redpanda-2"},
		},
		{
			name: "claim already being deleted is skipped",
			set:  statefulSet(sts, 2, current),
			objs: []client.Object{
				brokerPod(sts, 0, previous), brokerPod(sts, 1, previous),
				datadirClaim(sts, 0), datadirClaim(sts, 1), deleting(datadirClaim(sts, 2)),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(scheme))

			d := &StatefulSetDecomissioner{
				client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).Build(),
			}

			claims, err := d.findUnboundVolumeClaims(context.Background(), tc.set)
			require.NoError(t, err)

			var names []string
			for _, claim := range claims {
				names = append(names, claim.Name)
			}
			require.Equal(t, tc.unbound, names)
		})
	}
}

// podLabels mirrors the chart's pod template labels: the selector plus a label
// that changes on every upgrade.
func podLabels(sts, chart string) map[string]string {
	labels := selectorLabels(sts)
	labels["helm.sh/chart"] = chart
	return labels
}

// selectorLabels mirrors the chart's immutable StatefulSet selector.
func selectorLabels(sts string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "redpanda",
		"app.kubernetes.io/instance":  "redpanda",
		"app.kubernetes.io/component": sts + "-statefulset",
	}
}

func statefulSet(name string, replicas int32, templateLabels map[string]string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels(name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: templateLabels},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name: datadirVolume,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "redpanda",
						"app.kubernetes.io/instance":  "redpanda",
						"app.kubernetes.io/component": "redpanda",
					},
				},
			}},
		},
	}
}

func brokerPod(sts string, ordinal int, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", sts, ordinal), Namespace: "default", Labels: labels},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{{
				Name: datadirVolume,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName(sts, ordinal)},
				},
			}},
		},
	}
}

// datadirClaim mirrors the StatefulSet controller, which labels a claim with
// the volume claim template's labels overlaid by the selector's.
func datadirClaim(sts string, ordinal int) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claimName(sts, ordinal), Namespace: "default", Labels: selectorLabels(sts)},
	}
}

func claimName(sts string, ordinal int) string {
	return fmt.Sprintf("%s-%s-%d", datadirVolume, sts, ordinal)
}

func deleting(claim *corev1.PersistentVolumeClaim) *corev1.PersistentVolumeClaim {
	claim.DeletionTimestamp = ptr.To(metav1.Now())
	// The fake client refuses to create a deleting object that has no finalizers.
	claim.Finalizers = []string{"kubernetes.io/pvc-protection"}
	return claim
}
