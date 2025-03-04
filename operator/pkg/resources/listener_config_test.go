// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeListenersTemplateSpec(t *testing.T) {
	listenerConfigs := getListenerConfigs()
	result, err := listenerConfigs.Encode()
	require.NoError(t, err)

	expected := `{` +
		`"redpanda.kafka_api":[{"name":"kafka-sasl-pl","address":"0.0.0.0","port":30092},{"name":"kafka-mtls","address":"0.0.0.0","port":30093,"authentication_method":"mtls_identity"}],` +
		`"redpanda.advertised_kafka_api":[{"name":"kafka-sasl-pl","address":"{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com","port":{{32092 | add .Index | add .HostIndexOffset}}},{"name":"kafka-mtls","port":32094}],` +
		`"redpanda.kafka_api_tls":[{"name":"kafka-sasl-pl","key_file":"/etc/tls/certs/kafka/tls.key","cert_file":"/etc/tls/certs/kafka/tls.crt"},{"name":"kafka-mtls","truststore_file":"/etc/tls/certs/kafka/ca.crt","require_client_auth":true}],` +
		`"pandaproxy.pandaproxy_api":[{"name":"proxy-sasl-pl","address":"0.0.0.0","port":30082},{"name":"proxy-mtls","address":"0.0.0.0","port":30083,"authentication_method":"mtls_identity"}],` +
		`"pandaproxy.advertised_pandaproxy_api":[{"name":"proxy-sasl-pl","address":"{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com","port":{{35082 | add .Index | add .HostIndexOffset}}},{"name":"proxy-mtls","port":35084}],` +
		`"pandaproxy.pandaproxy_api_tls":[{"name":"proxy-sasl-pl","key_file":"/etc/tls/certs/proxy/tls.key","cert_file":"/etc/tls/certs/proxy/tls.crt"},{"name":"proxy-mtls","truststore_file":"/etc/tls/certs/proxy/ca.crt","require_client_auth":true}],` +
		`"schema_registry.schema_registry_api":[{"name":"schema-registry","address":"0.0.0.0","port":30081},{"name":"schema-registry-mtls","address":"0.0.0.0","port":30083,"authentication_method":"mtls_identity"}],` +
		`"schema_registry.schema_registry_api_tls":[{"name":"schema-registry","key_file":"/etc/tls/certs/schema-registry/tls.key","cert_file":"/etc/tls/certs/schema-registry/tls.crt"},{"name":"schema-registry-mtls","truststore_file":"/etc/tls/certs/schema-registry/ca.crt","require_client_auth":true}]` +
		`}`

	require.Equal(t, expected, result)
}

func TestAppendListenersTemplateSpec(t *testing.T) {
	listenerConfigs := getListenerConfigs()
	additionals := map[string]string{
		"redpanda.kafka_api":                      `[{'name': 'add-kafka', 'address': '0.0.0.0', 'port': 30094, 'authentication_method': 'sasl'}]`,
		"redpanda.advertised_kafka_api":           `[{'name': 'add-kafka', 'address': 'test.redpanda.com', 'port': {{ 32095 | add .Index }}}]`,
		"redpanda.kafka_api_tls":                  `[{'name': 'add-kafka', 'key_file': '/etc/tls/certs/kafka/tls.key'}]`,
		"pandaproxy.pandaproxy_api":               `[{'name': 'add-proxy', 'address': '0.0.0.0', 'port': 30084}]`,
		"pandaproxy.advertised_pandaproxy_api":    `[{'name': 'add-proxy', 'address': 'test.redpanda.com', 'port': {{ 35085 | add .Index }}}]`,
		"pandaproxy.pandaproxy_api_tls":           `[{'name': 'add-proxy', 'cert_file': '/etc/tls/certs/proxy/tls.crt'}]`,
		"schema_registry.schema_registry_api":     `[{'name': 'add-schema', 'address': '0.0.0.0', 'port': 30085}]`,
		"schema_registry.schema_registry_api_tls": `[{'name': 'add-schema', 'key_file': '/etc/tls/certs/schema/add.key'}]`,
	}
	result, err := listenerConfigs.Append(additionals)
	require.NoError(t, err)

	expected := `{` +
		`"redpanda.kafka_api":[{"name":"kafka-sasl-pl","address":"0.0.0.0","port":30092},{"name":"kafka-mtls","address":"0.0.0.0","port":30093,"authentication_method":"mtls_identity"},{"name":"add-kafka","address":"0.0.0.0","port":30094,"authentication_method":"sasl"}],` +
		`"redpanda.advertised_kafka_api":[{"name":"kafka-sasl-pl","address":"{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com","port":{{32092 | add .Index | add .HostIndexOffset}}},{"name":"kafka-mtls","port":32094},{"name":"add-kafka","address":"test.redpanda.com","port":{{ 32095 | add .Index }}}],` +
		`"redpanda.kafka_api_tls":[{"name":"kafka-sasl-pl","key_file":"/etc/tls/certs/kafka/tls.key","cert_file":"/etc/tls/certs/kafka/tls.crt"},{"name":"kafka-mtls","truststore_file":"/etc/tls/certs/kafka/ca.crt","require_client_auth":true},{"name":"add-kafka","key_file":"/etc/tls/certs/kafka/tls.key"}],` +
		`"pandaproxy.pandaproxy_api":[{"name":"proxy-sasl-pl","address":"0.0.0.0","port":30082},{"name":"proxy-mtls","address":"0.0.0.0","port":30083,"authentication_method":"mtls_identity"},{"name":"add-proxy","address":"0.0.0.0","port":30084}],` +
		`"pandaproxy.advertised_pandaproxy_api":[{"name":"proxy-sasl-pl","address":"{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com","port":{{35082 | add .Index | add .HostIndexOffset}}},{"name":"proxy-mtls","port":35084},{"name":"add-proxy","address":"test.redpanda.com","port":{{ 35085 | add .Index }}}],` +
		`"pandaproxy.pandaproxy_api_tls":[{"name":"proxy-sasl-pl","key_file":"/etc/tls/certs/proxy/tls.key","cert_file":"/etc/tls/certs/proxy/tls.crt"},{"name":"proxy-mtls","truststore_file":"/etc/tls/certs/proxy/ca.crt","require_client_auth":true},{"name":"add-proxy","cert_file":"/etc/tls/certs/proxy/tls.crt"}],` +
		`"schema_registry.schema_registry_api":[{"name":"schema-registry","address":"0.0.0.0","port":30081},{"name":"schema-registry-mtls","address":"0.0.0.0","port":30083,"authentication_method":"mtls_identity"},{"name":"add-schema","address":"0.0.0.0","port":30085}],` +
		`"schema_registry.schema_registry_api_tls":[{"name":"schema-registry","key_file":"/etc/tls/certs/schema-registry/tls.key","cert_file":"/etc/tls/certs/schema-registry/tls.crt"},{"name":"schema-registry-mtls","truststore_file":"/etc/tls/certs/schema-registry/ca.crt","require_client_auth":true},{"name":"add-schema","key_file":"/etc/tls/certs/schema/add.key"}]` +
		`}`

	require.Equal(t, expected, result)
}

func getListenerConfigs() *allListenersTemplateSpec {
	return &allListenersTemplateSpec{
		KafkaListeners: []listenerTemplateSpec{
			{Name: "kafka-sasl-pl", Address: "0.0.0.0", Port: "30092"},
			{Name: "kafka-mtls", Address: "0.0.0.0", Port: "30093", AuthenticationMethod: "mtls_identity"},
		},
		KafkaAdvertisedListeners: []listenerTemplateSpec{
			{Name: "kafka-sasl-pl", Port: "{{32092 | add .Index | add .HostIndexOffset}}", Address: "{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com"},
			{Name: "kafka-mtls", Port: "32094"},
		},
		KafkaTLSSpec: []tlsTemplateSpec{
			{Name: "kafka-sasl-pl", KeyFile: "/etc/tls/certs/kafka/tls.key", CertFile: "/etc/tls/certs/kafka/tls.crt"},
			{Name: "kafka-mtls", TruststoreFile: "/etc/tls/certs/kafka/ca.crt", RequireClientAuth: true},
		},
		ProxyListeners: []listenerTemplateSpec{
			{Name: "proxy-sasl-pl", Address: "0.0.0.0", Port: "30082"},
			{Name: "proxy-mtls", Address: "0.0.0.0", Port: "30083", AuthenticationMethod: "mtls_identity"},
		},
		ProxyAdvertisedListeners: []listenerTemplateSpec{
			{Name: "proxy-sasl-pl", Port: "{{35082 | add .Index | add .HostIndexOffset}}", Address: "{{ .Index }}-{{ .HostIP | sha256sum }}.redpanda.com"},
			{Name: "proxy-mtls", Port: "35084"},
		},
		ProxyTLSSpec: []tlsTemplateSpec{
			{Name: "proxy-sasl-pl", KeyFile: "/etc/tls/certs/proxy/tls.key", CertFile: "/etc/tls/certs/proxy/tls.crt"},
			{Name: "proxy-mtls", TruststoreFile: "/etc/tls/certs/proxy/ca.crt", RequireClientAuth: true},
		},
		SchemaRegistryListeners: []listenerTemplateSpec{
			{Name: "schema-registry", Address: "0.0.0.0", Port: "30081"},
			{Name: "schema-registry-mtls", Address: "0.0.0.0", Port: "30083", AuthenticationMethod: "mtls_identity"},
		},
		SchemaRegistryTLSSpec: []tlsTemplateSpec{
			{Name: "schema-registry", KeyFile: "/etc/tls/certs/schema-registry/tls.key", CertFile: "/etc/tls/certs/schema-registry/tls.crt"},
			{Name: "schema-registry-mtls", TruststoreFile: "/etc/tls/certs/schema-registry/ca.crt", RequireClientAuth: true},
		},
	}
}
