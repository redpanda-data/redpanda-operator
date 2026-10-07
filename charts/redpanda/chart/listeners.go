// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_listeners.go.tpl
package chart

import (
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// resolveListeners is this chart's one translation of values into the shared
// listener set. Infallible, which is what lets the port and path methods share
// it; template expansion and validation layer on separately.
//
// Reach it through [RenderState.Listeners], which resolves once per render.
func resolveListeners(state *RenderState, pki *redpanda.PKI) redpanda.Listeners {
	tls := &state.Values.TLS

	var kafkaAuth string
	var httpAuth string
	if state.Values.Auth.IsSASLEnabled() {
		kafkaAuth = string(SASLKafkaAuthenticationMethod)
		httpAuth = string(BasicHTTPAuthenticationMethod)
	}

	// NB: AsString collapses four authentication method types into one, so a
	// single resolver covers every API.
	admin := state.Values.Listeners.Admin.AsString()
	kafka := state.Values.Listeners.Kafka.AsString()
	http := state.Values.Listeners.HTTP.AsString()
	schemaRegistry := state.Values.Listeners.SchemaRegistry.AsString()

	rpc := state.Values.Listeners.RPC

	return redpanda.NewListeners([]redpanda.API{
		{
			Kind:      redpanda.AdminAPI,
			Listeners: resolveAPIListeners(redpanda.AdminAPI, &admin, "", tls, pki),
		},
		{
			Kind:      redpanda.KafkaAPI,
			Listeners: resolveAPIListeners(redpanda.KafkaAPI, &kafka, kafkaAuth, tls, pki),
		},
		{
			Kind:      redpanda.HTTPAPI,
			Listeners: resolveAPIListeners(redpanda.HTTPAPI, &http, httpAuth, tls, pki),
		},
		{
			Kind:      redpanda.SchemaRegistryAPI,
			Listeners: resolveAPIListeners(redpanda.SchemaRegistryAPI, &schemaRegistry, "", tls, pki),
		},
		{
			Kind: redpanda.RPCAPI,
			Listeners: []redpanda.Listener{{
				Name:              redpanda.InternalListenerName,
				Port:              rpc.Port,
				Address:           ptr.Deref(rpc.Address, "0.0.0.0"),
				ContainerPortName: redpanda.RPCAPI.InternalPortName(),
				TLS:               resolveInternalTLS(&rpc.TLS, tls, pki),
				PortName:          redpanda.RPCAPI.InternalPortName(),
			}},
		},
	})
}

// resolveAPIListeners returns an API's in-cluster listener followed by every
// external one Redpanda binds, the order redpanda.yaml carries them in. One it
// does not bind is absent, not flagged.
//
// its API alone: "schemaregistry" against "schema-<name>".
func resolveAPIListeners(kind redpanda.APIKind, listener *ListenerConfig[string], defaultAuth string, tls *TLS, pki *redpanda.PKI) []redpanda.Listener {
	listeners := []redpanda.Listener{{
		Name:                 redpanda.InternalListenerName,
		Port:                 listener.Port,
		Address:              ptr.Deref(listener.Address, "0.0.0.0"),
		AuthenticationMethod: ptr.Deref(listener.AuthenticationMethod, defaultAuth),
		PortName:             kind.InternalPortName(),
		ContainerPortName:    kind.InternalPortName(),
		AppProtocol:          listener.AppProtocol,
		TLS:                  resolveInternalTLS(&listener.TLS, tls, pki),
	}}

	for name, external := range helmette.SortedMap(listener.External) {
		if !external.IsEnabled() {
			continue
		}

		listeners = append(listeners, redpanda.Listener{
			Name:                 name,
			Port:                 external.Port,
			Address:              ptr.Deref(external.Address, "0.0.0.0"),
			AuthenticationMethod: ptr.Deref(external.AuthenticationMethod, defaultAuth),
			PortName:             kind.PortName(name),
			ContainerPortName:    kind.ContainerPortName(name),
			AppProtocol:          listener.AppProtocol,
			TLS:                  resolveExternalTLS(external.TLS, &listener.TLS, tls, pki),
			PrefixTemplate:       ptr.Deref(external.PrefixTemplate, ""),
			AdvertisedPorts:      external.AdvertisedPorts,
		})
	}

	return listeners
}

func resolveInternalTLS(internal *InternalTLS, tls *TLS, pki *redpanda.PKI) *redpanda.ListenerTLS {
	if !internal.IsEnabled(tls) {
		return nil
	}

	resolved := redpanda.NewListenerTLS(pki, internal.Cert, internal.RequireClientAuth, resolveTrustStore(internal.TrustStore))
	resolved.TrustStoreFallback = redpanda.OSTrustStorePath

	return resolved
}

// resolveExternalTLS resolves an exposed listener's TLS.
//
// NB: this chart issues client keypairs for in-cluster listeners only -- see
// Listeners.InUseClientCerts -- so an external listener asking for mTLS gets
// require_client_auth but no keypair.
func resolveExternalTLS(external *ExternalTLS, internal *InternalTLS, tls *TLS, pki *redpanda.PKI) *redpanda.ListenerTLS {
	if !external.IsEnabled(internal, tls) {
		return nil
	}

	resolved := redpanda.NewListenerTLS(pki, external.GetCertName(internal), ptr.Deref(external.RequireClientAuth, false), resolveTrustStore(external.TrustStore))
	resolved.TrustStoreFallback = redpanda.OSTrustStorePath

	return resolved
}

func resolveTrustStore(trustStore *TrustStore) *redpanda.TrustStore {
	if trustStore == nil {
		return nil
	}

	return &redpanda.TrustStore{
		ConfigMapKeyRef: trustStore.ConfigMapKeyRef,
		SecretKeyRef:    trustStore.SecretKeyRef,
	}
}

// resolveGateway keys on the per-listener type alone. That is authoritative
// for *exclusion* from the NodePort/LoadBalancer Services -- a type: tlsroute
// listener must never fall back to one -- while inclusion in the gateway
// resources stays gated on ExternalConfig.IsGatewayEnabled at the render
// sites.
func resolveGateway(state *RenderState, external ExternalListener[string]) *redpanda.GatewayRoute {
	if !external.IsGatewayListener() {
		return nil
	}

	var brokerHosts []string
	if template := ptr.Deref(external.HostTemplate, ""); template != "" {
		for i, podname := range gatewayPodNames(state) {
			brokerHosts = append(brokerHosts, renderBrokerHost(template, i, podname))
		}
	}

	return &redpanda.GatewayRoute{
		Host:        ptr.Deref(external.Host, ""),
		BrokerHosts: brokerHosts,
	}
}

// resolveNetwork returns the listeners and the Services that publish them. It
// makes all decisions from the values here, so that the Services and routes
// only read the result.
//
// NB: Each external listener is enabled, because [resolveAPIListeners] skips
// the others. If external.enabled is false, there are no external Services.
// Redpanda still binds and advertises the listeners, as values.yaml specifies.
func resolveNetwork(state *RenderState, listeners *redpanda.Listeners) redpanda.Network {
	network := redpanda.Network{
		Listeners: *listeners,
		Routes:    resolveRoutes(state),
	}

	// NB: listeners.admin.enabled does not disable the admin port. The probes
	// and the sidecar connect to this port.
	internal := listeners.InCluster()
	if !state.Values.Listeners.HTTP.Enabled {
		delete(internal.ByKind, redpanda.HTTPAPI)
	}
	if !state.Values.Listeners.SchemaRegistry.Enabled {
		delete(internal.ByKind, redpanda.SchemaRegistryAPI)
	}

	var external []redpanda.API
	var gateway []redpanda.API
	allExternal := listeners.External()
	for _, kind := range slices.Sorted(maps.Keys(allExternal.ByKind)) {
		var publishedExternal []redpanda.Listener
		var publishedGateway []redpanda.Listener
		for _, listener := range allExternal.ByKind[kind].Listeners {
			if network.Route(kind, listener.Name) != nil {
				publishedGateway = append(publishedGateway, listener)
			} else {
				publishedExternal = append(publishedExternal, listener)
			}
		}

		if len(publishedExternal) > 0 {
			external = append(external, redpanda.API{Kind: kind, Listeners: publishedExternal})
		}
		if len(publishedGateway) > 0 {
			gateway = append(gateway, redpanda.API{Kind: kind, Listeners: publishedGateway})
		}
	}

	network.Services = append(network.Services, internalServiceConfig(state, internal))

	serviceType := externalServiceType(state)
	if serviceType == corev1.ServiceTypeNodePort {
		network.Services = append(network.Services, nodePortServiceConfig(state, redpanda.NewListeners(external)))
	}
	if serviceType == corev1.ServiceTypeLoadBalancer {
		network.Services = append(network.Services, loadBalancerServiceConfig(state, redpanda.NewListeners(external)))
	}

	if state.Values.External.IsGatewayEnabled() {
		network.Services = append(network.Services, gatewayServiceConfig(state, redpanda.NewListeners(gateway)))
	}

	return network
}

// externalServiceType returns the type of the external Services. It returns
// "" if there are no external Services.
func externalServiceType(state *RenderState) corev1.ServiceType {
	if !state.Values.External.Enabled || !state.Values.External.Service.Enabled {
		return ""
	}
	return state.Values.External.Type
}

// resolveRoutes returns the Gateway API routing of each enabled gateway
// listener, by API kind and listener name.
func resolveRoutes(state *RenderState) map[redpanda.APIKind]map[string]redpanda.GatewayRoute {
	routes := map[redpanda.APIKind]map[string]redpanda.GatewayRoute{}
	for _, entry := range gatewayListenerConfigs(state) {
		for name, external := range helmette.SortedMap(entry.Listeners.External) {
			if !external.IsEnabled() {
				continue
			}

			gateway := resolveGateway(state, external)
			if gateway == nil {
				continue
			}

			byName, ok := routes[entry.Kind]
			if !ok {
				byName = map[string]redpanda.GatewayRoute{}
				routes[entry.Kind] = byName
			}
			byName[name] = *gateway
		}
	}
	return routes
}
