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
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Network contains the listeners of a cluster and the Services that publish
// them.
type Network struct {
	// Listeners contains every listener that Redpanda binds.
	Listeners Listeners

	// Services publish a subset of Listeners. A listener that no Service
	// publishes is still bound and advertised. For example, a disabled API
	// keeps its reserved listener, but the headless Service omits it.
	Services []ServiceConfig

	// Routes contains the Gateway API routing of each gateway listener, by API
	// kind and listener name.
	Routes map[APIKind]map[string]GatewayRoute
}

// Service returns the first [ServiceConfig] of kind. It returns nil if there
// is none.
func (n *Network) Service(kind ServiceKind) *ServiceConfig {
	for _, config := range n.Services {
		if config.Kind == kind {
			return &config
		}
	}
	return nil
}

// Render returns the Services of each [ServiceConfig] of kind.
func (n *Network) Render(kind ServiceKind) []*corev1.Service {
	var services []*corev1.Service
	for _, config := range n.Services {
		if config.Kind == kind {
			services = append(services, config.Render()...)
		}
	}
	return services
}

// Route returns the Gateway API routing of the listener name of kind. It
// returns nil if the listener does not use Gateway API.
func (n *Network) Route(kind APIKind, name string) *GatewayRoute {
	byName, ok := n.Routes[kind]
	if !ok {
		return nil
	}
	route, ok := byName[name]
	if !ok {
		return nil
	}
	return &route
}

// GatewayRoute is the Gateway API routing of a listener. The hostnames are
// fully rendered.
type GatewayRoute struct {
	// Host is the SNI name of the bootstrap TLSRoute.
	Host string

	// BrokerHosts contains one SNI name for each broker, in the sequence of the
	// global ordinals.
	BrokerHosts []string
}

// ServiceKind identifies a type of [ServiceConfig]. It sets the number of
// Services, the sequence of their ports, and how each port is calculated. The
// target port is always the port of the listener.
type ServiceKind string

const (
	// ServiceKindHeadless is one Service. It publishes the port of each
	// listener.
	ServiceKindHeadless ServiceKind = "headless"
	// ServiceKindBroker is one Service for each broker. It publishes the port
	// of each listener.
	ServiceKindBroker ServiceKind = "broker"
	// ServiceKindNodePort is one Service. It publishes the port of each
	// listener. The node port is the first advertised port. If there are no
	// advertised ports, the node port is the port of the listener.
	ServiceKindNodePort ServiceKind = "nodeport"
	// ServiceKindLoadBalancer is one Service for each broker. It publishes the
	// first advertised port of each listener. If there are no advertised ports,
	// it publishes the port of the listener. This agrees with
	// [Listener.AdvertisedPort].
	ServiceKindLoadBalancer ServiceKind = "loadbalancer"
	// ServiceKindGateway is the template Service, which is the bootstrap
	// Service, and one Service for each broker. It publishes the port of each
	// listener.
	ServiceKindGateway ServiceKind = "gateway"
)

// order returns the sequence of the APIs in the ports of k.
func (k ServiceKind) order() []APIKind {
	// NB: The in-cluster sequence is the sequence of
	// [Listeners.ContainerPorts].
	if k == ServiceKindHeadless || k == ServiceKindBroker {
		return []APIKind{AdminAPI, HTTPAPI, KafkaAPI, RPCAPI, SchemaRegistryAPI}
	}
	return []APIKind{AdminAPI, KafkaAPI, HTTPAPI, SchemaRegistryAPI}
}

// rendersTemplate reports whether k renders the template as a Service.
func (k ServiceKind) rendersTemplate() bool {
	return k == ServiceKindHeadless || k == ServiceKindNodePort || k == ServiceKindGateway
}

// ServiceConfig is the configuration of the Services of one [ServiceKind].
//
// ServiceConfig is a tagged union on Kind. Ideally, it is an interface with
// one implementation for each [ServiceKind]. gotohelm does not support
// interfaces that have methods.
type ServiceConfig struct {
	Kind ServiceKind

	// Listeners contains the listeners that the Services publish.
	Listeners Listeners

	// Template is the Service that each Service copies. Render replaces its
	// ports.
	Template corev1.Service

	// Brokers contains one entry for each broker. It is empty for
	// [ServiceKindHeadless] and [ServiceKindNodePort].
	Brokers []BrokerService
}

// BrokerService contains the values that make the Service of one broker
// different from the template.
type BrokerService struct {
	Name string

	// Selector and Annotations add to the values of the template. A value here
	// replaces a template value that has the same key.
	Selector    map[string]string
	Annotations map[string]string
}

// Render returns the Services of c. It returns nil if c publishes no
// listeners.
func (c *ServiceConfig) Render() []*corev1.Service {
	ports := c.Ports()
	if len(ports) == 0 {
		return nil
	}

	base := c.Template.DeepCopy()
	base.Spec.Ports = ports

	var services []*corev1.Service
	if c.Kind.rendersTemplate() {
		services = append(services, base)
	}

	for _, broker := range c.Brokers {
		svc := base.DeepCopy()
		svc.ObjectMeta.Name = broker.Name

		// NB: maps.Copy panics on a nil destination. Make the maps only when
		// there is a value to copy, so that a nil selector stays nil.
		if len(broker.Annotations) > 0 && svc.ObjectMeta.Annotations == nil {
			svc.ObjectMeta.Annotations = map[string]string{}
		}
		if len(broker.Selector) > 0 && svc.Spec.Selector == nil {
			svc.Spec.Selector = map[string]string{}
		}
		maps.Copy(svc.ObjectMeta.Annotations, broker.Annotations)
		maps.Copy(svc.Spec.Selector, broker.Selector)

		services = append(services, svc)
	}

	return services
}

// Ports returns the ports of each Service of c.
func (c *ServiceConfig) Ports() []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, listener := range c.Listeners.ListenersInOrder(c.Kind.order()) {
		port := corev1.ServicePort{
			Name:        listener.PortName,
			Protocol:    corev1.ProtocolTCP,
			AppProtocol: listener.AppProtocol,
			Port:        listener.Port,
			TargetPort:  intstr.FromInt32(listener.Port),
		}

		if c.Kind == ServiceKindNodePort {
			port.NodePort = listener.Port
			if len(listener.AdvertisedPorts) > 0 {
				port.NodePort = listener.AdvertisedPorts[0]
			}
		} else if c.Kind == ServiceKindLoadBalancer {
			if len(listener.AdvertisedPorts) > 0 {
				port.Port = listener.AdvertisedPorts[0]
			}
		}

		ports = append(ports, port)
	}

	return ports
}
