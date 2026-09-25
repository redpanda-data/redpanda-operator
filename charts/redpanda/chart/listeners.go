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
			Kind:        redpanda.AdminAPI,
			AppProtocol: admin.AppProtocol,
			// NB: published unconditionally. listeners.admin.enabled has never
			// gated the headless Service's admin port, and the probes and the
			// sidecar dial it.
			Listeners: resolveAPIListeners(state, redpanda.AdminAPI, &admin, "", tls, pki, true),
		},
		{
			Kind:        redpanda.KafkaAPI,
			AppProtocol: kafka.AppProtocol,
			Listeners:   resolveAPIListeners(state, redpanda.KafkaAPI, &kafka, kafkaAuth, tls, pki, true),
		},
		{
			Kind:        redpanda.HTTPAPI,
			AppProtocol: http.AppProtocol,
			Listeners:   resolveAPIListeners(state, redpanda.HTTPAPI, &http, httpAuth, tls, pki, http.Enabled),
		},
		{
			Kind:        redpanda.SchemaRegistryAPI,
			AppProtocol: schemaRegistry.AppProtocol,
			Listeners:   resolveAPIListeners(state, redpanda.SchemaRegistryAPI, &schemaRegistry, "", tls, pki, schemaRegistry.Enabled),
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
				Exposed:           true,
			}},
		},
	})
}

// resolveAPIListeners returns an API's in-cluster listener followed by every
// external one Redpanda binds, the order redpanda.yaml carries them in. One it
// does not bind is absent, not flagged.
//
// its API alone: "schemaregistry" against "schema-<name>".
func resolveAPIListeners(state *RenderState, kind redpanda.APIKind, listener *ListenerConfig[string], defaultAuth string, tls *TLS, pki *redpanda.PKI, serviceEnabled bool) []redpanda.Listener {
	// NB: unconditional. Redpanda always binds it; serviceEnabled carries
	// whether the headless Service publishes it.
	listeners := []redpanda.Listener{{
		Name:                 redpanda.InternalListenerName,
		Port:                 listener.Port,
		Address:              ptr.Deref(listener.Address, "0.0.0.0"),
		AuthenticationMethod: ptr.Deref(listener.AuthenticationMethod, defaultAuth),
		PortName:             kind.InternalPortName(),
		ContainerPortName:    kind.InternalPortName(),
		TLS:                  resolveInternalTLS(&listener.TLS, tls, pki),
		Exposed:              serviceEnabled,
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
			TLS:                  resolveExternalTLS(external.TLS, &listener.TLS, tls, pki),
			PrefixTemplate:       ptr.Deref(external.PrefixTemplate, ""),
			AdvertisedPorts:      external.AdvertisedPorts,
			NodePort:             external.NodePort,
			Gateway:              resolveGateway(state, external),
			Exposed:              ptr.Deref(external.Enabled, state.Values.External.Enabled),
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
