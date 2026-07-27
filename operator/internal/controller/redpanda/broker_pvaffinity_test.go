// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// pvAffinityFixture builds the mis-pin shape: a broker pod unschedulable on
// volume affinity past the remediation timeout, its datadir claim bound
// (full ClaimRef back-reference) to a HostPath PV pinned to a node occupied
// by a live pod matching the victim's own required anti-affinity term.
func pvAffinityFixture() (*redpandav1alpha2.Broker, *corev1.Pod, []client.Object) {
	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "ns"},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(1)),
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp-1",
			Namespace: "ns",
			UID:       types.UID("pod-uid-1"),
			Labels:    map[string]string{"app": "redpanda"},
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
						LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "redpanda"}},
						TopologyKey:   corev1.LabelHostname,
					}},
				},
			},
			Volumes: []corev1.Volume{{
				Name: "datadir",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-rp-1"},
				},
			}},
			Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{{
				Type:               corev1.PodScheduled,
				Status:             corev1.ConditionFalse,
				Reason:             "Unschedulable",
				Message:            "0/3 nodes are available: 3 node(s) had volume node affinity conflict.",
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			}},
		},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "datadir-rp-1", Namespace: "ns", UID: types.UID("pvc-uid-1")},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef: &corev1.ObjectReference{Namespace: "ns", Name: "datadir-rp-1", UID: types.UID("pvc-uid-1")},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/data"},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      corev1.LabelHostname,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"node-a"},
						}},
					}},
				},
			},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "node-a",
		Labels: map[string]string{corev1.LabelHostname: "node-a"},
	}}
	occupant := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-0", Namespace: "ns", Labels: map[string]string{"app": "redpanda"}},
		Spec:       corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	return broker, pod, []client.Object{pod, pvc, pv, node, occupant}
}

func pvAffinityScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, redpandav1alpha2.Install(s))
	return s
}

// TestRemediatePVAffinityMispin covers the live-node proof: the pod's only
// eligible node is occupied by an anti-affinity-conflicting pod, so the
// mis-pinned claim and the pod are deleted and the PV is Retain-patched.
func TestRemediatePVAffinityMispin(t *testing.T) {
	ctx := context.Background()
	broker, pod, objs := pvAffinityFixture()
	c := fake.NewClientBuilder().WithScheme(pvAffinityScheme(t)).WithObjects(objs...).Build()

	r := &BrokerReconciler{UnbindPVCsAfter: time.Minute}
	remediated, err := r.remediatePVAffinity(ctx, logr.Discard(), c, c, broker, pod)
	require.NoError(t, err)
	require.True(t, remediated)

	var pvc corev1.PersistentVolumeClaim
	require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: "datadir-rp-1", Namespace: "ns"}, &pvc)))
	var gone corev1.Pod
	require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &gone)))
	var pv corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "pv-1"}, &pv))
	require.Equal(t, corev1.PersistentVolumeReclaimRetain, pv.Spec.PersistentVolumeReclaimPolicy)
}

// TestRemediatePVAffinityTOCTOU proves the uncached re-read gate: when the
// authoritative view of the pod no longer shows the volume-affinity
// scheduling failure (or shows a different pod under the same name),
// nothing is deleted even though the caller's cached copy still qualifies.
func TestRemediatePVAffinityTOCTOU(t *testing.T) {
	ctx := context.Background()
	broker, pod, objs := pvAffinityFixture()

	// The "API server" view: same pod, but scheduled — since-resolved.
	resolved := pod.DeepCopy()
	resolved.Status.Conditions = []corev1.PodCondition{{
		Type:   corev1.PodScheduled,
		Status: corev1.ConditionTrue,
	}}
	objs[0] = resolved
	c := fake.NewClientBuilder().WithScheme(pvAffinityScheme(t)).WithObjects(objs...).Build()

	r := &BrokerReconciler{UnbindPVCsAfter: time.Minute}
	// `pod` is the stale informer copy that still looks unschedulable.
	remediated, err := r.remediatePVAffinity(ctx, logr.Discard(), c, c, broker, pod)
	require.NoError(t, err)
	require.False(t, remediated)

	var pvc corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "datadir-rp-1", Namespace: "ns"}, &pvc))
	var alive corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &alive))
	var pv corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "pv-1"}, &pv))
	require.Equal(t, corev1.PersistentVolumeReclaimDelete, pv.Spec.PersistentVolumeReclaimPolicy,
		"a disqualified pod must not Retain-patch PVs")
}
