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
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// defaultListenAddress is what every listener here binds; unlike the chart,
// there is no per-listener override.
const defaultListenAddress = "0.0.0.0"

// poolListeners resolves a pool's listeners, folding in the cluster-wide SASL
// setting that only the render state knows.
func poolListeners(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) redpanda.Listeners {
	pki := poolPKI(state, pool)
	return listenersForPool(&pool.Spec, state.Spec().Auth.IsSASLEnabled(), &pki)
}

// listenersForPool is this renderer's one translation of a pool spec into the
// shared listener set. Infallible, which is what lets the port and path
// methods share it.
func listenersForPool(spec *redpandav1alpha2.BrokerPoolSpec, saslEnabled bool, pki *redpanda.PKI) redpanda.Listeners {
	var listeners *redpandav1alpha2.StretchListeners
	if spec != nil {
		listeners = spec.Listeners
	}

	var admin, kafka, http, schemaRegistry *redpandav1alpha2.StretchAPIListener
	var rpc *redpandav1alpha2.StretchRPC
	if listeners != nil {
		admin = listeners.Admin
		kafka = listeners.Kafka
		http = listeners.HTTP
		schemaRegistry = listeners.SchemaRegistry
		rpc = listeners.RPC
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

	return redpanda.NewListeners([]redpanda.API{
		{
			Kind:        redpanda.AdminAPI,
			AppProtocol: apiAppProtocol(admin),
			// NB: published unconditionally. The probes and the sidecar dial
			// the in-cluster admin port, so gating it can only break them.
			Listeners: resolveAPIListeners(admin, spec, redpanda.AdminAPI, spec.AdminPort(), redpandav1alpha2.DefaultExternalAdminPort, "", pki, true),
		},
		{
			Kind:        redpanda.KafkaAPI,
			AppProtocol: apiAppProtocol(kafka),
			Listeners:   resolveAPIListeners(kafka, spec, redpanda.KafkaAPI, spec.KafkaPort(), redpandav1alpha2.DefaultExternalKafkaPort, kafkaAuth, pki, true),
		},
		{
			Kind:        redpanda.HTTPAPI,
			AppProtocol: apiAppProtocol(http),
			Listeners:   resolveAPIListeners(http, spec, redpanda.HTTPAPI, spec.HTTPPort(), redpandav1alpha2.DefaultExternalHTTPPort, httpAuth, pki, apiIsEnabled(http)),
		},
		{
			Kind:        redpanda.SchemaRegistryAPI,
			AppProtocol: apiAppProtocol(schemaRegistry),
			Listeners:   resolveAPIListeners(schemaRegistry, spec, redpanda.SchemaRegistryAPI, spec.SchemaRegistryPort(), redpandav1alpha2.DefaultExternalSchemaRegistryPort, "", pki, apiIsEnabled(schemaRegistry)),
		},
		{
			Kind: redpanda.RPCAPI,
			Listeners: []redpanda.Listener{{
				Name:              redpanda.InternalListenerName,
				Port:              spec.RPCPort(),
				Address:           defaultListenAddress,
				PortName:          redpanda.RPCAPI.InternalPortName(),
				ContainerPortName: redpanda.RPCAPI.InternalPortName(),
				Exposed:           true,
				// NB: RPC takes its own requireClientAuth rather than the
				// certificate's, unlike the APIs above.
				TLS: resolveListenerTLS(rpcTLS, spec, rpcTLS.RequiresClientAuth(), pki),
			}},
		},
	})
}

// resolveAPIListeners returns an API's in-cluster listener followed by every
// external one Redpanda binds, the order redpanda.yaml carries them in. One it
// does not bind is absent, not flagged.
//
// its API alone: "schemaregistry" against "schema-<name>".
func resolveAPIListeners(api *redpandav1alpha2.StretchAPIListener, spec *redpandav1alpha2.BrokerPoolSpec, kind redpanda.APIKind, port, defaultExternalPort int32, authMethod string, pki *redpanda.PKI, serviceEnabled bool) []redpanda.Listener {
	var tls *redpandav1alpha2.StretchListenerTLS
	if api != nil {
		tls = api.TLS
	}

	// NB: unconditional. Redpanda always binds it; serviceEnabled carries
	// whether the headless Service publishes it.
	listeners := []redpanda.Listener{{
		Name:                 redpanda.InternalListenerName,
		Port:                 port,
		Address:              defaultListenAddress,
		AuthenticationMethod: authMethod,
		PortName:             kind.InternalPortName(),
		ContainerPortName:    kind.InternalPortName(),
		TLS:                  resolveListenerTLS(tls, spec, certRequiresClientAuth(spec, tls), pki),
		Exposed:              serviceEnabled,
	}}

	if api == nil {
		return listeners
	}

	forEachExternal(api.External, func(name string, external *redpandav1alpha2.StretchExternalListener) {
		if !external.IsEnabled() {
			return
		}

		listeners = append(listeners, redpanda.Listener{
			Name: name,
			// NB: defaulted here rather than at each render site, so the config
			// and the Service ports cannot disagree about it.
			Port:                 external.GetPort(defaultExternalPort),
			Address:              defaultListenAddress,
			AuthenticationMethod: authMethod,
			PortName:             kind.PortName(name),
			ContainerPortName:    kind.ContainerPortName(name),
			TLS:                  resolveListenerTLS(external.TLS, spec, certRequiresClientAuth(spec, external.TLS), pki),
			PrefixTemplate:       ptrDeref(external.PrefixTemplate),
			AdvertisedPorts:      external.AdvertisedPorts,
			NodePort:             external.NodePort,
			// NB: always. This renderer has no cluster-wide external.enabled, so
			// binding and publishing are one decision where the chart splits
			// them.
			Exposed: true,
		})
	})

	return listeners
}

// resolveListenerTLS returns nil when this listener serves no TLS.
// requireClientAuth is a parameter because the APIs take the certificate's
// answer -- the OR across every listener sharing it -- while RPC takes its
// own.
func resolveListenerTLS(tls *redpandav1alpha2.StretchListenerTLS, spec *redpandav1alpha2.BrokerPoolSpec, requireClientAuth bool, pki *redpanda.PKI) *redpanda.ListenerTLS {
	certName := tls.GetCert()

	// NB: the pool's flag is checked separately because the listener's
	// overrides it rather than narrowing it -- tls.enabled true under a pool
	// with TLS off would otherwise serve TLS on a cluster that disabled it.
	if !poolTLS(spec).IsEnabled() || !tls.IsTLSEnabled(poolTLS(spec)) || certName == "" {
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

func certRequiresClientAuth(spec *redpandav1alpha2.BrokerPoolSpec, tls *redpandav1alpha2.StretchListenerTLS) bool {
	if spec == nil {
		return false
	}
	return spec.Listeners.CertRequiresClientAuth(tls.GetCert())
}

// poolTLS is spec.TLS, nil-safe on the spec itself. TLS's own methods are
// already nil-safe.
func poolTLS(spec *redpandav1alpha2.BrokerPoolSpec) *redpandav1alpha2.TLS {
	if spec == nil {
		return nil
	}
	return spec.TLS
}

// apiIsEnabled reports an API's own enabled flag, defaulting false when the API
// is absent.
//
// NB: not api.IsEnabled(). That promotes through an embedded value, so it
// panics on a nil API rather than defaulting.
func apiIsEnabled(api *redpandav1alpha2.StretchAPIListener) bool {
	return api != nil && api.IsEnabled()
}

func apiAppProtocol(api *redpandav1alpha2.StretchAPIListener) *string {
	if api == nil {
		return nil
	}
	return api.AppProtocol
}

func ptrDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// forEachExternal calls fn for every external listener in sorted-key order,
// enabled or not; callers gate for themselves.
func forEachExternal(externals map[string]*redpandav1alpha2.StretchExternalListener, fn func(string, *redpandav1alpha2.StretchExternalListener)) {
	for _, name := range sortedMapKeys(externals) {
		if external := externals[name]; external != nil {
			fn(name, external)
		}
	}
}
