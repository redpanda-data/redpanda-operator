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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestAdvertisedHostJSONGatewayUsesCurrentListenerConfig(t *testing.T) {
	state := &RenderState{
		Release: &helmette.Release{
			Name:      "redpanda",
			Namespace: "default",
		},
		Values: Values{
			External: ExternalConfig{
				Enabled: true,
				Gateway: &GatewayConfig{
					Enabled:        true,
					AdvertisedPort: ptr.To[int32](8443),
					ParentRefs:     []GatewayParentRef{{Name: "shared-gateway"}},
				},
			},
			Statefulset: Statefulset{
				Replicas: 2,
			},
		},
	}

	host := advertisedHostJSON(
		state,
		8082,
		1,
		"http.example.com",
		"http-$POD_ORDINAL.example.com",
		true,
	)

	require.Equal(t, "http-1.example.com", host["address"])
	require.Equal(t, int32(8443), host["port"])
}
