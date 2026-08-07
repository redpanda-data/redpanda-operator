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
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// diskLostCluster is a minimal cluster.Cluster for exercising the DiskLost
// reconcilers: the fake client doubles as the uncached reader.
type diskLostCluster struct {
	cluster.Cluster
	client client.Client
}

func (c *diskLostCluster) GetClient() client.Client    { return c.client }
func (c *diskLostCluster) GetAPIReader() client.Reader { return c.client }

func diskLostScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, redpandav1alpha2.Install(s))
	return s
}

// diskLostFixture builds the dead-node shape: a broker-owned pod
// unschedulable on volume affinity past the detection timeout, its datadir
// claim bound to a HostPath PV pinned to a node that has NO Node object.
// The returned objects deliberately omit any Node.
func diskLostFixture() (*redpandav1alpha2.Broker, *corev1.Pod, []client.Object) {
	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-1", Namespace: "ns", UID: types.UID("broker-uid-1")},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(1)),
			Storage: redpandav1alpha2.BrokerStorage{
				ExistingClaims: []redpandav1alpha2.ExistingClaim{{Name: "datadir-rp-1", MountPath: "/data"}},
			},
		},
		Status: redpandav1alpha2.BrokerStatus{BrokerID: ptr.To(int32(1))},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp-1",
			Namespace: "ns",
			UID:       types.UID("pod-uid-1"),
			Labels:    map[string]string{"app": "redpanda"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: redpandav1alpha2.GroupVersion.String(),
				Kind:       "Broker",
				Name:       "rp-1",
				UID:        types.UID("broker-uid-1"),
				Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PodSpec{
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "datadir-rp-1",
			Namespace: "ns",
			UID:       types.UID("pvc-uid-1"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: redpandav1alpha2.GroupVersion.String(),
				Kind:       "Broker",
				Name:       "rp-1",
				UID:        types.UID("broker-uid-1"),
				Controller: ptr.To(true),
			}},
		},
		Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1"},
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
							Values:   []string{"node-gone"},
						}},
					}},
				},
			},
		},
	}
	return broker, pod, []client.Object{pod, pvc, pv}
}

func runDiskLost(t *testing.T, broker *redpandav1alpha2.Broker, pod *corev1.Pod, objs []client.Object) (client.Client, *brokerReconciliationState, error) {
	c := fake.NewClientBuilder().WithScheme(diskLostScheme(t)).
		WithObjects(objs...).WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
	r := &BrokerReconciler{UnbindPVCsAfter: time.Minute}
	state := &brokerReconciliationState{
		broker:        broker,
		pod:           pod,
		initialStatus: broker.Status.DeepCopy(),
	}
	_, err := r.reconcileDiskLost(context.Background(), state, &diskLostCluster{client: c})
	return c, state, err
}

// TestDetectDiskLostMarksDeadNode covers the positive proof: dead node
// (no Node object carries the PV's pinned hostname), timeout elapsed,
// broker-owned pod. The marking pass sets the latch and phase but deletes
// NOTHING — dismantle acts only on the persisted latch — and the recorded
// BrokerID is untouched.
func TestDetectDiskLostMarksDeadNode(t *testing.T) {
	broker, pod, objs := diskLostFixture()
	c, state, err := runDiskLost(t, broker, pod, objs)
	require.NoError(t, err)

	require.NotNil(t, broker.Status.DiskLost, "the latch must be set")
	require.False(t, broker.Status.DiskLost.ResourcesReleased)
	require.Equal(t, redpandav1alpha2.BrokerPhaseDiskLost, state.phase)
	require.Equal(t, ptr.To(int32(1)), broker.Status.BrokerID, "identity is immutable")

	ctx := context.Background()
	var alive corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &alive), "nothing is deleted on the marking pass")
	var pvc corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "datadir-rp-1", Namespace: "ns"}, &pvc), "nothing is deleted on the marking pass")
}

// TestDetectDiskLostNegatives: every gate that must refuse the terminal
// marking.
func TestDetectDiskLostNegatives(t *testing.T) {
	for name, mutate := range map[string]func(broker *redpandav1alpha2.Broker, pod *corev1.Pod, objs []client.Object) []client.Object{
		"timeout not elapsed": func(_ *redpandav1alpha2.Broker, pod *corev1.Pod, objs []client.Object) []client.Object {
			pod.Status.Conditions[0].LastTransitionTime = metav1.Now()
			return objs
		},
		"decommissioning broker": func(broker *redpandav1alpha2.Broker, _ *corev1.Pod, objs []client.Object) []client.Object {
			broker.Spec.Decommission = true
			return objs
		},
		"pod not controlled by the broker (migration shadow)": func(_ *redpandav1alpha2.Broker, pod *corev1.Pod, objs []client.Object) []client.Object {
			pod.OwnerReferences[0].Kind = "StatefulSet"
			pod.OwnerReferences[0].APIVersion = "apps/v1"
			pod.OwnerReferences[0].UID = types.UID("sts-uid")
			return objs
		},
		"node alive (mis-pin stays Stuck)": func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, objs []client.Object) []client.Object {
			return append(objs, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
				Name:   "node-gone",
				Labels: map[string]string{corev1.LabelHostname: "node-gone"},
			}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker, pod, objs := diskLostFixture()
			objs = mutate(broker, pod, objs)
			_, _, err := runDiskLost(t, broker, pod, objs)
			require.NoError(t, err)
			require.Nil(t, broker.Status.DiskLost, "the latch must not be set")
		})
	}
}

// TestDetectDiskLostTOCTOU proves the uncached re-read gate: when the
// authoritative view of the pod no longer shows the volume-affinity
// scheduling failure, the marking is refused even though the caller's
// cached copy still qualifies.
func TestDetectDiskLostTOCTOU(t *testing.T) {
	broker, pod, objs := diskLostFixture()

	// The "API server" view: same pod, but scheduled — since-resolved.
	resolved := pod.DeepCopy()
	resolved.Status.Conditions = []corev1.PodCondition{{
		Type:   corev1.PodScheduled,
		Status: corev1.ConditionTrue,
	}}
	objs[0] = resolved

	// `pod` is the stale informer copy that still looks unschedulable.
	_, _, err := runDiskLost(t, broker, pod, objs)
	require.NoError(t, err)
	require.Nil(t, broker.Status.DiskLost)
}

// TestDismantleDiskLost covers the dismantle pass: pod and every claim
// (ExistingClaims included — the migrated-broker shape) are deleted, and
// ResourcesReleased is set once the uncached reader confirms them gone.
func TestDismantleDiskLost(t *testing.T) {
	broker, pod, objs := diskLostFixture()
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}

	c, state, err := runDiskLost(t, broker, pod, objs)
	require.NoError(t, err)
	require.Equal(t, redpandav1alpha2.BrokerPhaseDiskLost, state.phase)
	require.True(t, broker.Status.DiskLost.ResourcesReleased)

	ctx := context.Background()
	var gone corev1.Pod
	require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &gone)))
	var pvc corev1.PersistentVolumeClaim
	require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Name: "datadir-rp-1", Namespace: "ns"}, &pvc)))
	var pv corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "pv-1"}, &pv))
	require.Equal(t, corev1.PersistentVolumeReclaimDelete, pv.Spec.PersistentVolumeReclaimPolicy,
		"dismantle must not touch reclaim policies")
}

// TestDismantleDiskLostSparesForeignResources: resources answering to the
// tombstone's names but controlled by ANOTHER owner (the replacement CR, after
// index takeover) are left alone, and the tombstone still releases.
func TestDismantleDiskLostSparesForeignResources(t *testing.T) {
	broker, pod, objs := diskLostFixture()
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}

	// Rewrite pod and PVC as replacement-owned.
	for _, ref := range []*[]metav1.OwnerReference{&pod.OwnerReferences, &objs[1].(*corev1.PersistentVolumeClaim).OwnerReferences} {
		(*ref)[0].Name = "rp-1-replacement"
		(*ref)[0].UID = types.UID("replacement-uid")
	}

	// fetchState would have nulled a foreign pod out; mirror that here.
	c, _, err := runDiskLost(t, broker, nil, objs)
	require.NoError(t, err)
	require.True(t, broker.Status.DiskLost.ResourcesReleased,
		"foreign-owned resources count as released for the tombstone")

	ctx := context.Background()
	var alive corev1.Pod
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &alive), "the replacement's pod must survive")
	var pvc corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "datadir-rp-1", Namespace: "ns"}, &pvc), "the replacement's PVC must survive")
}

// TestDiskLostChainGating: a marked tombstone without decommission intent
// short-circuits the chain pass after pass — the pod is never recreated.
func TestDiskLostChainGating(t *testing.T) {
	broker, _, objs := diskLostFixture()
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}

	// No pod, no PVC in the world; two consecutive passes must not create
	// anything.
	for range 2 {
		c, state, err := runDiskLost(t, broker, nil, objs[2:]) // PV only
		require.NoError(t, err)
		require.Equal(t, redpandav1alpha2.BrokerPhaseDiskLost, state.phase)
		var pod corev1.Pod
		require.True(t, apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "rp-1", Namespace: "ns"}, &pod)))
	}
}

// TestDiskLostTombstoneDecommission: with decommission intent set by the
// engine, a tombstone carrying a recorded id falls through to the ordinary
// decommission machinery; one without a recorded id short-circuits to
// Decommissioned (never resolve-by-name — it would hit the replacement).
func TestDiskLostTombstoneDecommission(t *testing.T) {
	t.Run("recorded id falls through", func(t *testing.T) {
		broker, _, objs := diskLostFixture()
		broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
		broker.Spec.Decommission = true

		c := fake.NewClientBuilder().WithScheme(diskLostScheme(t)).
			WithObjects(objs[2:]...).WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
		r := &BrokerReconciler{UnbindPVCsAfter: time.Minute}
		state := &brokerReconciliationState{broker: broker, initialStatus: broker.Status.DeepCopy()}
		result, err := r.reconcileDiskLost(context.Background(), state, &diskLostCluster{client: c})
		require.NoError(t, err)
		require.True(t, result.IsZero(), "must fall through to reconcileDecommission")
	})

	t.Run("nil id short-circuits to Decommissioned", func(t *testing.T) {
		broker, _, objs := diskLostFixture()
		broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}
		broker.Status.BrokerID = nil
		broker.Spec.Decommission = true

		c := fake.NewClientBuilder().WithScheme(diskLostScheme(t)).
			WithObjects(objs[2:]...).WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
		r := &BrokerReconciler{UnbindPVCsAfter: time.Minute}
		state := &brokerReconciliationState{broker: broker, initialStatus: broker.Status.DeepCopy()}
		result, err := r.reconcileDiskLost(context.Background(), state, &diskLostCluster{client: c})
		require.NoError(t, err)
		require.False(t, result.IsZero())
		require.Equal(t, redpandav1alpha2.BrokerPhaseDecommissioned, state.phase)
	})
}
