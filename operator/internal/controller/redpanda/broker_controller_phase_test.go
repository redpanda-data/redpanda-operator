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

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// TestDefaultPhase pins the one-way gate out of Provisioning: once a broker
// has registered (Status.BrokerID set), a pod readiness dip must not regress
// its phase — the readiness probe is cluster-scoped, so one restarting
// broker flips every pod unready and would otherwise flap every registered
// broker's phase Running→Provisioning.
func TestDefaultPhase(t *testing.T) {
	registered := &redpandav1alpha2.Broker{}
	registered.Status.BrokerID = ptr.To(int32(4))
	unregistered := &redpandav1alpha2.Broker{}
	diskLost := &redpandav1alpha2.Broker{}
	diskLost.Status.BrokerID = ptr.To(int32(4))
	diskLost.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{}

	readyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}}}
	unreadyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionFalse},
	}}}
	unschedulablePod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
	}}}

	for name, tc := range map[string]struct {
		broker *redpandav1alpha2.Broker
		pod    *corev1.Pod
		want   redpandav1alpha2.BrokerPhase
	}{
		"unregistered, pod not ready":  {unregistered, unreadyPod, redpandav1alpha2.BrokerPhaseProvisioning},
		"unregistered, pod ready":      {unregistered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, pod ready":        {registered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, readiness dip":    {registered, unreadyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, no pod yet":       {registered, &corev1.Pod{}, redpandav1alpha2.BrokerPhaseRunning},
		"stuck overrides registration": {registered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
		"stuck overrides provisioning": {unregistered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
		"disk-lost latch is sticky":    {diskLost, readyPod, redpandav1alpha2.BrokerPhaseDiskLost},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, defaultPhase(tc.broker, tc.pod))
		})
	}
}

// TestReconcileUnmanagedIsNoOp pins the managed=false gate: the only write is
// dropping the finalizer (so deletion while unmanaged needs no operator), and
// after that the Broker quiesces — no pod is created for a podless Broker and
// a repeat pass writes nothing.
func TestReconcileUnmanagedIsNoOp(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rp-abcde",
			Namespace:   "ns",
			Annotations: map[string]string{feature.V2Managed.Key: "false"},
			Finalizers:  []string{brokerFinalizerName},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(0)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker).
		WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
	r := &BrokerReconciler{Manager: &unmanagedTestManager{cluster: &unmanagedTestCluster{client: c}}}

	req := mcreconcile.Request{Request: reconcile.Request{NamespacedName: client.ObjectKeyFromObject(broker)}}
	result, err := r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.IsZero())

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(broker), broker))
	require.Empty(t, broker.Finalizers)

	var pods corev1.PodList
	require.NoError(t, c.List(t.Context(), &pods))
	require.Empty(t, pods.Items)

	rv := broker.ResourceVersion
	result, err = r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.IsZero())
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(broker), broker))
	require.Equal(t, rv, broker.ResourceVersion)
}

// TestReconcilePodRotation pins that a granted, outdated pod whose broker
// cannot start is deleted without the drain a down broker never finishes, and
// that a no-drain grant never rotates a broker that has since recovered.
func TestReconcilePodRotation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	for name, tc := range map[string]struct {
		podHash string
		failing bool
		// hung runs redpanda without its startup probe passing; unschedulable
		// never schedules the pod.
		hung, unschedulable bool
		terminating         bool
		granted             bool
		noDrain             bool
		decommission        bool
		wantDeleted         bool
		wantRequeue         time.Duration
		// wantParked expects the no-grant path, which also clears maintenance
		// mode.
		wantParked bool
		// registered gives the broker an ID, so a normal grant drains it,
		// which fails here: the admin API is unreachable.
		registered, wantErr bool
		// startedSince has the pod's container start after it was read,
		// goneSince the pod deleted, revokedSince the grant revoked,
		// expiredSince it expired, and movedOnSince the template move on.
		startedSince, goneSince, revokedSince, expiredSince, movedOnSince bool
	}{
		"a no-drain grant deletes a pod that cannot start without draining": {
			podHash: "stale", failing: true, granted: true, noDrain: true,
			wantDeleted: true, wantRequeue: requeueShort,
		},
		"a no-drain grant never deletes a pod whose container started since it was read": {
			podHash: "stale", failing: true, granted: true, noDrain: true, startedSince: true,
			wantRequeue: requeueShort,
		},
		"a no-drain grant deletes a registered broker's pod, though no admin API answers to clear maintenance mode": {
			podHash: "stale", failing: true, granted: true, noDrain: true, registered: true,
			wantDeleted: true, wantRequeue: requeueShort,
		},
		"nor a registered broker's whose container started since": {
			podHash: "stale", failing: true, granted: true, noDrain: true, registered: true, startedSince: true,
			wantRequeue: requeueShort,
		},
		"a no-drain grant deletes a pod whose start hung": {
			podHash: "stale", hung: true, granted: true, noDrain: true,
			wantDeleted: true, wantRequeue: requeueShort,
		},
		"a no-drain grant deletes a pod that can't be scheduled": {
			podHash: "stale", unschedulable: true, granted: true, noDrain: true,
			wantDeleted: true, wantRequeue: requeueShort,
		},
		"a no-drain grant revoked since it was read deletes nothing": {
			podHash: "stale", failing: true, granted: true, noDrain: true, revokedSince: true,
			wantRequeue: requeueShort,
		},
		"a no-drain grant expired since it was read deletes nothing": {
			podHash: "stale", failing: true, granted: true, noDrain: true, expiredSince: true,
			wantRequeue: requeueShort,
		},
		"a no-drain grant whose template moved on since it was read deletes nothing": {
			podHash: "stale", failing: true, granted: true, noDrain: true, movedOnSince: true,
			wantRequeue: requeueShort,
		},
		"a no-drain grant on a pod already gone requeues": {
			podHash: "stale", failing: true, granted: true, noDrain: true, goneSince: true,
			wantDeleted: true, wantRequeue: requeueShort,
		},
		"no grant, no rotation": {
			podHash: "stale", failing: true,
			wantRequeue: periodicRequeue, wantParked: true,
		},
		"a no-drain grant on a broker that recovered is parked as if it held none": {
			podHash: "stale", granted: true, noDrain: true,
			wantRequeue: periodicRequeue, wantParked: true,
		},
		"a normal grant drains a pod that looks unable to start": {
			podHash: "stale", failing: true, granted: true, registered: true,
			wantErr: true,
		},
		"already terminating": {
			podHash: "stale", failing: true, granted: true, noDrain: true, terminating: true,
			wantRequeue: requeueShort,
		},
		"cannot start on the desired template is left to kubelet": {
			podHash: "desired", failing: true, granted: true,
		},
		"decommissioning broker is never rotated": {
			podHash: "stale", failing: true, granted: true, decommission: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker := &redpandav1alpha2.Broker{
				ObjectMeta: metav1.ObjectMeta{Name: "rp-a", Namespace: "ns"},
				Spec: redpandav1alpha2.BrokerSpec{
					ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
					NetworkIndex: ptr.To(int32(0)),
					Decommission: tc.decommission,
					PodTemplate: redpandav1alpha2.BrokerPodTemplate{Annotations: map[string]string{
						redpandav1alpha2.BrokerPodTemplateHashAnnotation: "desired",
					}},
				},
			}
			if tc.noDrain {
				broker.SetNoDrainRollGrant("desired", time.Now().Add(redpandav1alpha2.RollGrantTTL))
			}
			if tc.registered {
				broker.Status.BrokerID = ptr.To(int32(0))
			}

			redpanda := corev1.ContainerStatus{Name: "redpanda", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
			if tc.failing {
				now := metav1.Now()
				redpanda.State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
				redpanda.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ExitCode: 1, StartedAt: now, FinishedAt: now}
			}
			if tc.hung {
				redpanda.State.Running.StartedAt = metav1.NewTime(time.Now().Add(-15 * time.Minute))
				redpanda.Started = ptr.To(false)
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        broker.PodName(),
					Namespace:   broker.Namespace,
					Annotations: map[string]string{redpandav1alpha2.BrokerPodTemplateHashAnnotation: tc.podHash},
				},
				Status: corev1.PodStatus{
					Conditions:        []corev1.PodCondition{{Type: corev1.PodInitialized, Status: corev1.ConditionTrue}},
					ContainerStatuses: []corev1.ContainerStatus{redpanda},
				},
			}
			if tc.unschedulable {
				pod.Status = corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{
					Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
				}}}
			}
			if tc.terminating {
				pod.DeletionTimestamp = ptr.To(metav1.Now())
				pod.Finalizers = []string{"keep-alive-during-test"}
			}

			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker, pod).WithStatusSubresource(&corev1.Pod{}).Build()
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
			if tc.startedSince {
				started := pod.DeepCopy()
				started.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
				require.NoError(t, c.Status().Update(t.Context(), started))
			}
			if tc.goneSince {
				require.NoError(t, c.Delete(t.Context(), pod.DeepCopy()))
			}
			if tc.revokedSince || tc.expiredSince || tc.movedOnSince {
				since := broker.DeepCopy()
				checksum, _, _ := since.ParseRollGrant()
				switch {
				case tc.revokedSince:
					since.RemoveRollGrant()
				case tc.expiredSince:
					since.SetNoDrainRollGrant(checksum, time.Now().Add(-time.Minute))
				case tc.movedOnSince:
					since.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerPodTemplateHashAnnotation] = "moved on"
				}
				require.NoError(t, c.Update(t.Context(), since))
			}
			r := &BrokerReconciler{ClientFactory: unreachableAdminFactory{}}
			state := &brokerReconciliationState{broker: broker, pod: pod, granted: tc.granted}

			result, err := r.reconcilePodRotation(t.Context(), state, &unmanagedTestCluster{client: c})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantRequeue, result.RequeueAfter)
			require.Equal(t, tc.wantParked, state.phase == redpandav1alpha2.BrokerPhaseRunning, "parked")

			err = c.Get(t.Context(), client.ObjectKeyFromObject(pod), &corev1.Pod{})
			require.Equal(t, tc.wantDeleted, apierrors.IsNotFound(err), "pod deleted")
		})
	}
}

// unreachableAdminFactory fails every admin client it is asked for.
type unreachableAdminFactory struct{ internalclient.ClientFactory }

func (unreachableAdminFactory) RedpandaAdminClientForCluster(context.Context, any, string) (*rpadmin.AdminAPI, error) {
	return nil, errors.New("admin API unreachable")
}

// unmanagedTestManager/-Cluster satisfy just enough of multicluster.Manager
// and cluster.Cluster to drive Reconcile against a fake client.
type unmanagedTestManager struct {
	multicluster.Manager
	cluster cluster.Cluster
}

func (m *unmanagedTestManager) GetCluster(context.Context, string) (cluster.Cluster, error) {
	return m.cluster, nil
}

type unmanagedTestCluster struct {
	cluster.Cluster
	client client.Client
}

func (c *unmanagedTestCluster) GetClient() client.Client { return c.client }

func (c *unmanagedTestCluster) GetAPIReader() client.Reader { return c.client }
