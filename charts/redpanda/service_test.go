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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestNetworkRoute(t *testing.T) {
	network := Network{Routes: map[APIKind]map[string]GatewayRoute{
		SchemaRegistryAPI: {"default": {Host: "schema.example.com"}},
	}}

	require.Equal(t, &GatewayRoute{Host: "schema.example.com"}, network.Route(SchemaRegistryAPI, "default"))
	require.Nil(t, network.Route(SchemaRegistryAPI, "other"))
	require.Nil(t, network.Route(KafkaAPI, "default"))
}

func TestServiceConfigPorts(t *testing.T) {
	for kind, expected := range map[ServiceKind][]corev1.ServicePort{
		// In-cluster sequence.
		ServiceKindHeadless: {
			{Name: "http", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 8082, TargetPort: intstr.FromInt32(8082)},
			{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9092, TargetPort: intstr.FromInt32(9092)},
			{Name: "rpc", Protocol: corev1.ProtocolTCP, Port: 33145, TargetPort: intstr.FromInt32(33145)},
		},
		ServiceKindBroker: {
			{Name: "http", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 8082, TargetPort: intstr.FromInt32(8082)},
			{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9092, TargetPort: intstr.FromInt32(9092)},
			{Name: "rpc", Protocol: corev1.ProtocolTCP, Port: 33145, TargetPort: intstr.FromInt32(33145)},
		},
		// External sequence, which omits RPC.
		ServiceKindGateway: {
			{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9092, TargetPort: intstr.FromInt32(9092)},
			{Name: "http", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 8082, TargetPort: intstr.FromInt32(8082)},
		},
		// The node port is the first advertised port, else the listener port.
		ServiceKindNodePort: {
			{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9092, TargetPort: intstr.FromInt32(9092), NodePort: 31092},
			{Name: "http", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 8082, TargetPort: intstr.FromInt32(8082), NodePort: 8082},
		},
		// The port is the first advertised port, else the listener port.
		ServiceKindLoadBalancer: {
			{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 31092, TargetPort: intstr.FromInt32(9092)},
			{Name: "http", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 8082, TargetPort: intstr.FromInt32(8082)},
		},
	} {
		t.Run(string(kind), func(t *testing.T) {
			config := testServiceConfig(kind, "svc")
			require.Equal(t, expected, config.Ports())
		})
	}
}

func TestServiceConfigRender(t *testing.T) {
	brokers := []string{"broker-0", "broker-1"}

	for kind, tc := range map[ServiceKind]struct {
		brokers  []string
		expected []string
	}{
		ServiceKindHeadless:     {expected: []string{"svc"}},
		ServiceKindNodePort:     {expected: []string{"svc"}},
		ServiceKindBroker:       {brokers: brokers, expected: brokers},
		ServiceKindLoadBalancer: {brokers: brokers, expected: brokers},
		// The template is the bootstrap Service.
		ServiceKindGateway: {brokers: brokers, expected: []string{"svc", "broker-0", "broker-1"}},
	} {
		t.Run(string(kind), func(t *testing.T) {
			config := testServiceConfig(kind, "svc", tc.brokers...)

			services := config.Render()
			require.Equal(t, tc.expected, serviceNames(services))
			for _, svc := range services {
				require.Equal(t, config.Ports(), svc.Spec.Ports)
			}

			config.Listeners = Listeners{}
			require.Nil(t, config.Render())
		})
	}
}

func TestServiceConfigRenderBrokers(t *testing.T) {
	config := testServiceConfig(ServiceKindBroker, "svc")
	config.Brokers = []BrokerService{
		{Name: "broker-0", Selector: map[string]string{"pod": "0"}, Annotations: map[string]string{"dns": "0", "shared": "broker"}},
		{Name: "broker-1"},
	}

	services := config.Render()
	require.Len(t, services, 2)

	require.Equal(t, map[string]string{"app": "redpanda", "pod": "0"}, services[0].Spec.Selector)
	require.Equal(t, map[string]string{"shared": "broker", "dns": "0"}, services[0].Annotations)
	require.Equal(t, map[string]string{"app": "redpanda"}, services[1].Spec.Selector)
	require.Equal(t, map[string]string{"shared": "template"}, services[1].Annotations)

	// Services share no maps or slices with the template or each other.
	services[0].Labels["mutated"] = "true"
	services[1].Spec.Ports[0].Port = 1
	require.Equal(t, testServiceConfig(ServiceKindBroker, "svc").Template, config.Template)
	require.NotContains(t, services[1].Labels, "mutated")
	require.NotEqual(t, int32(1), services[0].Spec.Ports[0].Port)

	// Flat networks use Services without selectors.
	config.Template.Spec.Selector = nil
	require.Nil(t, config.Render()[1].Spec.Selector)
}

// testServiceConfig returns a [ServiceConfig] that publishes one listener of
// kafka, http, and rpc. Only kafka advertises ports.
func testServiceConfig(kind ServiceKind, template string, brokers ...string) ServiceConfig {
	config := ServiceConfig{
		Kind: kind,
		Listeners: NewListeners([]API{
			{Kind: KafkaAPI, Reserved: &Listener{Name: ReservedListenerName, PortName: "kafka", Port: 9092, AdvertisedPorts: []int32{31092, 31093}}},
			{Kind: HTTPAPI, Reserved: &Listener{Name: ReservedListenerName, PortName: "http", Port: 8082, AppProtocol: ptr.To("https")}},
			{Kind: RPCAPI, Reserved: &Listener{Name: ReservedListenerName, PortName: "rpc", Port: 33145}},
		}),
		Template: corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        template,
				Labels:      map[string]string{"app": "redpanda"},
				Annotations: map[string]string{"shared": "template"},
			},
			Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "redpanda"}},
		},
	}
	for _, name := range brokers {
		config.Brokers = append(config.Brokers, BrokerService{Name: name})
	}
	return config
}

func serviceNames(services []*corev1.Service) []string {
	var names []string
	for _, svc := range services {
		names = append(names, svc.Name)
	}
	return names
}
