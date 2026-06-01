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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// TestSidecarsPassthrough verifies that the broker-decommissioner and
// pvc-unbinder sidecar fields added to the Redpanda CRD reach the rendered
// Helm chart values (the operator marshals clusterSpec straight into the
// chart's value loader), so operator-managed clusters can enable the sidecars.
func TestSidecarsPassthrough(t *testing.T) {
	rp := &Redpanda{
		Spec: RedpandaSpec{
			ClusterSpec: &RedpandaClusterSpec{
				Statefulset: &Statefulset{
					SideCars: &SideCars{
						BrokerDecommissioner: &BrokerDecommissioner{
							Enabled:                    ptr.To(true),
							DecommissionAfter:          ptr.To("45s"),
							DecommissionRequeueTimeout: ptr.To("15s"),
						},
						PVCUnbinder: &PVCUnbinder{
							Enabled:     ptr.To(true),
							UnbindAfter: ptr.To("90s"),
						},
					},
				},
			},
		},
	}

	values, err := rp.GetValues()
	require.NoError(t, err)

	require.True(t, values.Statefulset.SideCars.BrokerDecommissioner.Enabled)
	require.Equal(t, "45s", values.Statefulset.SideCars.BrokerDecommissioner.DecommissionAfter)
	require.Equal(t, "15s", values.Statefulset.SideCars.BrokerDecommissioner.DecommissionRequeueTimeout)

	require.True(t, values.Statefulset.SideCars.PVCUnbinder.Enabled)
	require.Equal(t, "90s", values.Statefulset.SideCars.PVCUnbinder.UnbindAfter)
}
