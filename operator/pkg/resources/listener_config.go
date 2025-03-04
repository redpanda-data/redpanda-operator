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

type TemplatedInt string

func (t TemplatedInt) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"removequote%sremovequote"`, t)), nil
}

// listenerTemplateSpec defines the external listener.
// An example of the encoded value is as below:
// {'name':'private-link-kafka','address':'0.0.0.0','port':{{39002 | add .Index}},'authentication_method':'sasl'}
type listenerTemplateSpec struct {
	Name                 string       `json:"name"`
	Address              string       `json:"address,omitempty"`
	Port                 TemplatedInt `json:"port,omitempty"`
	AuthenticationMethod string       `json:"authentication_method,omitempty"`
}

// tlsTemplateSpec defines the TLS configuration for a listener.
// An example of the encoded value is as below:
// {'name':'mtls-kafka','key_file':'/etc/tls/certs/schema-registry/tls.key','cert_file':'/etc/tls/certs/schema-registry/tls.crt','truststore_file':'/etc/tls/certs/schema-registry/ca.crt','required_client_auth':true}
type tlsTemplateSpec struct {
	Name              string `json:"name"`
	KeyFile           string `json:"key_file,omitempty"`
	CertFile          string `json:"cert_file,omitempty"`
	TruststoreFile    string `json:"truststore_file,omitempty"`
	RequireClientAuth bool   `json:"require_client_auth,omitempty"`
}

// allListenersTemplateSpec defines all the external listeners.
// The encoded value will be passed to the configurator for configuring the listeners at the init time.
type allListenersTemplateSpec struct {
	KafkaListeners           []listenerTemplateSpec `json:"redpanda.kafka_api,omitempty"`
	KafkaAdvertisedListeners []listenerTemplateSpec `json:"redpanda.advertised_kafka_api,omitempty"`
	KafkaTLSSpec             []tlsTemplateSpec      `json:"redpanda.kafka_api_tls,omitempty"`
	ProxyListeners           []listenerTemplateSpec `json:"pandaproxy.pandaproxy_api,omitempty"`
	ProxyAdvertisedListeners []listenerTemplateSpec `json:"pandaproxy.advertised_pandaproxy_api,omitempty"`
	ProxyTLSSpec             []tlsTemplateSpec      `json:"pandaproxy.pandaproxy_api_tls,omitempty"`
	SchemaRegistryListeners  []listenerTemplateSpec `json:"schema_registry.schema_registry_api,omitempty"`
	SchemaRegistryTLSSpec    []tlsTemplateSpec      `json:"schema_registry.schema_registry_api_tls,omitempty"`
}

func afterJSONEncoding(a interface{}) string {
	s, _ := json.Marshal(a)
	re1 := regexp.MustCompile(`(^|[^\\])(\\{2})*"`)
	strSingleQuotes := re1.ReplaceAllStringFunc(string(s), func(match string) string {
		if match == `"` {
			return `'`
		}
		return match[:len(match)-1] + `'`
	})

	re2 := regexp.MustCompile(`'removequote|removequote'`)
	// Remove the single quote pattens from the encoded string for templated int values.
	return re2.ReplaceAllString(strSingleQuotes, "")
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
			encoded = append(encoded, afterJSONEncoding(l))
		}
		return strings.Join(encoded, ",")
	}
	encodeTLSSpecs := func(listeners []tlsTemplateSpec) string {
		encoded := []string{}
		for _, l := range listeners {
			encoded = append(encoded, afterJSONEncoding(l))
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
	fmt.Println(encoded)
	err := json.Unmarshal([]byte(encoded), &spec)
	if err != nil {
		fmt.Println(err)
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
