// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestResolveNetwork(t *testing.T) {
	for name, tc := range map[string]struct {
		values   ExternalListener[NoAuth]
		http     bool
		internal []string
		nodePort []string
		gateway  []string
	}{
		"external": {
			values:   ExternalListener[NoAuth]{Port: 9645},
			http:     true,
			internal: []string{"admin", "http", "kafka", "rpc"},
			nodePort: []string{"admin-default"},
		},
		"disabled": {
			values:   ExternalListener[NoAuth]{Port: 9645, Enabled: ptr.To(false)},
			http:     true,
			internal: []string{"admin", "http", "kafka", "rpc"},
		},
		"gateway": {
			values:   ExternalListener[NoAuth]{Port: 9645, Type: ptr.To(ExternalListenerTypeTLSRoute), Host: ptr.To("admin.example.com")},
			http:     true,
			internal: []string{"admin", "http", "kafka", "rpc"},
			gateway:  []string{"admin-default"},
		},
		// listeners.http.enabled false removes http from the internal Services.
		// listeners.admin.enabled does not have this effect.
		"http disabled": {
			values:   ExternalListener[NoAuth]{Port: 9645},
			internal: []string{"admin", "kafka", "rpc"},
			nodePort: []string{"admin-default"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := &RenderState{
				Release: &helmette.Release{Name: "redpanda", Namespace: "default"},
				Chart:   &helmette.Chart{Name: "redpanda", Version: "0.0.0"},
				Values: Values{
					External: ExternalConfig{
						Enabled: true,
						Type:    corev1.ServiceTypeNodePort,
						Service: Enableable{Enabled: true},
						Gateway: &GatewayConfig{
							Enabled:    true,
							ParentRefs: []gatewayv1.ParentReference{{Name: "gateway"}},
						},
					},
					Listeners: Listeners{
						Admin: ListenerConfig[NoAuth]{
							Port:     9644,
							External: map[string]ExternalListener[NoAuth]{"default": tc.values},
						},
						HTTP:  ListenerConfig[HTTPAuthenticationMethod]{Port: 8082, Enabled: tc.http},
						Kafka: ListenerConfig[KafkaAuthenticationMethod]{Port: 9093},
					},
				},
			}

			listeners := resolveListeners(state, &redpanda.PKI{})
			network := resolveNetwork(state, &listeners)

			require.Equal(t, tc.internal, servicePortNames(network.Service(redpanda.ServiceKindHeadless)))
			require.Equal(t, tc.nodePort, servicePortNames(network.Service(redpanda.ServiceKindNodePort)))
			require.Equal(t, tc.gateway, servicePortNames(network.Service(redpanda.ServiceKindGateway)))
			require.Nil(t, network.Service(redpanda.ServiceKindLoadBalancer))

			// Only a gateway listener has a route.
			require.Equal(t, len(tc.gateway) > 0, network.Route(redpanda.AdminAPI, "default") != nil)
		})
	}
}

func servicePortNames(config *redpanda.ServiceConfig) []string {
	var names []string
	for _, port := range config.Ports() {
		names = append(names, port.Name)
	}
	return names
}
