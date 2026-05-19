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
		1, // pool-local replica index
		1, // global ordinal (single pool → equal to replica index)
		"http.example.com",
		"http-$POD_ORDINAL.example.com",
		true,
	)

	require.Equal(t, "http-1.example.com", host["address"])
	require.Equal(t, int32(8443), host["port"])
}

// TestGatewayNodePoolAdvertisesGlobalOrdinalHost is the regression test for the
// node-pool advertised-host bug: TLSRoutes/services are named and hosted by the
// global pod-list index, but the configurator renders per node pool with a
// StatefulSet-local ordinal. A pool broker must advertise the host at its GLOBAL
// ordinal (offset + local) so its advertised address matches the TLSRoute that
// routes to it — previously it advertised the local-ordinal host (e.g. -0),
// which matched no TLSRoute / a different broker's service.
func TestGatewayNodePoolAdvertisesGlobalOrdinalHost(t *testing.T) {
	state := &RenderState{
		Release: &helmette.Release{Name: "redpanda", Namespace: "default"},
		Chart:   &helmette.Chart{Name: "redpanda", Version: "0.0.0"},
		Values: Values{
			External: ExternalConfig{
				Enabled: true,
				Gateway: &GatewayConfig{
					Enabled:        true,
					ParentRefs:     []GatewayParentRef{{Name: "kafka-gateway"}},
					AdvertisedPort: ptr.To[int32](9094),
				},
			},
			Statefulset: Statefulset{Replicas: 2},
			Listeners: Listeners{
				Kafka: ListenerConfig[KafkaAuthenticationMethod]{
					Port: 9094,
					External: map[string]ExternalListener[KafkaAuthenticationMethod]{
						"default": {
							Port:         9094,
							Gateway:      ptr.To(true),
							Host:         ptr.To("redpanda.example.com"),
							HostTemplate: ptr.To("redpanda-$POD_ORDINAL.example.com"),
						},
					},
				},
			},
		},
		// One additional node pool with a single broker.
		Pools: []Pool{{Name: "np", Statefulset: Statefulset{Replicas: 1}}},
	}

	// Global pod order: main STS (2) then the pool (1).
	require.Equal(t, []string{"redpanda-0", "redpanda-1", "redpanda-np-0"}, gatewayPodNames(state))

	// The pool's local ordinal 0 is global ordinal 2 (offset = main replicas).
	const poolGlobalOrdinal = 2
	advertised := advertisedHostJSONGateway(state, poolGlobalOrdinal, "redpanda.example.com", "redpanda-$POD_ORDINAL.example.com")
	require.Equal(t, "redpanda-2.example.com", advertised["address"],
		"pool broker must advertise its global-ordinal host, not the local-ordinal (redpanda-0) host")

	// And that advertised host must equal the hostname of the per-broker TLSRoute
	// rendered for the same global ordinal.
	var poolRouteHost string
	for _, r := range TLSRoutes(state) {
		if r.ObjectMeta.Name == "redpanda-kafka-default-2" {
			poolRouteHost = r.Spec.Hostnames[0]
		}
	}
	require.Equal(t, "redpanda-2.example.com", poolRouteHost)
	require.Equal(t, advertised["address"], poolRouteHost,
		"advertised host and TLSRoute SNI for the pool broker must match")
}
