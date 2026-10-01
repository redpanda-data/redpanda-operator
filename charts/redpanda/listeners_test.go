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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// testListeners is one fixture shared by most tests below, so a change in one
// render path shows up as a diff against the same input every other path sees.
//
// admin  -- in-cluster TLS with a CA and mTLS, one plain exposed listener
// kafka  -- SASL on both, exposed listener with two advertised ports
// http   -- no TLS, in-cluster listener not published by the headless Service
// schema -- one exposed listener that opted into Gateway API
// rpc    -- single valued, TLS via an explicit truststore
func testListeners() Listeners {
	pki := testPKI()

	return NewListeners([]API{
		{
			Kind: AdminAPI,
			Listeners: []Listener{
				{
					Name:              InternalListenerName,
					Port:              9644,
					Address:           "0.0.0.0",
					ContainerPortName: "admin",
					PortName:          "admin",
					Exposed:           true,
					TLS:               chartTLS(&pki, "default", true, nil),
				},
				{
					Name:              "default",
					Port:              9645,
					Address:           "0.0.0.0",
					ContainerPortName: "admin-default",
					PortName:          "admin-default",
					Exposed:           true,
					AdvertisedPorts:   []int32{31644},
					TLS:               chartTLS(&pki, "external", false, nil),
				},
			},
		},
		{
			Kind: KafkaAPI,
			Listeners: []Listener{
				{
					Name:                 InternalListenerName,
					Port:                 9093,
					Address:              "0.0.0.0",
					AuthenticationMethod: "sasl",
					ContainerPortName:    "kafka",
					PortName:             "kafka",
					Exposed:              true,
				},
				{
					Name:                 "default",
					Port:                 9094,
					Address:              "0.0.0.0",
					AuthenticationMethod: "sasl",
					ContainerPortName:    "kafka-default",
					PortName:             "kafka-default",
					NodePort:             ptr.To[int32](32092),
					Exposed:              true,
					AdvertisedPorts:      []int32{31092, 31093},
				},
			},
		},
		{
			Kind: HTTPAPI,
			Listeners: []Listener{{
				Name:              InternalListenerName,
				Port:              8082,
				Address:           "0.0.0.0",
				ContainerPortName: "http",
				// Redpanda binds it and the container declares its port; no
				// exposure, so the headless Service does not publish it.
			}},
		},
		{
			Kind: SchemaRegistryAPI,
			Listeners: []Listener{
				{
					Name:    InternalListenerName,
					Port:    8081,
					Address: "0.0.0.0",
					// NB: named for its API alone, and that name is not the
					// API's own -- "schemaregistry" against a "schema" prefix.
					ContainerPortName: "schemaregistry",
					PortName:          "schemaregistry",
					Exposed:           true,
				},
				{
					Name:              "default",
					Port:              8084,
					Address:           "0.0.0.0",
					ContainerPortName: "schema-default",
					Gateway: &GatewayRoute{
						Host:        "schema.example.com",
						BrokerHosts: []string{"schema-0.example.com"},
					},
					PortName: "schema-default",
					Exposed:  true,
				},
			},
		},
		{
			Kind: RPCAPI,
			Listeners: []Listener{{
				Name:              InternalListenerName,
				Port:              33145,
				Address:           "0.0.0.0",
				ContainerPortName: "rpc",
				PortName:          "rpc",
				Exposed:           true,
				TLS:               chartTLS(&pki, "default", false, &TrustStore{ConfigMapKeyRef: cmKeyRef("rpc-ca", "ca.crt")}),
			}},
		},
	})
}

// chartTLS is [NewListenerTLS] with the chart's truststore fallback, which the
// fixture models. The operator leaves it empty.
func chartTLS(pki *PKI, cert string, requireClientAuth bool, trustStore *TrustStore) *ListenerTLS {
	tls := NewListenerTLS(pki, cert, requireClientAuth, trustStore)
	tls.TrustStoreFallback = OSTrustStorePath
	return tls
}

func testPKI() PKI {
	return PKI{Certificates: map[string]Certificate{
		"default": {
			Server: Keypair{Name: "default", CA: ptr.To("ca.crt")},
			Client: &Keypair{Name: "default-client"},
		},
		// No CA: exercises both truststore fallbacks.
		"external": {Server: Keypair{Name: "external"}},
	}}
}

func cmKeyRef(name, key string) *corev1.ConfigMapKeySelector {
	return &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
}

func secretKeyRef(name, key string) *corev1.SecretKeySelector {
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
}

func configKeys(apis []*API) []string {
	var keys []string
	for _, api := range apis {
		keys = append(keys, api.Kind.ConfigKey())
	}
	return keys
}

func portNames(ports []corev1.ContainerPort) []string {
	var names []string
	for _, p := range ports {
		names = append(names, p.Name)
	}
	return names
}

func sortedMapKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// TestAccessorOrders pins all four. They disagree, and aligning any two
// reshuffles Service ports, rolls every broker, or rotates certificates.
func TestAccessorOrders(t *testing.T) {
	l := testListeners()

	require.Equal(t, []string{"admin", "kafka_api", "pandaproxy_api", "schema_registry_api"}, configKeys(l.APIs()))
	require.Equal(t, []string{"kafka_api", "admin", "pandaproxy_api", "schema_registry_api", "rpc_server"}, configKeys(l.All()))
	require.Equal(t, []string{"admin", "pandaproxy_api", "kafka_api", "rpc_server", "schema_registry_api"}, configKeys(l.Ports()))
	require.Equal(t, []string{"kafka_api", "pandaproxy_api", "admin", "schema_registry_api"}, configKeys(l.Gateways()))

	// rpc_server is not an *_api key, so only All() carries it.
	require.NotContains(t, configKeys(l.APIs()), "rpc_server")
	require.NotContains(t, configKeys(l.Gateways()), "rpc_server")
	require.Len(t, l.Ports(), 5)
}

// TestInClusterAndExternal pins the two derived views over the one list. The
// in-cluster listener is found by name, not position or a field.
func TestInClusterAndExternal(t *testing.T) {
	l := testListeners()
	admin := l.Admin()

	require.Len(t, admin.Listeners, 2)

	inCluster := admin.InCluster()
	require.Equal(t, InternalListenerName, inCluster.Name)
	require.Equal(t, int32(9644), inCluster.Port)

	external := admin.External()
	require.Len(t, external, 1)
	require.Equal(t, "default", external[0].Name)

	// Found by name, so ordering the list differently does not change it.
	reordered := l.Admin()
	reordered.Listeners = []Listener{reordered.Listeners[1], reordered.Listeners[0]}
	require.Equal(t, InternalListenerName, reordered.InCluster().Name)
	require.Equal(t, int32(9644), reordered.InCluster().Port)

	// An API with no in-cluster listener yields nil, so a caller that built one
	// by hand panics rather than rendering a zero-valued listener on port 0.
	var empty API
	require.Nil(t, empty.InCluster())
	require.Empty(t, empty.External())

	http := l.HTTP()
	require.Empty(t, http.External())
	require.Equal(t, int32(8082), http.InCluster().Port)
}

// TestConfigSections pins the whole rendered map: which section each API nests
// under, and that a single valued API carries no "name".
func TestConfigSections(t *testing.T) {
	l := testListeners()
	sections := l.ConfigSections()

	require.Equal(t, []string{"pandaproxy", "redpanda", "schema_registry"}, sortedMapKeys(sections))

	require.Equal(t, map[string]any{
		"admin": []map[string]any{
			{"name": "internal", "address": "0.0.0.0", "port": int32(9644)},
			{"name": "default", "address": "0.0.0.0", "port": int32(9645)},
		},
		"admin_api_tls": []map[string]any{
			{
				"name":                "internal",
				"enabled":             true,
				"cert_file":           "/etc/tls/certs/default/tls.crt",
				"key_file":            "/etc/tls/certs/default/tls.key",
				"require_client_auth": true,
				"truststore_file":     "/etc/tls/certs/default/ca.crt",
			},
			{
				"name":      "default",
				"enabled":   true,
				"cert_file": "/etc/tls/certs/external/tls.crt",
				"key_file":  "/etc/tls/certs/external/tls.key",
				// Per-listener, and the "external" cert ships no CA, so this
				// falls all the way through to the supplied fallback.
				"require_client_auth": false,
				"truststore_file":     OSTrustStorePath,
			},
		},
		"kafka_api": []map[string]any{
			{"name": "internal", "address": "0.0.0.0", "port": int32(9093), "authentication_method": "sasl"},
			{"name": "default", "address": "0.0.0.0", "port": int32(9094), "authentication_method": "sasl"},
		},
		// No listener serves TLS, so kafka_api_tls is absent entirely.
		"rpc_server": map[string]any{"address": "0.0.0.0", "port": int32(33145)},
		"rpc_server_tls": map[string]any{
			"enabled":             true,
			"cert_file":           "/etc/tls/certs/default/tls.crt",
			"key_file":            "/etc/tls/certs/default/tls.key",
			"require_client_auth": false,
			"truststore_file":     "/etc/truststores/configmaps/rpc-ca-ca.crt",
		},
	}, sections["redpanda"])

	// A single valued API renders a bare map with no "name" key at all --
	// redpanda's rpc_server takes one listener, not a named list.
	rpcTLS := sections["redpanda"]["rpc_server_tls"].(map[string]any)
	require.NotContains(t, rpcTLS, "name")

	// EmitNilTLSKey is false for rpc, so an rpc listener serving no TLS omits
	// the key rather than nulling it.
	plainRPC := testListeners()
	plainRPC.RPC().Listeners[0].TLS = nil
	require.NotContains(t, plainRPC.ConfigSections()["redpanda"], "rpc_server_tls")

	require.Equal(t, map[string]any{
		"pandaproxy_api": []map[string]any{
			{"name": "internal", "address": "0.0.0.0", "port": int32(8082)},
		},
	}, sections["pandaproxy"])

	require.Equal(t, map[string]any{
		"schema_registry_api": []map[string]any{
			{"name": "internal", "address": "0.0.0.0", "port": int32(8081)},
			{"name": "default", "address": "0.0.0.0", "port": int32(8084)},
		},
	}, sections["schema_registry"])
}

// TestConfigSectionsGating asserts Exposed does not reach the config. Whether
// Redpanda binds a listener is answered by its presence in the list.
func TestConfigSectionsGating(t *testing.T) {
	unpublished := testListeners()
	unpublished.Admin().Listeners[1].Exposed = false
	entries := unpublished.ConfigSections()["redpanda"]["admin"].([]map[string]any)
	require.Len(t, entries, 2, "an unpublished listener is still bound by Redpanda")

	unbound := testListeners()
	unbound.Admin().Listeners = unbound.Admin().Listeners[:1]
	entries = unbound.ConfigSections()["redpanda"]["admin"].([]map[string]any)
	require.Len(t, entries, 1)
	require.Equal(t, "internal", entries[0]["name"])

	// Its TLS entry goes with it.
	tlsEntries := unbound.ConfigSections()["redpanda"]["admin_api_tls"].([]map[string]any)
	require.Len(t, tlsEntries, 1)
}

// TestAPIConfigKeys pins the five keys each [APIKind] derives. The irregular
// pairs are the point: admin's bare listener key against its admin_api_tls
// twin, and the two APIs advertised at all.
func TestAPIConfigKeys(t *testing.T) {
	for _, tc := range []struct {
		kind              APIKind
		configKey         string
		tlsConfigKey      string
		section           string
		advertisedKey     string
		advertisedSection string
	}{
		{AdminAPI, "admin", "admin_api_tls", "redpanda", "", ""},
		{KafkaAPI, "kafka_api", "kafka_api_tls", "redpanda", "advertised_kafka_api", "redpanda"},
		{HTTPAPI, "pandaproxy_api", "pandaproxy_api_tls", "pandaproxy", "advertised_pandaproxy_api", "pandaproxy"},
		{SchemaRegistryAPI, "schema_registry_api", "schema_registry_api_tls", "schema_registry", "", ""},
		{RPCAPI, "rpc_server", "rpc_server_tls", "redpanda", "", ""},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			require.Equal(t, tc.configKey, tc.kind.ConfigKey())
			require.Equal(t, tc.tlsConfigKey, tc.kind.TLSConfigKey())
			require.Equal(t, tc.section, tc.kind.ConfigSection())
			require.Equal(t, tc.advertisedKey, tc.kind.AdvertisedConfigKey())
			require.Equal(t, tc.advertisedSection, tc.kind.AdvertisedConfigSection())
		})
	}
}

// TestContainerPorts pins the port order. Container ports are part of the pod
// template, so a change here is a broker roll.
func TestContainerPorts(t *testing.T) {
	l := testListeners()

	require.Equal(t, []corev1.ContainerPort{
		{Name: "admin", ContainerPort: 9644},
		{Name: "admin-default", ContainerPort: 9645},
		{Name: "http", ContainerPort: 8082},
		{Name: "kafka", ContainerPort: 9093},
		{Name: "kafka-default", ContainerPort: 9094},
		{Name: "rpc", ContainerPort: 33145},
		{Name: "schemaregistry", ContainerPort: 8081},
		{Name: "schema-default", ContainerPort: 8084},
	}, l.ContainerPorts())

	// One port per listener, not per exposure: Redpanda binds the port, so the
	// container declares it whether or not anything publishes it. http proves
	// it -- no exposures, still a port.
	unpublished := testListeners()
	unpublished.Admin().Listeners[1].Exposed = false
	require.Contains(t, portNames(unpublished.ContainerPorts()), "admin-default")
}

// TestServicePorts renders all five formulas from one fixture. They disagree;
// unifying any two moves goldens on one path or the other.
func TestServicePorts(t *testing.T) {
	l := testListeners()

	// Headless Service: in-cluster listeners only, one port per exposure.
	// http has none, so it is absent while still being bound.
	require.Equal(t, []corev1.ServicePort{
		{Name: "admin", Protocol: corev1.ProtocolTCP, Port: 9644, TargetPort: intstr.FromInt32(9644)},
		{Name: "kafka", Protocol: corev1.ProtocolTCP, Port: 9093, TargetPort: intstr.FromInt32(9093)},
		{Name: "rpc", Protocol: corev1.ProtocolTCP, Port: 33145, TargetPort: intstr.FromInt32(33145)},
		{Name: "schemaregistry", Protocol: corev1.ProtocolTCP, Port: 8081, TargetPort: intstr.FromInt32(8081)},
	}, l.InternalServicePorts())

	// NodePort: the listener's own port, published at the first advertised
	// port. The gateway listener is excluded.
	require.Equal(t, []corev1.ServicePort{
		{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645), NodePort: 31644},
		{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 9094, TargetPort: intstr.FromInt32(9094), NodePort: 31092},
	}, l.NodePortServicePorts())

	// LoadBalancer: nodePort wins, then the first advertised port, then the
	// API's in-cluster port. admin has no nodePort so it advertises; kafka's
	// nodePort overrides its advertised ports.
	require.Equal(t, []corev1.ServicePort{
		{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 31644, TargetPort: intstr.FromInt32(9645)},
		{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 32092, TargetPort: intstr.FromInt32(9094)},
	}, l.LoadBalancerServicePorts())

	// The operator's LoadBalancer publishes the listener's bound port where the
	// chart's publishes the advertised one. The only surviving difference
	// between the two formulas.
	require.Equal(t, []corev1.ServicePort{
		{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645)},
		{Name: "kafka-default", Protocol: corev1.ProtocolTCP, Port: 9094, TargetPort: intstr.FromInt32(9094)},
	}, l.ExternalServicePorts())

	// Gateway Services carry only the listeners that opted in, and in APIs()
	// order rather than Gateways().
	require.Equal(t, []corev1.ServicePort{
		{Name: "schema-default", Protocol: corev1.ProtocolTCP, Port: 8084, TargetPort: intstr.FromInt32(8084)},
	}, l.GatewayServicePorts())
}

// TestLoadBalancerPortFallback walks the three-step chain on its own, since the
// shared fixture only exercises two of the steps.
func TestLoadBalancerPortFallback(t *testing.T) {
	base := testListeners()
	base.Admin().Listeners[1].NodePort = nil
	base.Admin().Listeners[1].AdvertisedPorts = nil

	// Neither set: the API's in-cluster port, not the exposed listener's.
	require.Equal(t, int32(9644), base.LoadBalancerServicePorts()[0].Port)
	require.Equal(t, intstr.FromInt32(9645), base.LoadBalancerServicePorts()[0].TargetPort)

	advertised := testListeners()
	advertised.Admin().Listeners[1].NodePort = nil
	require.Equal(t, int32(31644), advertised.LoadBalancerServicePorts()[0].Port)

	nodePort := testListeners()
	nodePort.Admin().Listeners[1].NodePort = ptr.To[int32](30001)
	require.Equal(t, int32(30001), nodePort.LoadBalancerServicePorts()[0].Port)
}

// TestServicePortGating asserts the two questions stay apart. Exposed drives
// every Service port method at once, while the listener stays bound and still
// advertised -- what values.yaml's external.enabled false promises.
func TestServicePortGating(t *testing.T) {
	unpublished := testListeners()
	unpublished.Admin().Listeners[1].Exposed = false
	unpublished.SchemaRegistry().Listeners[1].Exposed = false

	require.Len(t, unpublished.NodePortServicePorts(), 1)
	require.Len(t, unpublished.ExternalServicePorts(), 1)
	require.Len(t, unpublished.LoadBalancerServicePorts(), 1)
	require.Empty(t, unpublished.GatewayServicePorts())
	require.Contains(t, portNames(unpublished.ContainerPorts()), "admin-default")

	// Still bound, still advertised at the port it would have been published
	// on, and the container still declares it. That is the half exposure does
	// not reach.
	entries := unpublished.ConfigSections()["redpanda"]["admin"].([]map[string]any)
	require.Len(t, entries, 2)
	require.Equal(t, int32(31644), unpublished.Admin().Listeners[1].AdvertisedPort(0))

	// A listener Redpanda does not bind is absent instead, which drops the
	// config entry as well.
	unbound := testListeners()
	unbound.Admin().Listeners = unbound.Admin().Listeners[:1]

	entries = unbound.ConfigSections()["redpanda"]["admin"].([]map[string]any)
	require.Len(t, entries, 1)
	require.Len(t, unbound.NodePortServicePorts(), 1)
}

// TestTrustStoreFile pins the fallback split. Conflating the two makes a broker
// with caEnabled false trust every public CA, or a client trust none.
func TestTrustStoreFile(t *testing.T) {
	pki := testPKI()

	explicit := chartTLS(&pki, "external", false, &TrustStore{SecretKeyRef: secretKeyRef("my-ca", "ca.pem")})
	// An explicit truststore wins over everything, for both answers.
	require.Equal(t, "/etc/truststores/secrets/my-ca-ca.pem", explicit.TrustStoreFile())
	require.Equal(t, "/etc/truststores/secrets/my-ca-ca.pem", explicit.ServerCAFile())

	withCA := chartTLS(&pki, "default", false, nil)
	// The certificate's own CA is next, again for both.
	require.Equal(t, "/etc/tls/certs/default/ca.crt", withCA.TrustStoreFile())
	require.Equal(t, "/etc/tls/certs/default/ca.crt", withCA.ServerCAFile())

	// A certificate shipping no CA is the only place TrustStoreFallback is
	// reached, and the only place the two answers diverge.
	noCA := chartTLS(&pki, "external", false, nil)
	require.Equal(t, OSTrustStorePath, noCA.TrustStoreFile())
	require.Equal(t, "/etc/tls/certs/external/tls.crt", noCA.ServerCAFile())

	// Empty is the operator's setting and the zero value: both answers are the
	// serving certificate, so a listener trusts almost no one rather than every
	// public CA.
	failClosed := NewListenerTLS(&pki, "external", false, nil)
	require.Equal(t, "/etc/tls/certs/external/tls.crt", failClosed.TrustStoreFile())
	require.Equal(t, "/etc/tls/certs/external/tls.crt", failClosed.ServerCAFile())
}

// TestTrustStores asserts RPC is included. The chart writes
// rpc_server_tls.truststore_file but its aggregate skipped RPC, so nothing
// projected the file the broker was told to read.
func TestTrustStores(t *testing.T) {
	l := testListeners()
	pki := testPKI()
	l.Kafka().Listeners[0].TLS = chartTLS(&pki, "default", false, &TrustStore{SecretKeyRef: secretKeyRef("kafka-ca", "ca.crt")})

	stores := l.TrustStores()
	require.Len(t, stores, 2)

	// All() order: kafka before rpc.
	require.Equal(t, "/etc/truststores/secrets/kafka-ca-ca.crt", stores[0].AbsolutePath())
	require.Equal(t, "/etc/truststores/configmaps/rpc-ca-ca.crt", stores[1].AbsolutePath())

	// A listener serving no TLS projects nothing, truststore or not.
	l.RPC().Listeners[0].TLS = nil
	require.Len(t, l.TrustStores(), 1)

	var none Listeners
	require.Empty(t, none.TrustStores())
	require.Nil(t, none.TrustStoreVolume())
	require.Nil(t, none.TrustStoreMount())
}

// TestTrustStoreVolume pins the source ordering -- all ConfigMap projections
// name-sorted, then all Secret ones -- and the per-source key dedup. Pod
// templates merge by volume name, so the order is observable.
func TestTrustStoreVolume(t *testing.T) {
	l := testListeners()
	pki := testPKI()
	l.Kafka().Listeners[0].TLS = chartTLS(&pki, "default", false, &TrustStore{SecretKeyRef: secretKeyRef("secret-b", "one.crt")})
	l.Admin().Listeners[0].TLS.TrustStore = &TrustStore{ConfigMapKeyRef: cmKeyRef("cm-a", "two.crt")}
	// Same ConfigMap and key as rpc's: deduplicated into one item.
	l.SchemaRegistry().Listeners[0].TLS = chartTLS(&pki, "default", false, &TrustStore{ConfigMapKeyRef: cmKeyRef("rpc-ca", "ca.crt")})

	vol := l.TrustStoreVolume()
	require.NotNil(t, vol)
	require.Equal(t, "truststores", vol.Name)

	sources := vol.Projected.Sources
	require.Len(t, sources, 3)

	// ConfigMaps first, name-sorted.
	require.Equal(t, "cm-a", sources[0].ConfigMap.Name)
	require.Equal(t, []corev1.KeyToPath{{Key: "two.crt", Path: "configmaps/cm-a-two.crt"}}, sources[0].ConfigMap.Items)
	require.Equal(t, "rpc-ca", sources[1].ConfigMap.Name)
	require.Equal(t, []corev1.KeyToPath{{Key: "ca.crt", Path: "configmaps/rpc-ca-ca.crt"}}, sources[1].ConfigMap.Items)

	// Then Secrets.
	require.Equal(t, "secret-b", sources[2].Secret.Name)
	require.Equal(t, []corev1.KeyToPath{{Key: "one.crt", Path: "secrets/secret-b-one.crt"}}, sources[2].Secret.Items)

	require.Equal(t, &corev1.VolumeMount{
		Name:      "truststores",
		MountPath: trustStoreMountPath,
		ReadOnly:  true,
	}, l.TrustStoreMount())
}

// TestClientTLS pins the three client shapes. rpk and Redpanda read different
// keys for the same file, and both fall back to the serving certificate rather
// than the OS bundle. All three read the API's in-cluster listener.
func TestClientTLS(t *testing.T) {
	l := testListeners()
	require.Equal(t, map[string]any{
		"ca_file":   "/etc/tls/certs/default/ca.crt",
		"cert_file": "/etc/tls/certs/default-client/tls.crt",
		"key_file":  "/etc/tls/certs/default-client/tls.key",
	}, l.Admin().RPKClientTLS())

	require.Equal(t, map[string]any{
		"enabled":             true,
		"require_client_auth": true,
		"truststore_file":     "/etc/tls/certs/default/ca.crt",
		"cert_file":           "/etc/tls/certs/default-client/tls.crt",
		"key_file":            "/etc/tls/certs/default-client/tls.key",
	}, l.Admin().BrokerClientTLS())

	require.Equal(t,
		"--cacert /etc/tls/certs/default-client/ca.crt --cert /etc/tls/certs/default-client/tls.crt --key /etc/tls/certs/default-client/tls.key",
		l.Admin().CurlFlags())

	// Without mTLS there is no client keypair to present. Set at construction:
	// [ListenerTLS.Client] is resolved, so flipping the flag after the fact
	// would leave the keypair behind.
	pki := testPKI()
	noClientAuth := testListeners()
	noClientAuth.Admin().Listeners[0].TLS = chartTLS(&pki, "default", false, nil)
	require.Equal(t, map[string]any{"ca_file": "/etc/tls/certs/default/ca.crt"}, noClientAuth.Admin().RPKClientTLS())
	require.Equal(t, "--cacert /etc/tls/certs/default/ca.crt", noClientAuth.Admin().CurlFlags())

	// Nil, not an empty map: the callers disagree on what absent looks like in
	// YAML, so each normalises at its own call site.
	require.Nil(t, l.Kafka().RPKClientTLS())
	require.Nil(t, l.Kafka().BrokerClientTLS())
	require.Empty(t, l.Kafka().CurlFlags())

	// mTLS on a certificate the PKI issued no client keypair for. The
	// listener's flag and the certificate's issuance are decided separately, so
	// this is representable; presenting a path to a keypair nothing mounted is
	// not.
	noClientCert := testListeners()
	noClientCert.Admin().Listeners[0].TLS = chartTLS(&pki, "external", true, nil)
	require.Equal(t, map[string]any{
		"ca_file": "/etc/tls/certs/external/tls.crt",
	}, noClientCert.Admin().RPKClientTLS())
	require.Equal(t, "--cacert /etc/tls/certs/external/tls.crt", noClientCert.Admin().CurlFlags())
}

// TestAdvertisedPorts pins the two formulas apart. The profile's starts from
// the in-cluster port and guards on > 1; the configurator's starts from the
// listener's own.
func TestAdvertisedPorts(t *testing.T) {
	l := testListeners()

	// Two advertised ports: indexed by replica.
	require.Equal(t, int32(31092), l.Kafka().ProfileAdvertisedPort(0))
	require.Equal(t, int32(31093), l.Kafka().ProfileAdvertisedPort(1))
	kafkaExt := l.Kafka().External()[0]
	require.Equal(t, int32(31092), kafkaExt.AdvertisedPort(0))
	require.Equal(t, int32(31093), kafkaExt.AdvertisedPort(1))

	// One advertised port: every replica shares it.
	require.Equal(t, int32(31644), l.Admin().ProfileAdvertisedPort(0))
	require.Equal(t, int32(31644), l.Admin().ProfileAdvertisedPort(3))
	adminExt := l.Admin().External()[0]
	require.Equal(t, int32(31644), adminExt.AdvertisedPort(3))

	// None: the profile takes the listener's port, guarded on > 1.
	plain := testListeners()
	plain.Admin().Listeners[1].AdvertisedPorts = nil
	require.Equal(t, int32(9645), plain.Admin().ProfileAdvertisedPort(0))
	plainExt := plain.Admin().External()[0]
	require.Equal(t, int32(9645), plainExt.AdvertisedPort(0))

	portless := testListeners()
	portless.Admin().Listeners[1].AdvertisedPorts = nil
	portless.Admin().Listeners[1].Port = 1
	require.Equal(t, int32(9644), portless.Admin().ProfileAdvertisedPort(0), "guarded on > 1, so falls through to the in-cluster port")

	// No exposed listeners at all: the in-cluster port, and no first name.
	require.Equal(t, int32(8082), l.HTTP().ProfileAdvertisedPort(0))
}
