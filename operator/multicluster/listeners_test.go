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
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// poolSpec builds a spec with TLS on and one external listener per API, which
// is the shape MergeDefaults produces. BrokerPoolSpec embeds
// EmbeddedBrokerPoolSpec, so the literal needs the nested form.
func poolSpec(tlsEnabled bool) *redpandav1alpha2.BrokerPoolSpec {
	api := func(cert string, requireClientAuth bool) *redpandav1alpha2.StretchAPIListener {
		return &redpandav1alpha2.StretchAPIListener{
			StretchListener: redpandav1alpha2.StretchListener{
				TLS: &redpandav1alpha2.StretchListenerTLS{
					Cert:              ptr.To(cert),
					RequireClientAuth: ptr.To(requireClientAuth),
				},
			},
			External: map[string]*redpandav1alpha2.StretchExternalListener{
				"default": {
					StretchListener: redpandav1alpha2.StretchListener{
						TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("external")},
					},
				},
			},
		}
	}

	return &redpandav1alpha2.BrokerPoolSpec{
		EmbeddedBrokerPoolSpec: redpandav1alpha2.EmbeddedBrokerPoolSpec{
			TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(tlsEnabled)},
			Listeners: &redpandav1alpha2.StretchListeners{
				Admin:          api("default", true),
				Kafka:          api("default", false),
				HTTP:           api("default", false),
				SchemaRegistry: api("default", false),
				RPC: &redpandav1alpha2.StretchRPC{
					TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default")},
				},
			},
		},
	}
}

// resolveForSpec resolves a spec against the PKI its TLS resolves against,
// which is what poolListeners does at render time.
func resolveForSpec(spec *redpandav1alpha2.BrokerPoolSpec, saslEnabled bool) redpanda.Listeners {
	pki := redpanda.PKI{Certificates: poolCertificates(spec, "pool")}
	return listenersForPool(spec, saslEnabled, &pki)
}

// TestListenersForPoolListenerList asserts each API resolves to one ordered
// list with the in-cluster listener first, and that its port names follow the
// two different schemes: the in-cluster listener is named for its API alone
// while the rest carry the API's name and their own.
func TestListenersForPoolListenerList(t *testing.T) {
	listeners := resolveForSpec(poolSpec(true), false)

	schema := listeners.SchemaRegistry()
	require.Len(t, schema.Listeners, 2)

	require.Equal(t, redpanda.InternalListenerName, schema.Listeners[0].Name)
	require.Equal(t, "schemaregistry", schema.Listeners[0].PortName)
	require.Equal(t, "schemaregistry", schema.Listeners[0].ContainerPortName)

	require.Equal(t, "default", schema.Listeners[1].Name)
	require.Equal(t, "schema-default", schema.Listeners[1].PortName)
	require.Equal(t, "schema-default", schema.Listeners[1].ContainerPortName)

	// The derived views agree with the list.
	inCluster := schema.InCluster()
	require.Equal(t, redpanda.InternalListenerName, inCluster.Name)
	require.Len(t, schema.External(), 1)
	require.Equal(t, "default", schema.External()[0].Name)

	// RPC is single valued: one listener, no externals.
	rpc := listeners.RPC()
	require.Len(t, rpc.Listeners, 1)
	require.Empty(t, rpc.External())
	require.Equal(t, "rpc", rpc.InCluster().PortName)
}

// TestPoolCertificates pins that issuance reads the pool spec rather than the
// resolved listeners. The listeners take a [redpanda.PKI] to resolve their
// keypairs, so deriving one from them would be circular -- which makes
// issuance a superset of serving.
func TestPoolCertificates(t *testing.T) {
	certs := poolCertificates(poolSpec(true), "pool")
	require.Equal(t, []string{"default", "external"}, slices.Sorted(maps.Keys(certs)))
	// Only admin asks for mTLS, and issuance follows each listener's own flag.
	require.NotNil(t, certs["default"].Client)
	require.Nil(t, certs["external"].Client)

	// Pool TLS off: nothing is issued, so nothing is mounted.
	require.Empty(t, poolCertificates(poolSpec(false), "pool"))

	// The superset: a listener serving no TLS keeps its certificate, because
	// the spec still names it. Harmless -- a mounted file nothing reads --
	// where the reverse crashloops the broker.
	quiet := poolSpec(true)
	quiet.Listeners.Kafka.TLS.Enabled = ptr.To(false)
	require.Contains(t, poolCertificates(quiet, "pool"), "default")
	quietListeners := resolveForSpec(quiet, false)
	require.Nil(t, quietListeners.Kafka().InCluster().TLS)

	require.Empty(t, poolCertificates(nil, "pool"))
	require.Empty(t, poolCertificates(&redpandav1alpha2.BrokerPoolSpec{}, "pool"))
}

// TestListenersForPoolTLSEnablement pins what serves TLS. [redpanda.Listener.TLS]'s
// presence is the whole answer, and the pool's flag stays authoritative over
// the listener's, which overrides rather than narrows it. What gets *issued*
// is a separate, wider question -- see [TestPoolCertificates].
func TestListenersForPoolTLSEnablement(t *testing.T) {
	spec := poolSpec(false)
	spec.Listeners.Kafka.TLS.Enabled = ptr.To(true)

	listeners := resolveForSpec(spec, false)
	kafka := listeners.Kafka().InCluster()
	require.Nil(t, kafka.TLS, "no certificate is issued, so nothing can serve TLS")

	sections := listeners.ConfigSections()
	require.NotContains(t, sections["redpanda"], "kafka_api_tls")

	// The other direction: pool TLS on, one listener explicitly off. It serves
	// nothing and issues nothing, while its API's other listeners are
	// untouched.
	serving := poolSpec(true)
	serving.Listeners.Kafka.TLS.Enabled = ptr.To(false)

	listeners = resolveForSpec(serving, false)

	kafka = listeners.Kafka().InCluster()
	require.Nil(t, kafka.TLS)
	require.NotNil(t, listeners.Kafka().External()[0].TLS)

	// Only the external entry survives, where both did before.
	sections = listeners.ConfigSections()
	entries := sections["redpanda"]["kafka_api_tls"].([]map[string]any)
	require.Len(t, entries, 1)
	require.Equal(t, "default", entries[0]["name"])
}

// TestListenersForPoolRequireClientAuth pins the two answers apart: the APIs
// render the certificate's answer, which is the OR across every listener
// sharing it, while RPC renders its own flag.
func TestListenersForPoolRequireClientAuth(t *testing.T) {
	listeners := resolveForSpec(poolSpec(true), false)

	// Admin, kafka, http and schemaRegistry all share cert "default", and
	// admin requires mTLS, so all four render require_client_auth: true.
	admin := listeners.Admin().InCluster()
	kafka := listeners.Kafka().InCluster()
	http := listeners.HTTP().InCluster()
	require.True(t, admin.TLS.RequireClientAuth)
	require.True(t, kafka.TLS.RequireClientAuth)
	require.True(t, http.TLS.RequireClientAuth)

	// RPC shares that certificate too but renders its own flag.
	rpc := listeners.RPC().InCluster()
	require.False(t, rpc.TLS.RequireClientAuth)

	// The keypair follows require_client_auth, which here is the certificate's
	// answer -- so every API sharing "default" presents it, not just admin.
	require.NotNil(t, admin.TLS.Client)
	require.NotNil(t, kafka.TLS.Client)
	require.Nil(t, rpc.TLS.Client, "rpc takes its own flag, and it is false")
}

// TestListenersForPoolAuthenticationMethods asserts saslEnabled is the only
// thing the render state contributes, which is what keeps the resolver
// state-free.
func TestListenersForPoolAuthenticationMethods(t *testing.T) {
	spec := poolSpec(true)

	without := resolveForSpec(spec, false)
	withoutKafka := without.Kafka().InCluster()
	withoutHTTP := without.HTTP().InCluster()
	require.Empty(t, withoutKafka.AuthenticationMethod)
	require.Empty(t, withoutHTTP.AuthenticationMethod)

	with := resolveForSpec(spec, true)
	withKafka := with.Kafka().InCluster()
	withHTTP := with.HTTP().InCluster()
	withAdmin := with.Admin().InCluster()
	withSchema := with.SchemaRegistry().InCluster()
	require.Equal(t, "sasl", withKafka.AuthenticationMethod)
	require.Equal(t, "sasl", with.Kafka().External()[0].AuthenticationMethod)
	require.Equal(t, "http_basic", withHTTP.AuthenticationMethod)
	// Admin and schema registry are never authenticated.
	require.Empty(t, withAdmin.AuthenticationMethod)
	require.Empty(t, withSchema.AuthenticationMethod)
}

// TestListenersForPoolExternalPortDefaulting pins that a listener's port is
// defaulted in the resolver. The Service ports used to read the raw port and
// skip the listener when it was unset, while the config and container ports
// defaulted it, so a listener could be advertised with no Service behind it.
func TestListenersForPoolExternalPortDefaulting(t *testing.T) {
	listeners := resolveForSpec(poolSpec(true), false)

	require.Equal(t, redpandav1alpha2.DefaultExternalAdminPort, listeners.Admin().External()[0].Port)
	require.Equal(t, redpandav1alpha2.DefaultExternalKafkaPort, listeners.Kafka().External()[0].Port)
	require.Equal(t, redpandav1alpha2.DefaultExternalHTTPPort, listeners.HTTP().External()[0].Port)
	require.Equal(t, redpandav1alpha2.DefaultExternalSchemaRegistryPort, listeners.SchemaRegistry().External()[0].Port)

	// An explicit port wins.
	spec := poolSpec(true)
	spec.Listeners.Kafka.External["default"].Port = ptr.To[int32](31234)
	explicit := resolveForSpec(spec, false)
	require.Equal(t, int32(31234), explicit.Kafka().External()[0].Port)
}
