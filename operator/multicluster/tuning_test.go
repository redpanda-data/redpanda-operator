// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestValidateTuning(t *testing.T) {
	cases := []struct {
		name    string
		tuning  *redpandav1alpha2.StretchTuning
		wantErr bool
	}{
		{name: "nil tuning", tuning: nil, wantErr: false},
		{name: "empty tuning", tuning: &redpandav1alpha2.StretchTuning{}, wantErr: false},
		{
			name: "aio only",
			tuning: &redpandav1alpha2.StretchTuning{
				TuneAioEvents: ptr.To(true),
			},
			wantErr: false,
		},
		{
			name: "host tuners with aio",
			tuning: &redpandav1alpha2.StretchTuning{
				TuneAioEvents:   ptr.To(true),
				ApplyHostTuners: ptr.To(true),
			},
			wantErr: false,
		},
		{
			// The contradiction this validation exists for: host tuners
			// enabled but the init container that applies them (gated on
			// tune_aio_events) never renders.
			name: "host tuners without aio",
			tuning: &redpandav1alpha2.StretchTuning{
				ApplyHostTuners: ptr.To(true),
			},
			wantErr: true,
		},
		{
			name: "host tuners with aio explicitly disabled",
			tuning: &redpandav1alpha2.StretchTuning{
				TuneAioEvents:   ptr.To(false),
				ApplyHostTuners: ptr.To(true),
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTuning(tc.tuning)
			if tc.wantErr {
				require.ErrorContains(t, err, "apply_host_tuners requires")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRenderHostTunersWithoutAioFails ensures the contradiction is rejected by
// both render entry points, not just the helper: a StretchCluster setting
// apply_host_tuners while explicitly disabling tune_aio_events must fail to
// render StatefulSets and ConfigMaps alike. (Leaving tune_aio_events unset is
// fine — MergeDefaults defaults it to true, matching the Helm chart.)
func TestRenderHostTunersWithoutAioFails(t *testing.T) {
	cluster := &redpandav1alpha2.StretchCluster{}
	cluster.Name = "host-tuners-invalid"
	cluster.Namespace = "host-tuners-invalid"
	cluster.Spec.Tuning = &redpandav1alpha2.StretchTuning{
		TuneAioEvents:   ptr.To(false),
		ApplyHostTuners: ptr.To(true),
	}

	pool := &redpandav1alpha2.RedpandaBrokerPool{}
	pool.Name = "pool-a"
	pool.Namespace = cluster.Namespace
	pool.Spec.Replicas = ptr.To(int32(1))

	state, err := NewRenderState(nil, cluster, []*redpandav1alpha2.RedpandaBrokerPool{pool}, []*redpandav1alpha2.RedpandaBrokerPool{pool}, "test")
	require.NoError(t, err)

	_, err = RenderBrokerPools(state)
	require.ErrorContains(t, err, "apply_host_tuners requires")

	_, err = RenderResources(state)
	require.ErrorContains(t, err, "apply_host_tuners requires")
}
