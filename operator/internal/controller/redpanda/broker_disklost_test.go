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
	"strings"
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
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/pvcunbinder"
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

// wipedDiskFixture builds the live-node shape: the dead-node fixture's
// broker and claim, but the pod is scheduled on a node that EXISTS, the
// claim's PV is a Local volume pinned to that node, and the kubelet's
// FailedMount event says the PV's directory does not exist — first seen
// past the detection timeout, refreshed just now.
func wipedDiskFixture() (*redpandav1alpha2.Broker, *corev1.Pod, *corev1.Event, []client.Object) {
	broker, pod, objs := diskLostFixture()
	pod.Spec.NodeName = "node-alive"
	pod.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
		{Type: corev1.PodReadyToStartContainers, Status: corev1.ConditionFalse},
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  "redpanda",
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
	}}
	const path = "/var/lib/rancher/k3s/storage/pv-1_ns_datadir-rp-1"
	pv := objs[2].(*corev1.PersistentVolume)
	pv.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{Local: &corev1.LocalVolumeSource{Path: path}}
	pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"node-alive"}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "node-alive",
		Labels: map[string]string{corev1.LabelHostname: "node-alive"},
	}}
	event := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "rp-1.18de0808fe711e03", Namespace: "ns"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "ns", Name: "rp-1", UID: pod.UID},
		Type:           corev1.EventTypeWarning,
		Reason:         "FailedMount",
		Message:        `MountVolume.NewMounter initialization failed for volume "pv-1" : path "` + path + `" does not exist`,
		Source:         corev1.EventSource{Component: "kubelet", Host: "node-alive"},
		FirstTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		LastTimestamp:  metav1.Now(),
		Count:          7,
	}
	return broker, pod, event, append(objs, node, event)
}

func runDiskLost(t *testing.T, broker *redpandav1alpha2.Broker, objs []client.Object) (client.Client, *brokerReconciliationState, error) {
	c := fake.NewClientBuilder().WithScheme(diskLostScheme(t)).
		WithObjects(objs...).WithStatusSubresource(&redpandav1alpha2.Broker{}).
		WithIndex(&corev1.Event{}, "involvedObject.uid", func(o client.Object) []string {
			return []string{string(o.(*corev1.Event).InvolvedObject.UID)}
		}).Build()
	r := &BrokerReconciler{MarkDiskLostAfter: time.Minute}
	state, err := r.fetchState(t.Context(), mcreconcile.Request{}, c, broker)
	require.NoError(t, err)
	_, err = r.reconcileDiskLost(context.Background(), state, &diskLostCluster{client: c})
	return c, state, err
}

// TestDetectDiskLostMarksDeadNode covers the positive proof: dead node
// (no Node object carries the PV's pinned hostname), timeout elapsed,
// broker-owned pod. The marking pass sets the latch and phase but deletes
// NOTHING — dismantle acts only on the persisted latch — and the recorded
// BrokerID is untouched.
func TestDetectDiskLostMarksDeadNode(t *testing.T) {
	broker, _, objs := diskLostFixture()
	c, state, err := runDiskLost(t, broker, objs)
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
			_, _, err := runDiskLost(t, broker, objs)
			require.NoError(t, err)
			require.Nil(t, broker.Status.DiskLost, "the latch must not be set")
		})
	}
}

// TestDetectDiskLostMarksWipedDisk covers the live-node proof: the node
// exists (so there is no dead-node proof), the pod is scheduled, and the
// kubelet has been reporting the Local PV's directory gone for longer than
// the timeout. Like the dead-node pass it sets the latch and phase, deletes
// nothing, and leaves the recorded BrokerID alone.
func TestDetectDiskLostMarksWipedDisk(t *testing.T) {
	broker, _, _, objs := wipedDiskFixture()
	c, state, err := runDiskLost(t, broker, objs)
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

// TestDetectWipedDiskNegatives: every gate of the live-node proof that must
// refuse the terminal marking. Only a proof still inside the timeout parks
// the broker in Stuck; every other refusal leaves the phase to defaultPhase.
// A scheduled pod with no sandbox and no verdict yet asks for the mount
// poll — the verdict is an event, which fires no watch — without blocking
// the chain; one whose sandbox is up does not.
func TestDetectWipedDiskNegatives(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate    func(broker *redpandav1alpha2.Broker, pod *corev1.Pod, event *corev1.Event)
		wantPhase redpandav1alpha2.BrokerPhase
		wantPoll  bool
	}{
		"timeout not elapsed": {
			mutate: func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, event *corev1.Event) {
				event.FirstTimestamp = metav1.Now()
			},
			wantPhase: redpandav1alpha2.BrokerPhaseStuck,
		},
		"no verdict yet, sandbox down": {
			mutate: func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, event *corev1.Event) {
				event.Reason = "Scheduled"
				event.Message = "Successfully assigned ns/rp-1 to node-alive"
			},
			wantPoll: true,
		},
		"no verdict, sandbox up": {
			mutate: func(_ *redpandav1alpha2.Broker, pod *corev1.Pod, event *corev1.Event) {
				event.Reason = "Scheduled"
				event.Message = "Successfully assigned ns/rp-1 to node-alive"
				pod.Status.Conditions[1].Status = corev1.ConditionTrue
			},
		},
		"event stale (mount recovered, sandbox torn down later)": {
			mutate: func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, event *corev1.Event) {
				event.FirstTimestamp = metav1.NewTime(time.Now().Add(-30 * time.Minute))
				event.LastTimestamp = metav1.NewTime(time.Now().Add(-pvcunbinder.FailedMountEventStaleAfter - time.Minute))
			},
			wantPoll: true,
		},
		"event names a volume that is not the broker's": {
			mutate: func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, event *corev1.Event) {
				event.Message = strings.Replace(event.Message, `"pv-1"`, `"pv-other"`, 1)
			},
			wantPoll: true,
		},
		"a different FailedMount": {
			mutate: func(_ *redpandav1alpha2.Broker, _ *corev1.Pod, event *corev1.Event) {
				event.Message = `MountVolume.SetUp failed for volume "config" : configmap "rp" not found`
			},
			wantPoll: true,
		},
		"sandbox is up (volumes mounted)": {
			mutate: func(_ *redpandav1alpha2.Broker, pod *corev1.Pod, _ *corev1.Event) {
				pod.Status.Conditions[1].Status = corev1.ConditionTrue
			},
		},
		"pod not controlled by the broker (migration shadow)": {
			mutate: func(_ *redpandav1alpha2.Broker, pod *corev1.Pod, _ *corev1.Event) {
				pod.OwnerReferences[0].Kind = "StatefulSet"
				pod.OwnerReferences[0].APIVersion = "apps/v1"
				pod.OwnerReferences[0].UID = types.UID("sts-uid")
			},
		},
		"decommissioning broker": {
			mutate: func(broker *redpandav1alpha2.Broker, _ *corev1.Pod, _ *corev1.Event) {
				broker.Spec.Decommission = true
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker, pod, event, objs := wipedDiskFixture()
			tc.mutate(broker, pod, event)
			_, state, err := runDiskLost(t, broker, objs)
			require.NoError(t, err)
			require.Nil(t, broker.Status.DiskLost, "the latch must not be set")
			require.Equal(t, tc.wantPhase, state.phase)
			var wantRequeue time.Duration
			if tc.wantPoll {
				wantRequeue = requeueMountPoll
			}
			require.Equal(t, wantRequeue, state.requeueAfter)
		})
	}
}

// TestDismantleDiskLost covers the dismantle pass: pod and every claim
// (ExistingClaims included — the migrated-broker shape) are deleted, and
// ResourcesReleased is set once the reader confirms them gone.
func TestDismantleDiskLost(t *testing.T) {
	broker, _, objs := diskLostFixture()
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now()}

	c, state, err := runDiskLost(t, broker, objs)
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

// TestDiskLostChainGating: a marked tombstone without decommission intent
// short-circuits the chain pass after pass — the pod is never recreated.
func TestDiskLostChainGating(t *testing.T) {
	broker, _, objs := diskLostFixture()
	broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true}

	// No pod, no PVC in the world; two consecutive passes must not create
	// anything.
	for range 2 {
		c, state, err := runDiskLost(t, broker, objs[2:]) // PV only
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
		r := &BrokerReconciler{MarkDiskLostAfter: time.Minute}
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
		r := &BrokerReconciler{MarkDiskLostAfter: time.Minute}
		state := &brokerReconciliationState{broker: broker, initialStatus: broker.Status.DeepCopy()}
		result, err := r.reconcileDiskLost(context.Background(), state, &diskLostCluster{client: c})
		require.NoError(t, err)
		require.False(t, result.IsZero())
		require.Equal(t, redpandav1alpha2.BrokerPhaseDecommissioned, state.phase)
	})
}
