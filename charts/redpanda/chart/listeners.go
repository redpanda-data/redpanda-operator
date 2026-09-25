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

// resolveListeners returns the shared listener set of the chart values.
// Template expansion and validation occur elsewhere
func resolveListeners(state *RenderState, pki *redpanda.PKI) redpanda.Listeners {
	tls := &state.Values.TLS

	var kafkaAuth string
	var httpAuth string
	if state.Values.Auth.IsSASLEnabled() {
		kafkaAuth = string(SASLKafkaAuthenticationMethod)
		httpAuth = string(BasicHTTPAuthenticationMethod)
	}

	// NB: AsString converts the four authentication method types to one type.
	// Thus, one resolver is sufficient for all APIs.
	admin := state.Values.Listeners.Admin.AsString()
	kafka := state.Values.Listeners.Kafka.AsString()
	http := state.Values.Listeners.HTTP.AsString()
	schemaRegistry := state.Values.Listeners.SchemaRegistry.AsString()

	rpc := state.Values.Listeners.RPC

	return redpanda.NewListeners([]redpanda.API{
		// NB: The headless Service always publishes the admin port. The probes
		// and the sidecar use it, thus listeners.admin.enabled has no effect.
		resolveAPIListeners(state, redpanda.AdminAPI, &admin, "", tls, pki, true),
		resolveAPIListeners(state, redpanda.KafkaAPI, &kafka, kafkaAuth, tls, pki, true),
		resolveAPIListeners(state, redpanda.HTTPAPI, &http, httpAuth, tls, pki, http.Enabled),
		resolveAPIListeners(state, redpanda.SchemaRegistryAPI, &schemaRegistry, "", tls, pki, schemaRegistry.Enabled),
		{
			Kind: redpanda.RPCAPI,
			Reserved: &redpanda.Listener{
				Name:              redpanda.ReservedListenerName,
				Port:              rpc.Port,
				Address:           ptr.Deref(rpc.Address, "0.0.0.0"),
				ContainerPortName: redpanda.RPCAPI.ReservedPortName(),
				TLS:               resolveInternalTLS(&rpc.TLS, tls, pki),
				PortName:          redpanda.RPCAPI.ReservedPortName(),
				Exposed:           true,
			},
		},
	})
}

// resolveAPIListeners returns an API with its reserved listener and the
// additional listeners that Redpanda binds.
func resolveAPIListeners(state *RenderState, kind redpanda.APIKind, listener *ListenerConfig[string], defaultAuth string, tls *TLS, pki *redpanda.PKI, serviceEnabled bool) redpanda.API {
	api := redpanda.API{
		Kind:        kind,
		AppProtocol: listener.AppProtocol,
		// NB: Redpanda always binds the reserved listener. serviceEnabled
		// controls only the headless Service port.
		Reserved: &redpanda.Listener{
			Name:                 redpanda.ReservedListenerName,
			Port:                 listener.Port,
			Address:              ptr.Deref(listener.Address, "0.0.0.0"),
			AuthenticationMethod: ptr.Deref(listener.AuthenticationMethod, defaultAuth),
			PortName:             kind.ReservedPortName(),
			ContainerPortName:    kind.ReservedPortName(),
			TLS:                  resolveInternalTLS(&listener.TLS, tls, pki),
			Exposed:              serviceEnabled,
		},
	}

	for name, external := range helmette.SortedMap(listener.External) {
		if !external.IsEnabled() {
			continue
		}

		api.Additional = append(api.Additional, redpanda.Listener{
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

	return api
}

func resolveInternalTLS(internal *InternalTLS, tls *TLS, pki *redpanda.PKI) *redpanda.ListenerTLS {
	if !internal.IsEnabled(tls) {
		return nil
	}

	resolved := redpanda.NewListenerTLS(pki, internal.Cert, internal.RequireClientAuth, resolveTrustStore(internal.TrustStore))
	resolved.TrustStoreFallback = redpanda.OSTrustStorePath

	return resolved
}

// resolveExternalTLS resolves the TLS of an external listener.
func resolveExternalTLS(external *ExternalTLS, internal *InternalTLS, tls *TLS, pki *redpanda.PKI) *redpanda.ListenerTLS {
	// NB: This chart issues client keypairs only for in-cluster listeners. See
	// Listeners.InUseClientCerts. Thus, an external listener with mTLS gets
	// require_client_auth but no keypair.
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

// resolveGateway uses only the type of the listener. A tlsroute listener must
// not use the NodePort and LoadBalancer Services, thus the type alone removes
// it from these Services. To add the listener to the gateway resources, the
// render functions also use ExternalConfig.IsGatewayEnabled.
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
