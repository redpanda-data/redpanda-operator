// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestReconcileDiskLostBrokers pins the tombstone lifecycle rules the
// identity-change handover rides on (K8S-977): a released tombstone at a
// desired index is decommissioned only once its replacement has REGISTERED
// (the drain needs it as a re-replication target), one disruptive operation
// runs at a time cluster-wide, an undesired index is reaped without waiting
// for a replacement that will never come, and a Decommissioned tombstone is
// deleted.
func TestReconcileDiskLostBrokers(t *testing.T) {
	released := func() *redpandav1alpha2.Broker {
		return &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{Name: "rp-abcde", Namespace: "ns"},
			Spec:       redpandav1alpha2.BrokerSpec{NetworkIndex: ptr.To(int32(0))},
			Status: redpandav1alpha2.BrokerStatus{
				Phase:    redpandav1alpha2.BrokerPhaseDiskLost,
				BrokerID: ptr.To(int32(2)),
				DiskLost: &redpandav1alpha2.DiskLostStatus{At: metav1.Now(), ResourcesReleased: true},
			},
		}
	}
	registered := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-fghij", Namespace: "ns"},
		Spec:       redpandav1alpha2.BrokerSpec{NetworkIndex: ptr.To(int32(0))},
		Status:     redpandav1alpha2.BrokerStatus{BrokerID: ptr.To(int32(4))},
	}
	unregistered := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-fghij", Namespace: "ns"},
		Spec:       redpandav1alpha2.BrokerSpec{NetworkIndex: ptr.To(int32(0))},
	}

	for _, tc := range []struct {
		name            string
		tombstone       func() *redpandav1alpha2.Broker
		replacement     *redpandav1alpha2.Broker
		desiredReplicas int32
		inFlight        bool
		wantMarked      bool
		wantDeleted     bool
	}{
		{
			name:            "marks once the replacement has registered",
			tombstone:       released,
			replacement:     registered,
			desiredReplicas: 3,
			wantMarked:      true,
		},
		{
			name:            "waits for an unregistered replacement",
			tombstone:       released,
			replacement:     unregistered,
			desiredReplicas: 3,
		},
		{
			name:            "waits while no replacement exists at a desired index",
			tombstone:       released,
			desiredReplicas: 3,
		},
		{
			name:            "marks an undesired index immediately",
			tombstone:       released,
			desiredReplicas: 0,
			wantMarked:      true,
		},
		{
			name:            "never starts a second disruption",
			tombstone:       released,
			replacement:     registered,
			desiredReplicas: 3,
			inFlight:        true,
		},
		{
			name: "deletes a Decommissioned tombstone",
			tombstone: func() *redpandav1alpha2.Broker {
				b := released()
				b.Status.Phase = redpandav1alpha2.BrokerPhaseDecommissioned
				b.Status.BrokerID = nil
				return b
			},
			desiredReplicas: 3,
			wantDeleted:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, redpandav1alpha2.Install(scheme))

			tombstone := tc.tombstone()
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&redpandav1alpha2.Broker{}).
				WithObjects(tombstone).Build()
			s := &BrokerSet{Client: c, Arbitration: &Arbitration{}}

			liveByIndex := map[int32]*redpandav1alpha2.Broker{}
			if tc.replacement != nil {
				liveByIndex[0] = tc.replacement
			}

			inFlight, err := s.ReconcileDiskLostBrokers(ctx, logr.Discard(),
				[]*redpandav1alpha2.Broker{tombstone}, liveByIndex, tc.desiredReplicas, tc.inFlight, false)
			require.NoError(t, err)

			var got redpandav1alpha2.Broker
			getErr := c.Get(ctx, k8sclient.ObjectKeyFromObject(tombstone), &got)
			if tc.wantDeleted {
				require.True(t, apierrors.IsNotFound(getErr), "a Decommissioned tombstone must be deleted")
				return
			}
			require.NoError(t, getErr)
			require.Equal(t, tc.wantMarked, got.Spec.Decommission)
			require.Equal(t, tc.wantMarked, s.Arbitration.DecommissionMarked(),
				"every mark must register with the pass-wide arbitration")
			require.Equal(t, tc.wantMarked || tc.inFlight, inFlight)
		})
	}
}
