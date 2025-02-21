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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

const (
	// DefaultExternalKafkaListenerName is the default name of a kafka external listener
	DefaultExternalKafkaListenerName = vectorizedv1alpha1.ExternalListenerName
	// InternalListenerName is the name of the internal Kafka listener
	InternalListenerName = vectorizedv1alpha1.InternalListenerName
	// ExternalListenerName is the default name of external listener
	ExternalListenerName = DefaultExternalKafkaListenerName
	// DefaultExternalPandaproxyListenerName is the default name of a pandaproxy external listener
	DefaultExternalProxyListenerName = vectorizedv1alpha1.PandaproxyExternalListenerName
	// InternalProxyListenerName is the name of the internal proxy listener
	InternalProxyListenerName = "proxy"
	// DefaultExternalSchemaRegistryListenerName is the default name of a schema registry external listener
	DefaultExternalSchemaRegistryListenerName = vectorizedv1alpha1.SchemaRegistryExternalListenerName

	// KafkfaAPIConfigPath is the path to the kafka API configuration at the RP brokers.
	KafkaAPIConfigPath = "redpanda.kafka_api"
	// AdvertisedKafkaAPIConfigPath is the path to the advertised kafka API configuration at the RP brokers.
	AdvertisedKafkaAPIConfigPath = "redpanda.advertised_kafka_api"
	// PandaproxyAPIConfigPath is the path to the pandaproxy API configuration at the
	PandaproxyAPIConfigPath = "pandaproxy.pandaproxy_api"
	// AdvertisedPandaproxyAPIConfigPath is the path to the advertised pandaproxy API configuration at the RP brokers.
	AdvertisedPandaproxyAPIConfigPath = "pandaproxy.advertised_pandaproxy_api"
	// SchemaRegistryAPIConfigPath is the path to the schema registry API configuration at the RP brokers.
	SchemaRegistryAPIConfigPath = "schema_registry.schema_registry_api"
	// KafkaAPIConfigTLSPath is the path to the kafka API TLS configuration at the RP brokers.
	KafkaAPIConfigTLSPath = "redpanda.kafka_api_tls"
	// PandaproxyAPIConfigTLSPath is the path to the pandaproxy API TLS configuration at the RP brokers.
	PandaproxyAPIConfigTLSPath = "pandaproxy.pandaproxy_api_tls"
	// SchemaRegistryAPIConfigTLSPath is the path to the schema registry API TLS configuration at the RP brokers.
	SchemaRegistryAPIConfigTLSPath = "schema_registry.schema_registry_api_tls"
)

// AdditionalListenerCfgNames contains the list of the listener names supported in additionalConfiguration.
var AdditionalListenerCfgNames = []string{
	KafkaAPIConfigPath,
	AdvertisedKafkaAPIConfigPath,
	PandaproxyAPIConfigPath,
	AdvertisedPandaproxyAPIConfigPath,
	SchemaRegistryAPIConfigPath,
	KafkaAPIConfigTLSPath,
	PandaproxyAPIConfigTLSPath,
	SchemaRegistryAPIConfigTLSPath,
}

// listenerTemplateSpec defines the external listener.
type listenerTemplateSpec struct {
	Name                 string
	Address              string
	Port                 string
	AuthenticationMethod string
}

// Encode returns the listenerTemplateSpec as a string in the format as below:
// {'name':'private-link-kafka','address':'0.0.0.0','port':{{39002 | add .Index}},'authentication_method':'sasl'}
func (l *listenerTemplateSpec) Encode() string {
	args := []any{l.Name}
	s := "{'name':'%s'"
	if l.Address != "" {
		s += ",'address':'%s'"
		args = append(args, l.Address)
	}
	if l.Port != "" {
		s += ",'port':%s"
		args = append(args, l.Port)
	}
	if l.AuthenticationMethod != "" {
		s += ",'authentication_method':'%s'"
		args = append(args, l.AuthenticationMethod)
	}
	s += "}"
	return fmt.Sprintf(s, args...)
}

// tlsTemplateSpec defines the TLS configuration for a listener.
type tlsTemplateSpec struct {
	Name              string
	KeyFile           string
	CertFile          string
	TruststoreFile    string
	RequireClientAuth bool
}

// Encode returns the tlsTemplateSpec as a string in the format as below:
// {'name':'mtls-kafka','key_file':'/etc/tls/certs/schema-registry/tls.key','cert_file':'/etc/tls/certs/schema-registry/tls.crt','truststore_file':'/etc/tls/certs/schema-registry/ca.crt','required_client_auth':true}
func (t *tlsTemplateSpec) Encode() string {
	args := []any{t.Name}
	s := "{'name':'%s'"
	if t.KeyFile != "" {
		s += ",'key_file':'%s'"
		args = append(args, t.KeyFile)
	}
	if t.CertFile != "" {
		s += ",'cert_file':'%s'"
		args = append(args, t.CertFile)
	}
	if t.TruststoreFile != "" {
		s += ",'truststore_file':'%s'"
		args = append(args, t.TruststoreFile)
	}
	if t.RequireClientAuth {
		s += ",'require_client_auth':%t"
		args = append(args, t.RequireClientAuth)
	}
	s += "}"
	return fmt.Sprintf(s, args...)
}

// allListenersTemplateSpec defines all the external listeners.
// The encoded value will be passed to the configurator for configuring the listeners at the init time.
type allListenersTemplateSpec struct {
	KafkaListeners           []listenerTemplateSpec
	KafkaAdvertisedListeners []listenerTemplateSpec
	KafkaTLSSpec             []tlsTemplateSpec
	ProxyListeners           []listenerTemplateSpec
	ProxyAdvertisedListeners []listenerTemplateSpec
	ProxyTLSSpec             []tlsTemplateSpec
	SchemaRegistryListeners  []listenerTemplateSpec
	SchemaRegistryTLSSpec    []tlsTemplateSpec
}

// Encode returns the allListenersTemplateSpec as a string in the format as below:
//
//	{
//	   "redpanda.advertised_kafka_api":"[{'name':'pl-kafka-external','port':{{32092 | add .Index | add .HostIndexOffset}}},{'name':'kafka-mtls','port': 30094}]",
//	   "redpanda.kafka_api":"[{'name':'pl-kafka-external','address':'0.0.0.0','port':32092,{'name':'kafka-mtls','address':'0.0.0.0','port':30094}]",
//	   "pandaproxy.advertised_pandaproxy_api":"[{'name':'pl-proxy-external','port':{{35082 | add .Index | add .HostIndexOffset}}},{'name':'proxy-mtls','port':30084}]",
//	   "pandaproxy.pandaproxy_api":"[{'name':'pl-proxy-external','address':'0.0.0.0','port':31082},{'name':'proxy-mtls','address':'0.0.0.0','port':30084}]",
//	   "schema_registry.schema_registry_api":"[{'name':'schema-registry','address':'0.0.0.0','port': 30081},{'name':'schema-registry-mtls','address':'0.0.0.0','port':30083}]"
//	   "redpanda.kafka_api_tls":"[{'name':'pl-kafka-external','key_file':'/etc/tls/certs/schema-registry/tls.key','cert_file':'/etc/tls/certs/schema-registry/tls.crt'},{'name':'kafka-mtls','truststore_file':'/etc/tls/certs/schema-registry/ca.crt']",
//	   "pandaproxy.pandaproxy_api_tls":"[{'name':'pl-proxy-external','key_file':'/etc/tls/certs/schema-registry/tls.key','cert_file':'/etc/tls/certs/schema-registry/tls.crt'},{'name':'schema-registry-mtls','truststore_file':'/etc/tls/certs/schema-registry/ca.crt']",
//	   "schema_registry.schema_registry_api_tls":"[{'name':'schema-registry','key_file':'/etc/tls/certs/schema-registry/tls.key','cert_file':'/etc/tls/certs/schema-registry/tls.crt'},{'name':'schema-registry-mtls','truststore_file':'/etc/tls/certs/schema-registry/ca.crt']"
//	}
func (a *allListenersTemplateSpec) Encode() string {
	encodeListeners := func(listeners []listenerTemplateSpec) string {
		encoded := []string{}
		for _, l := range listeners {
			encoded = append(encoded, l.Encode())
		}
		return strings.Join(encoded, ",")
	}
	encodeTLSSpecs := func(listeners []tlsTemplateSpec) string {
		encoded := []string{}
		for _, l := range listeners {
			encoded = append(encoded, l.Encode())
		}
		return strings.Join(encoded, ",")
	}

	s := "{\n"
	args := []any{}

	for _, v := range []struct {
		key string
		val []listenerTemplateSpec
	}{
		{KafkaAPIConfigPath, a.KafkaListeners},
		{AdvertisedKafkaAPIConfigPath, a.KafkaAdvertisedListeners},
		{PandaproxyAPIConfigPath, a.ProxyListeners},
		{AdvertisedPandaproxyAPIConfigPath, a.ProxyAdvertisedListeners},
		{SchemaRegistryAPIConfigPath, a.SchemaRegistryListeners},
	} {
		if len(v.val) > 0 {
			if len(args) > 0 {
				s += ",\n"
			}
			s += `"` + v.key + `":"[%s]"`
			args = append(args, encodeListeners(v.val))
		}
	}

	for _, v := range []struct {
		key string
		val []tlsTemplateSpec
	}{
		{KafkaAPIConfigTLSPath, a.KafkaTLSSpec},
		{PandaproxyAPIConfigTLSPath, a.ProxyTLSSpec},
		{SchemaRegistryAPIConfigTLSPath, a.SchemaRegistryTLSSpec},
	} {
		if len(v.val) > 0 {
			if len(args) > 0 {
				s += ",\n"
			}
			s += `"` + v.key + `":"[%s]"`
			args = append(args, encodeTLSSpecs(v.val))
		}
	}

	s += "\n}"
	return fmt.Sprintf(s, args...)
}

// Concat concatenates the given spec1 with the current spec.
// For each of the keys in the given spec1, the value is concatenated with the value in the current spec.
// The concatenated value is a list of listeners printed as a string.
//
//	vaule1 = [{'name':'mtls-kafka','port':{{9094 | add .Index | add .HostIndexOffset}}}]
//	value2 = [{'name':'sasl-kafka','port': {{9092 | add .Index | add .HostIndexOffset}}}]
//	Concat value = [{'name':'mtls-kafka','port':{{9094 | add .Index | add .HostIndexOffset}},{'name':'pl2-kafka','port': {{9092 | add .Index | add .HostIndexOffset}}}]
func (a *allListenersTemplateSpec) Concat(spec1 map[string]string) (string, error) {
	regex := regexp.MustCompile(`\[(.*)\]`)
	encoded := a.Encode()
	spec := map[string]string{}
	err := json.Unmarshal([]byte(encoded), &spec)
	if err != nil {
		return "", err
	}
	for k, v1 := range spec1 {
		matches1 := regex.FindStringSubmatch(v1)
		if len(matches1) < 2 {
			return "", fmt.Errorf("invalid value for key %s: %s", k, v1)
		}
		v := spec[k]
		if v == "" {
			spec[k] = v1
			continue
		}
		matches := regex.FindStringSubmatch(v)
		if len(matches) < 2 {
			return "", fmt.Errorf("invalid value for key %s: %s", k, v)
		}
		spec[k] = fmt.Sprintf("[%s,%s]", matches[1], matches1[1])
	}

	s, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(s), nil
}
