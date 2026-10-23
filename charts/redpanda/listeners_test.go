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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// Rendered chart output is covered by the golden cases of
// charts/redpanda/chart. The tests here cover the paths that the chart does
// not take.

func TestExternalServicePorts(t *testing.T) {
	l := NewListeners([]API{
		{
			Kind:     AdminAPI,
			Reserved: &Listener{Name: ReservedListenerName, Port: 9644},
			Additional: []Listener{
				{Name: "default", Port: 9645, PortName: "admin-default", Exposed: true, AdvertisedPorts: []int32{31644}},
				{Name: "unexposed", Port: 9646, PortName: "admin-unexposed"},
			},
		},
		{
			Kind:     SchemaRegistryAPI,
			Reserved: &Listener{Name: ReservedListenerName, Port: 8081},
			Additional: []Listener{
				{Name: "default", Port: 8084, PortName: "schema-default", Exposed: true, Gateway: &GatewayRoute{Host: "schema.example.com"}},
			},
		},
	})

	require.Equal(t, []corev1.ServicePort{
		{Name: "admin-default", Protocol: corev1.ProtocolTCP, Port: 9645, TargetPort: intstr.FromInt32(9645)},
	}, l.ExternalServicePorts())
}

func TestTrustStoreFile(t *testing.T) {
	pki := testPKI()

	for _, tc := range []struct {
		name           string
		tls            *ListenerTLS
		trustStoreFile string
		serverCAFile   string
	}{
		{
			name:           "explicit truststore",
			tls:            NewListenerTLS(&pki, "external", false, &TrustStore{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "my-ca"}, Key: "ca.pem"}}),
			trustStoreFile: "/etc/truststores/secrets/my-ca-ca.pem",
			serverCAFile:   "/etc/truststores/secrets/my-ca-ca.pem",
		},
		{
			name:           "certificate with CA",
			tls:            NewListenerTLS(&pki, "default", false, nil),
			trustStoreFile: "/etc/tls/certs/default/ca.crt",
			serverCAFile:   "/etc/tls/certs/default/ca.crt",
		},
		{
			// The operator does not set TrustStoreFallback.
			name:           "certificate without CA or fallback",
			tls:            NewListenerTLS(&pki, "external", false, nil),
			trustStoreFile: "/etc/tls/certs/external/tls.crt",
			serverCAFile:   "/etc/tls/certs/external/tls.crt",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.trustStoreFile, tc.tls.TrustStoreFile())
			require.Equal(t, tc.serverCAFile, tc.tls.ServerCAFile())
		})
	}
}

func TestClientTLS(t *testing.T) {
	pki := testPKI()

	for _, tc := range []struct {
		name   string
		tls    *ListenerTLS
		rpk    map[string]any
		broker map[string]any
		curl   string
	}{
		{
			name: "mTLS",
			tls:  NewListenerTLS(&pki, "default", true, nil),
			rpk: map[string]any{
				"ca_file":   "/etc/tls/certs/default/ca.crt",
				"cert_file": "/etc/tls/certs/default-client/tls.crt",
				"key_file":  "/etc/tls/certs/default-client/tls.key",
			},
			broker: map[string]any{
				"enabled":             true,
				"require_client_auth": true,
				"truststore_file":     "/etc/tls/certs/default/ca.crt",
				"cert_file":           "/etc/tls/certs/default-client/tls.crt",
				"key_file":            "/etc/tls/certs/default-client/tls.key",
			},
			curl: "--cacert /etc/tls/certs/default/ca.crt --cert /etc/tls/certs/default-client/tls.crt --key /etc/tls/certs/default-client/tls.key",
		},
		{
			// The PKI has a client keypair for the certificate, but this
			// listener does not require mTLS.
			name:   "TLS without client auth",
			tls:    NewListenerTLS(&pki, "default", false, nil),
			rpk:    map[string]any{"ca_file": "/etc/tls/certs/default/ca.crt"},
			broker: map[string]any{"enabled": true, "require_client_auth": false, "truststore_file": "/etc/tls/certs/default/ca.crt"},
			curl:   "--cacert /etc/tls/certs/default/ca.crt",
		},
		{
			// The client config must not contain a path to a keypair that is
			// not mounted.
			name:   "mTLS without client keypair",
			tls:    NewListenerTLS(&pki, "external", true, nil),
			rpk:    map[string]any{"ca_file": "/etc/tls/certs/external/tls.crt"},
			broker: map[string]any{"enabled": true, "require_client_auth": true, "truststore_file": "/etc/tls/certs/external/tls.crt"},
			curl:   "--cacert /etc/tls/certs/external/tls.crt",
		},
		{
			name: "no TLS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := API{Kind: AdminAPI, Reserved: &Listener{Name: ReservedListenerName, TLS: tc.tls}}
			require.Equal(t, tc.rpk, api.RPKClientTLS())
			require.Equal(t, tc.broker, api.BrokerClientTLS())
			require.Equal(t, tc.curl, api.CurlFlags())
		})
	}
}

func testPKI() PKI {
	return PKI{Certificates: map[string]Certificate{
		"default": {
			Server: Keypair{Name: "default", CA: ptr.To("ca.crt")},
			Client: &Keypair{Name: "default-client"},
		},
		// No CA, to test the truststore fallbacks.
		"external": {Server: Keypair{Name: "external"}},
	}}
}
