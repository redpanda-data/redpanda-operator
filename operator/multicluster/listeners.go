// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// defaultListenAddress is the address of all listeners. The chart can override
// the address of each listener, but the operator cannot.
const defaultListenAddress = "0.0.0.0"

// poolListeners resolves the listeners of a pool with the SASL setting of the
// cluster.
func poolListeners(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *redpanda.Listeners {
	pki := poolPKI(state, pool)
	return listenersForPool(&pool.Spec, state.Spec().Auth.IsSASLEnabled(), &pki)
}

// listenersForPool returns the shared listener set of a pool spec.
func listenersForPool(spec *redpandav1alpha2.BrokerPoolSpec, saslEnabled bool, pki *redpanda.PKI) *redpanda.Listeners {
	var admin, kafka, http, schemaRegistry *redpandav1alpha2.StretchAPIListener
	var rpc *redpandav1alpha2.StretchRPC
	if l := spec.Listeners; l != nil {
		admin = l.Admin
		kafka = l.Kafka
		http = l.HTTP
		schemaRegistry = l.SchemaRegistry
		rpc = l.RPC
	}

	var kafkaAuth, httpAuth string
	if saslEnabled {
		kafkaAuth = "sasl"
		httpAuth = "http_basic"
	}

	var rpcTLS *redpandav1alpha2.StretchListenerTLS
	if rpc != nil {
		rpcTLS = rpc.TLS
	}

	listeners := redpanda.NewListeners([]redpanda.API{
		// NB: The headless Service always publishes the admin port, because the
		// probes and the sidecar use it.
		resolveAPIListeners(admin, spec, redpanda.AdminAPI, spec.AdminPort(), redpandav1alpha2.DefaultExternalAdminPort, "", pki, true),
		resolveAPIListeners(kafka, spec, redpanda.KafkaAPI, spec.KafkaPort(), redpandav1alpha2.DefaultExternalKafkaPort, kafkaAuth, pki, true),
		resolveAPIListeners(http, spec, redpanda.HTTPAPI, spec.HTTPPort(), redpandav1alpha2.DefaultExternalHTTPPort, httpAuth, pki, http.IsEnabled()),
		resolveAPIListeners(schemaRegistry, spec, redpanda.SchemaRegistryAPI, spec.SchemaRegistryPort(), redpandav1alpha2.DefaultExternalSchemaRegistryPort, "", pki, schemaRegistry.IsEnabled()),
		{
			Kind: redpanda.RPCAPI,
			Reserved: &redpanda.Listener{
				Name:              redpanda.ReservedListenerName,
				Port:              spec.RPCPort(),
				Address:           defaultListenAddress,
				PortName:          redpanda.RPCAPI.ReservedPortName(),
				ContainerPortName: redpanda.RPCAPI.ReservedPortName(),
				Exposed:           true,
				// NB: RPC uses its own requireClientAuth. The other APIs use the
				// flag of the certificate.
				TLS: resolveListenerTLS(rpcTLS, spec, rpcTLS.RequiresClientAuth(), pki),
			},
		},
	})

	return &listeners
}

// resolveAPIListeners returns an API with its reserved listener and the
// additional listeners that Redpanda binds.
func resolveAPIListeners(api *redpandav1alpha2.StretchAPIListener, spec *redpandav1alpha2.BrokerPoolSpec, kind redpanda.APIKind, port, defaultExternalPort int32, authMethod string, pki *redpanda.PKI, serviceEnabled bool) redpanda.API {
	var tls *redpandav1alpha2.StretchListenerTLS
	var appProtocol *string
	if api != nil {
		tls = api.TLS
		appProtocol = api.AppProtocol
	}

	resolved := redpanda.API{
		Kind:        kind,
		AppProtocol: appProtocol,
		// NB: Redpanda always binds the reserved listener. serviceEnabled
		// controls only the headless Service port.
		Reserved: &redpanda.Listener{
			Name:                 redpanda.ReservedListenerName,
			Port:                 port,
			Address:              defaultListenAddress,
			AuthenticationMethod: authMethod,
			PortName:             kind.ReservedPortName(),
			ContainerPortName:    kind.ReservedPortName(),
			TLS:                  resolveListenerTLS(tls, spec, spec.CertRequiresClientAuth(tls.GetCert()), pki),
			Exposed:              serviceEnabled,
		},
	}

	if api == nil {
		return resolved
	}

	forEachEnabledExternal(api.External, func(name string, external *redpandav1alpha2.StretchExternalListener) {
		resolved.Additional = append(resolved.Additional, redpanda.Listener{
			Name: name,
			// NB: Set the default here, thus the config and the Service ports
			// use the same port.
			Port:                 external.GetPort(defaultExternalPort),
			Address:              defaultListenAddress,
			AuthenticationMethod: authMethod,
			PortName:             kind.PortName(name),
			ContainerPortName:    kind.ContainerPortName(name),
			TLS:                  resolveListenerTLS(external.TLS, spec, spec.CertRequiresClientAuth(external.TLS.GetCert()), pki),
			PrefixTemplate:       ptr.Deref(external.PrefixTemplate, ""),
			AdvertisedPorts:      external.AdvertisedPorts,
			NodePort:             external.NodePort,
			// NB: The operator has no external.enabled setting. Thus, all
			// listeners that Redpanda binds are exposed.
			Exposed: true,
		})
	})

	return resolved
}

// resolveListenerTLS returns nil if this listener has no TLS.
func resolveListenerTLS(tls *redpandav1alpha2.StretchListenerTLS, spec *redpandav1alpha2.BrokerPoolSpec, requireClientAuth bool, pki *redpanda.PKI) *redpanda.ListenerTLS {
	certName := tls.GetCert()

	if !tls.IsTLSEnabled(spec.TLS) || certName == "" {
		return nil
	}

	return redpanda.NewListenerTLS(pki, certName, requireClientAuth, resolveTrustStore(tls.TrustStore))
}

func resolveTrustStore(trustStore *redpandav1alpha2.TrustStore) *redpanda.TrustStore {
	if trustStore == nil {
		return nil
	}

	return &redpanda.TrustStore{
		ConfigMapKeyRef: trustStore.ConfigMapKeyRef,
		SecretKeyRef:    trustStore.SecretKeyRef,
	}
}
