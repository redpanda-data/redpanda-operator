// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

func TestListenerPorts(t *testing.T) {
	type external = redpandachart.ExternalListener[redpandachart.KafkaAuthenticationMethod]

	for _, tc := range []struct {
		name     string
		external map[string]external
		want     []int32
	}{
		{name: "internal only", want: []int32{9093}},
		{
			name: "enabled externals, sorted",
			external: map[string]external{
				"b": {Port: 9095},
				"a": {Port: 9094, Enabled: ptr.To(true)},
			},
			want: []int32{9093, 9094, 9095},
		},
		{
			name: "disabled and portless externals bind nothing",
			external: map[string]external{
				"off":   {Port: 9094, Enabled: ptr.To(false)},
				"unset": {},
			},
			want: []int32{9093},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := redpandachart.ListenerConfig[redpandachart.KafkaAuthenticationMethod]{Port: 9093, External: tc.external}
			require.Equal(t, tc.want, listenerPorts(listener))
		})
	}
}

func TestV1KafkaPorts(t *testing.T) {
	external := vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}

	for _, tc := range []struct {
		name      string
		listeners []vectorizedv1alpha1.KafkaAPI
		want      []int32
	}{
		{name: "no internal listener", listeners: []vectorizedv1alpha1.KafkaAPI{{Port: 30092, External: external}}},
		{name: "internal only", listeners: []vectorizedv1alpha1.KafkaAPI{{Port: 9092}}, want: []int32{9092}},
		{
			name:      "external naming its port",
			listeners: []vectorizedv1alpha1.KafkaAPI{{Port: 9092}, {Port: 30092, External: external}},
			want:      []int32{9092, 30092},
		},
		{
			name:      "external naming no port binds the internal one's plus one",
			listeners: []vectorizedv1alpha1.KafkaAPI{{Port: 9092}, {External: external}},
			want:      []int32{9092, 9093},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &vectorizedv1alpha1.Cluster{}
			cluster.Spec.Configuration.KafkaAPI = tc.listeners
			require.Equal(t, tc.want, v1KafkaPorts(cluster))
		})
	}
}
