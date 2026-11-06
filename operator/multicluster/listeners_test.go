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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestListenersForPool(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*redpandav1alpha2.BrokerPoolSpec)
		pool   bool
		sasl   bool
		check  func(*testing.T, *redpandav1alpha2.BrokerPoolSpec, *redpanda.Listeners)
	}{
		{
			name: "port names",
			pool: true,
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				schema := l.SchemaRegistry()
				require.Equal(t, redpanda.ReservedListenerName, schema.Reserved.Name)
				require.Equal(t, "schemaregistry", schema.Reserved.PortName)
				require.Equal(t, "schemaregistry", schema.Reserved.ContainerPortName)

				require.Len(t, schema.Additional, 1)
				require.Equal(t, "default", schema.Additional[0].Name)
				require.Equal(t, "schema-default", schema.Additional[0].PortName)
				require.Equal(t, "schema-default", schema.Additional[0].ContainerPortName)

				require.Empty(t, l.RPC().Additional)
				require.Equal(t, "rpc", l.RPC().Reserved.PortName)
			},
		},
		{
			name: "listener TLS enabled, pool TLS disabled",
			mutate: func(s *redpandav1alpha2.BrokerPoolSpec) {
				s.Listeners.Kafka.TLS.Enabled = ptr.To(true)
			},
			check: func(t *testing.T, s *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				kafka := l.Kafka().Reserved
				require.NotNil(t, kafka.TLS)
				require.Nil(t, l.Admin().Reserved.TLS)
				require.Contains(t, poolCertificates(s, "pool"), kafka.TLS.Server.Name)

				// Admin requires mTLS on the same certificate, but admin has
				// no TLS. Thus, no client certificate is issued.
				require.False(t, kafka.TLS.RequireClientAuth)
				require.Nil(t, kafka.TLS.Client)

				sections := l.ConfigSections()
				require.Contains(t, sections["redpanda"], "kafka_api_tls")
				require.NotContains(t, sections["redpanda"], "admin_api_tls")
			},
		},
		{
			name: "listener TLS enabled with mTLS, pool TLS disabled",
			mutate: func(s *redpandav1alpha2.BrokerPoolSpec) {
				s.Listeners.Kafka.TLS.Enabled = ptr.To(true)
				s.Listeners.Kafka.TLS.RequireClientAuth = ptr.To(true)
			},
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				kafka := l.Kafka().Reserved
				require.True(t, kafka.TLS.RequireClientAuth)
				require.NotNil(t, kafka.TLS.Client)
				require.Contains(t, l.Kafka().BrokerClientTLS(), "cert_file")
			},
		},
		{
			name: "listener TLS disabled, pool TLS enabled",
			pool: true,
			mutate: func(s *redpandav1alpha2.BrokerPoolSpec) {
				s.Listeners.Kafka.TLS.Enabled = ptr.To(false)
			},
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				require.Nil(t, l.Kafka().Reserved.TLS)
				require.NotNil(t, l.Kafka().Additional[0].TLS)

				entries := l.ConfigSections()["redpanda"]["kafka_api_tls"].([]map[string]any)
				require.Len(t, entries, 1)
				require.Equal(t, "default", entries[0]["name"])
			},
		},
		{
			// All APIs use the "default" certificate, and admin requires mTLS.
			// RPC uses its own flag.
			name: "require client auth",
			pool: true,
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				for _, api := range []*redpanda.API{l.Admin(), l.Kafka(), l.HTTP()} {
					require.True(t, api.Reserved.TLS.RequireClientAuth, api.Kind)
					require.NotNil(t, api.Reserved.TLS.Client, api.Kind)
				}
				require.False(t, l.RPC().Reserved.TLS.RequireClientAuth)
				require.Nil(t, l.RPC().Reserved.TLS.Client)
			},
		},
		{
			name: "no SASL",
			pool: true,
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				require.Empty(t, l.Kafka().Reserved.AuthenticationMethod)
				require.Empty(t, l.HTTP().Reserved.AuthenticationMethod)
			},
		},
		{
			name: "SASL",
			pool: true,
			sasl: true,
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				require.Equal(t, "sasl", l.Kafka().Reserved.AuthenticationMethod)
				require.Equal(t, "sasl", l.Kafka().Additional[0].AuthenticationMethod)
				require.Equal(t, "http_basic", l.HTTP().Reserved.AuthenticationMethod)
				require.Empty(t, l.Admin().Reserved.AuthenticationMethod)
				require.Empty(t, l.SchemaRegistry().Reserved.AuthenticationMethod)
			},
		},
		{
			name: "default external ports",
			pool: true,
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				require.Equal(t, redpandav1alpha2.DefaultExternalAdminPort, l.Admin().Additional[0].Port)
				require.Equal(t, redpandav1alpha2.DefaultExternalKafkaPort, l.Kafka().Additional[0].Port)
				require.Equal(t, redpandav1alpha2.DefaultExternalHTTPPort, l.HTTP().Additional[0].Port)
				require.Equal(t, redpandav1alpha2.DefaultExternalSchemaRegistryPort, l.SchemaRegistry().Additional[0].Port)
			},
		},
		{
			name: "explicit external port",
			pool: true,
			mutate: func(s *redpandav1alpha2.BrokerPoolSpec) {
				s.Listeners.Kafka.External["default"].Port = ptr.To[int32](31234)
			},
			check: func(t *testing.T, _ *redpandav1alpha2.BrokerPoolSpec, l *redpanda.Listeners) {
				require.Equal(t, int32(31234), l.Kafka().Additional[0].Port)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := poolSpec(tt.pool)
			if tt.mutate != nil {
				tt.mutate(spec)
			}
			tt.check(t, spec, resolveForSpec(spec, tt.sasl))
		})
	}
}

// poolSpec returns a spec with one external listener for each API, as
// MergeDefaults makes.
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

// resolveForSpec resolves a spec with its PKI, as poolListeners does.
func resolveForSpec(spec *redpandav1alpha2.BrokerPoolSpec, saslEnabled bool) *redpanda.Listeners {
	pki := redpanda.PKI{Certificates: poolCertificates(spec, "pool")}
	return listenersForPool(spec, saslEnabled, &pki)
}
