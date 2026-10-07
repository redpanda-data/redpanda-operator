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

func TestServiceConfigPorts(t *testing.T) {
	admin := testRef{kind: AdminAPI, name: "default"}
	kafka := testRef{kind: KafkaAPI, name: "default"}

	for name, tc := range map[string]struct {
		mutate   func(*Listeners)
		refs     []testRef
		kind     ServiceKind
		expected []corev1.ServicePort
	}{
		"empty": {
			kind: ServiceKindHeadless,
		},
		// The in-cluster sequence applies, not the sequence of refs.
		"headless": {
			refs: []testRef{
				{kind: SchemaRegistryAPI, name: "default"},
				{kind: KafkaAPI, name: InternalListenerName},
			},
			kind: ServiceKindHeadless,
			expected: []corev1.ServicePort{
				{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9093, TargetPort: intstr.FromInt32(9093)},
				{Name: "schema-default", Protocol: corev1.ProtocolTCP, Port: 8084, TargetPort: intstr.FromInt32(8084)},
			},
		},
		"app protocol": {
			mutate: func(l *Listeners) { l.Admin().Listeners[0].AppProtocol = ptr.To("https") },
			refs:   []testRef{{kind: AdminAPI, name: InternalListenerName}},
			kind:   ServiceKindBroker,
			expected: []corev1.ServicePort{
				{Name: "admin", Protocol: corev1.ProtocolTCP, AppProtocol: ptr.To("https"), Port: 9644, TargetPort: intstr.FromInt32(9644)},
			},
		},
		// The external sequence applies.
		"gateway": {
			refs: []testRef{kafka, admin},
			kind: ServiceKindGateway,
			expected: []corev1.ServicePort{
				{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645)},
				{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 9094, TargetPort: intstr.FromInt32(9094)},
			},
		},
		// The Service publishes the port of the listener. The node port is the
		// first advertised port.
		"nodeport": {
			refs: []testRef{admin, kafka},
			kind: ServiceKindNodePort,
			expected: []corev1.ServicePort{
				{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645), NodePort: 31644},
				{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 9094, TargetPort: intstr.FromInt32(9094), NodePort: 31092},
			},
		},
		"nodeport without advertised ports": {
			mutate: func(l *Listeners) { l.Admin().Listeners[1].AdvertisedPorts = nil },
			refs:   []testRef{admin},
			kind:   ServiceKindNodePort,
			expected: []corev1.ServicePort{
				{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645), NodePort: 9645},
			},
		},
		// The Service publishes the first advertised port, because the listener
		// advertises this port.
		"loadbalancer": {
			refs: []testRef{admin, kafka},
			kind: ServiceKindLoadBalancer,
			expected: []corev1.ServicePort{
				{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 31644, TargetPort: intstr.FromInt32(9645)},
				{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 31092, TargetPort: intstr.FromInt32(9094)},
			},
		},
		"loadbalancer without advertised ports": {
			mutate: func(l *Listeners) { l.Admin().Listeners[1].AdvertisedPorts = nil },
			refs:   []testRef{admin},
			kind:   ServiceKindLoadBalancer,
			expected: []corev1.ServicePort{
				{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645)},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := testListeners()
			if tc.mutate != nil {
				tc.mutate(&l)
			}

			config := ServiceConfig{Kind: tc.kind, Listeners: resolve(&l, tc.refs...), Template: testServiceTemplate()}
			require.Equal(t, tc.expected, config.Ports())
		})
	}
}

func TestServiceConfigRender(t *testing.T) {
	brokers := []BrokerService{{Name: "broker-0"}, {Name: "broker-1"}}

	for name, tc := range map[string]struct {
		kind     ServiceKind
		brokers  []BrokerService
		expected []string
	}{
		"headless":     {kind: ServiceKindHeadless, expected: []string{"svc"}},
		"nodeport":     {kind: ServiceKindNodePort, expected: []string{"svc"}},
		"broker":       {kind: ServiceKindBroker, brokers: brokers, expected: []string{"broker-0", "broker-1"}},
		"loadbalancer": {kind: ServiceKindLoadBalancer, brokers: brokers, expected: []string{"broker-0", "broker-1"}},
		// The template Service is the bootstrap Service.
		"gateway": {kind: ServiceKindGateway, brokers: brokers, expected: []string{"svc", "broker-0", "broker-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			l := testListeners()
			config := ServiceConfig{
				Kind:      tc.kind,
				Listeners: resolve(&l, testRef{kind: KafkaAPI, name: "default"}),
				Template:  testServiceTemplate(),
				Brokers:   tc.brokers,
			}

			var names []string
			for _, svc := range config.Render() {
				names = append(names, svc.Name)
				require.Equal(t, config.Ports(), svc.Spec.Ports)
			}
			require.Equal(t, tc.expected, names)

			// No listeners gives no Services.
			config.Listeners = Listeners{}
			require.Nil(t, config.Render())
		})
	}
}

func TestServiceConfigRenderBrokers(t *testing.T) {
	l := testListeners()
	config := ServiceConfig{
		Kind:      ServiceKindBroker,
		Listeners: resolve(&l, testRef{kind: KafkaAPI, name: "default"}),
		Template:  testServiceTemplate(),
		Brokers: []BrokerService{
			{Name: "broker-0", Selector: map[string]string{"pod": "0"}, Annotations: map[string]string{"dns": "0", "shared": "broker"}},
			{Name: "broker-1", Selector: map[string]string{"pod": "1"}},
		},
	}

	services := config.Render()
	require.Len(t, services, 2)

	require.Equal(t, "broker-0", services[0].Name)
	require.Equal(t, map[string]string{"app": "redpanda", "pod": "0"}, services[0].Spec.Selector)
	require.Equal(t, map[string]string{"shared": "broker", "dns": "0"}, services[0].Annotations)

	require.Equal(t, "broker-1", services[1].Name)
	require.Equal(t, map[string]string{"app": "redpanda", "pod": "1"}, services[1].Spec.Selector)
	require.Equal(t, map[string]string{"shared": "template"}, services[1].Annotations)

	// The Services do not share maps or ports with the template or with each
	// other.
	services[0].Labels["mutated"] = "true"
	services[1].Spec.Ports[0].Port = 1
	require.Equal(t, testServiceTemplate(), config.Template)
	require.NotContains(t, services[1].Labels, "mutated")
	require.Equal(t, int32(9094), services[0].Spec.Ports[0].Port)

	// A nil selector in the template and the broker gives a nil selector. Flat
	// networks use Services that have no selector.
	config.Template.Spec.Selector = nil
	config.Brokers = []BrokerService{{Name: "broker-0"}}
	services = config.Render()
	require.Nil(t, services[0].Spec.Selector)
}

func TestNetworkRoute(t *testing.T) {
	network := Network{Routes: map[APIKind]map[string]GatewayRoute{
		SchemaRegistryAPI: {"default": {Host: "schema.example.com"}},
	}}

	require.Equal(t, &GatewayRoute{Host: "schema.example.com"}, network.Route(SchemaRegistryAPI, "default"))
	require.Nil(t, network.Route(SchemaRegistryAPI, "other"))
	require.Nil(t, network.Route(KafkaAPI, "default"))
}

type testRef struct {
	kind APIKind
	name string
}

// resolve returns the listeners of refs.
func resolve(l *Listeners, refs ...testRef) Listeners {
	byKind := map[APIKind][]Listener{}
	for _, ref := range refs {
		for _, listener := range l.ByKind[ref.kind].Listeners {
			if listener.Name == ref.name {
				byKind[ref.kind] = append(byKind[ref.kind], listener)
			}
		}
	}

	var apis []API
	for kind, listeners := range byKind {
		apis = append(apis, API{Kind: kind, Listeners: listeners})
	}
	return NewListeners(apis)
}

func testServiceTemplate() corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc",
			Labels:      map[string]string{"app": "redpanda"},
			Annotations: map[string]string{"shared": "template"},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "redpanda"},
		},
	}
}
