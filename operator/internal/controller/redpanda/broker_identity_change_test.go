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
	"fmt"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// TestReconcileChangedIdentity pins the K8S-977 remediation entry point: a
// pod that re-registered under a new node_id (same-node disk wipe) converts
// its Broker CR into a released DiskLost tombstone — pod handed over for the
// replacement to adopt, old id pinned for the tombstone lifecycle to
// decommission — but only once the IdentityChanged condition has held
// unchanged for MarkDiskLostAfter, the new identity is provably alive, and
// an owning cluster exists to create the replacement. Anything short of
// that parks the CR in Stuck exactly as before.
func TestReconcileChangedIdentity(t *testing.T) {
	const oldID, newID = 2, 4
	conflict := fmt.Sprintf("broker re-registered with node_id %d, expected %d", newID, oldID)

	member := func(id int, alive bool, host string) rpadmin.Broker {
		return rpadmin.Broker{
			NodeID:             id,
			InternalRPCAddress: host + ".rp.test.svc.cluster.local",
			MembershipStatus:   rpadmin.MembershipStatusActive,
			IsAlive:            ptr.To(alive),
		}
	}

	for _, tc := range []struct {
		name string
		// membership at the pod's address; built per-case with the pod name.
		membership func(podName string) []rpadmin.Broker
		condition  *metav1.Condition
		unowned    bool
		wantPhase  redpandav1alpha2.BrokerPhase
		// wantConverted asserts DiskLost{ResourcesReleased} is latched and
		// the pod's owner ref is stripped.
		wantConverted bool
	}{
		{
			name: "parks while the debounce has not elapsed",
			membership: func(p string) []rpadmin.Broker {
				return []rpadmin.Broker{member(oldID, false, p), member(newID, true, p)}
			},
			condition: &metav1.Condition{
				Type:               statuses.BrokerBrokerRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             string(statuses.BrokerBrokerRegisteredReasonIdentityChanged),
				Message:            conflict,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Minute)),
			},
			wantPhase: redpandav1alpha2.BrokerPhaseStuck,
		},
		{
			name: "converts after the debounce",
			membership: func(p string) []rpadmin.Broker {
				return []rpadmin.Broker{member(oldID, false, p), member(newID, true, p)}
			},
			condition: &metav1.Condition{
				Type:               statuses.BrokerBrokerRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             string(statuses.BrokerBrokerRegisteredReasonIdentityChanged),
				Message:            conflict,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-6 * time.Minute)),
			},
			wantPhase:     redpandav1alpha2.BrokerPhaseDiskLost,
			wantConverted: true,
		},
		{
			name: "a different conflict restarts the clock",
			membership: func(p string) []rpadmin.Broker {
				return []rpadmin.Broker{member(oldID, false, p), member(newID, true, p)}
			},
			condition: &metav1.Condition{
				Type:               statuses.BrokerBrokerRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             string(statuses.BrokerBrokerRegisteredReasonIdentityChanged),
				Message:            fmt.Sprintf("broker re-registered with node_id %d, expected %d", 9, oldID),
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			wantPhase: redpandav1alpha2.BrokerPhaseStuck,
		},
		{
			name: "parks when the new identity is not alive",
			membership: func(p string) []rpadmin.Broker {
				return []rpadmin.Broker{member(newID, false, p)}
			},
			condition: &metav1.Condition{
				Type:               statuses.BrokerBrokerRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             string(statuses.BrokerBrokerRegisteredReasonIdentityChanged),
				Message:            conflict,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			wantPhase: redpandav1alpha2.BrokerPhaseStuck,
		},
		{
			name: "parks without an owning cluster",
			membership: func(p string) []rpadmin.Broker {
				return []rpadmin.Broker{member(oldID, false, p), member(newID, true, p)}
			},
			condition: &metav1.Condition{
				Type:               statuses.BrokerBrokerRegistered,
				Status:             metav1.ConditionFalse,
				Reason:             string(statuses.BrokerBrokerRegisteredReasonIdentityChanged),
				Message:            conflict,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			unowned:   true,
			wantPhase: redpandav1alpha2.BrokerPhaseStuck,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := diskLostScheme(t)

			owner := &redpandav1alpha2.Redpanda{
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"},
			}
			broker := &redpandav1alpha2.Broker{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rp-broker-0", Namespace: "test", UID: "broker-uid",
					Finalizers: []string{brokerFinalizerName},
				},
				Spec: redpandav1alpha2.BrokerSpec{
					ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
					NetworkIndex: ptr.To(int32(0)),
				},
				Status: redpandav1alpha2.BrokerStatus{
					BrokerID: ptr.To(int32(oldID)),
					Phase:    redpandav1alpha2.BrokerPhaseStuck,
				},
			}
			if tc.condition != nil {
				broker.Status.Conditions = []metav1.Condition{*tc.condition}
			}
			if !tc.unowned {
				require.NoError(t, controllerutil.SetControllerReference(owner, broker, scheme))
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: broker.PodName(), Namespace: "test"},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.9"},
			}
			require.NoError(t, controllerutil.SetControllerReference(broker, pod, scheme))

			var calls adminCalls
			srv := fakeAdminServer(t, tc.membership(broker.PodName()), &calls)

			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&redpandav1alpha2.Broker{}).
				WithObjects(owner, broker, pod).Build()
			r := &BrokerReconciler{
				ClientFactory:     stubAdminFactory{url: srv.URL},
				MarkDiskLostAfter: 5 * time.Minute,
			}
			state := &brokerReconciliationState{
				broker:        broker,
				pod:           pod,
				initialStatus: broker.Status.DeepCopy(),
			}

			_, err := r.reconcileBrokerRegistration(ctx, state, &diskLostCluster{client: c})
			require.NoError(t, err)

			phase := state.phase
			if tc.wantPhase == redpandav1alpha2.BrokerPhaseStuck {
				require.NotEmpty(t, state.registrationConflict, "a parked identity change must carry its conflict")
				phase = redpandav1alpha2.BrokerPhaseStuck
			}
			require.Equal(t, tc.wantPhase, phase)

			require.Equal(t, ptr.To(int32(oldID)), broker.Status.BrokerID,
				"the old id must stay pinned either way: it is the tombstone's decommission record")
			require.Empty(t, calls.decommissions,
				"conversion must never decommission anything itself; that is the tombstone lifecycle's job")

			var livePod corev1.Pod
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &livePod))
			if tc.wantConverted {
				require.NotNil(t, broker.Status.DiskLost, "conversion must latch the tombstone")
				require.True(t, broker.Status.DiskLost.ResourcesReleased,
					"a handed-over slot is released: the pod and PVCs live on under the replacement")
				require.Nil(t, metav1.GetControllerOf(&livePod),
					"the pod must be released for the replacement to adopt")
			} else {
				require.Nil(t, broker.Status.DiskLost)
				require.True(t, metav1.IsControlledBy(&livePod, broker),
					"a parked Broker must keep its pod")
			}
		})
	}
}

// identityCluster is diskLostCluster plus a scheme, which reconcilePod's
// adoption branch needs.
type identityCluster struct {
	cluster.Cluster
	client client.Client
	scheme *runtime.Scheme
}

func (c *identityCluster) GetClient() client.Client    { return c.client }
func (c *identityCluster) GetScheme() *runtime.Scheme  { return c.scheme }
func (c *identityCluster) GetAPIReader() client.Reader { return c.client }

// TestTombstoneNeverTouchesHandedOverPod pins the handover invariant stated
// on reconcileChangedIdentity: from conversion onward, the pod at a released
// tombstone's name belongs to the slot's replacement. A decommissioning
// tombstone must not re-adopt it when it is still an orphan (e.g. the
// undesired-index path marks the tombstone before any replacement exists),
// and the decommission-completion path must release — never delete — a pod
// the tombstone still owns (pre-guard adoption, or an operator predating the
// guard).
func TestTombstoneNeverTouchesHandedOverPod(t *testing.T) {
	tombstone := func() *redpandav1alpha2.Broker {
		return &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rp-broker-0", Namespace: "test", UID: "tombstone-uid",
				Finalizers: []string{brokerFinalizerName},
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
				NetworkIndex: ptr.To(int32(0)),
				Decommission: true,
			},
			Status: redpandav1alpha2.BrokerStatus{
				Phase:    redpandav1alpha2.BrokerPhaseDiskLost,
				BrokerID: ptr.To(int32(2)),
				DiskLost: &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true},
			},
		}
	}
	livePod := func(b *redpandav1alpha2.Broker) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: b.PodName(), Namespace: "test"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.9"},
		}
	}

	t.Run("a decommissioning tombstone must not adopt the handed-over pod", func(t *testing.T) {
		ctx := context.Background()
		scheme := diskLostScheme(t)
		broker := tombstone()
		pod := livePod(broker) // orphan: handed over, replacement not yet created

		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&redpandav1alpha2.Broker{}).
			WithObjects(broker, pod).Build()
		r := &BrokerReconciler{}
		state := &brokerReconciliationState{broker: broker, pod: pod}

		res, err := r.reconcilePod(ctx, state, &identityCluster{client: c, scheme: scheme})
		require.NoError(t, err)
		require.True(t, res.IsZero(), "the chain must continue to the decommission")

		var got corev1.Pod
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(pod), &got))
		require.Nil(t, metav1.GetControllerOf(&got),
			"the handed-over pod must stay free for the replacement to adopt")
	})

	t.Run("decommission completion releases a still-owned pod", func(t *testing.T) {
		ctx := context.Background()
		scheme := diskLostScheme(t)
		broker := tombstone()
		pod := livePod(broker)
		require.NoError(t, controllerutil.SetControllerReference(broker, pod, scheme))

		// The pinned id 2 is already absent from membership: the
		// decommission completes on this pass.
		var calls adminCalls
		srv := fakeAdminServer(t, []rpadmin.Broker{
			{NodeID: 1, InternalRPCAddress: "rp-1.rp.test.svc.cluster.local", MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: ptr.To(true)},
			{NodeID: 4, InternalRPCAddress: broker.PodName() + ".rp.test.svc.cluster.local", MembershipStatus: rpadmin.MembershipStatusActive, IsAlive: ptr.To(true)},
		}, &calls)

		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&redpandav1alpha2.Broker{}).
			WithObjects(broker, pod).Build()
		r := &BrokerReconciler{ClientFactory: stubAdminFactory{url: srv.URL}}
		state := &brokerReconciliationState{broker: broker, pod: pod}

		_, err := r.reconcileDecommission(ctx, state, &identityCluster{client: c, scheme: scheme})
		require.NoError(t, err)
		require.Equal(t, redpandav1alpha2.BrokerPhaseDecommissioned, state.phase)

		var got corev1.Pod
		getErr := c.Get(ctx, client.ObjectKeyFromObject(pod), &got)
		require.False(t, apierrors.IsNotFound(getErr),
			"the pod is the live replacement broker; completion must never delete it")
		require.NoError(t, getErr)
		require.Nil(t, metav1.GetControllerOf(&got),
			"completion must release the pod for the replacement to adopt")
	})
}
