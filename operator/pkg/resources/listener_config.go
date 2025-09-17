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

	"gopkg.in/yaml.v3" //nolint:depguard // this is necessary due to differences in how the yaml tagging mechanisms work and the fact that some structs on config.RedpandaYaml are missing inline annotations

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

// TemplateInt defines for a template used to compute an integer value, like port {{39002 | add .Index}}
type TemplatedInt string

const (
	removeQuote          = "removequote"
	removeQuoteRegexStr  = `"` + removeQuote + `|` + removeQuote + `"`
	portTemplateRegexStr = `'port'\s*:\s*\{\{.*?\}\}`
)

var (
	regexRemoveQuotes    = regexp.MustCompile(removeQuoteRegexStr)
	regexAddSingleQuotes = regexp.MustCompile(portTemplateRegexStr)
)

// MarshalJSON converts a TemplatedInt into a templated string. Since the final port value must be
// an integer rather than a string, we encode it using the "removequote" marker to ensure the
// quotes are stripped, allowing the port to be interpreted as an integer.
func (t TemplatedInt) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s%s%s"`, removeQuote, t, removeQuote)), nil
}

// listenerTemplateSpec defines the external listener.
type listenerTemplateSpec struct {
	Name                 string       `json:"name" yaml:"name" `
	Address              string       `json:"address,omitempty" yaml:"address,omitempty"`
	Port                 TemplatedInt `json:"port,omitempty" yaml:"port,omitempty"`
	AuthenticationMethod string       `json:"authentication_method,omitempty" yaml:"authentication_method,omitempty"`
}

// tlsTemplateSpec defines the TLS configuration for a listener.
type tlsTemplateSpec struct {
	Name              string `json:"name" yaml:"name"`
	KeyFile           string `json:"key_file,omitempty" yaml:"key_file,omitempty"`
	CertFile          string `json:"cert_file,omitempty" yaml:"cert_file,omitempty"`
	TruststoreFile    string `json:"truststore_file,omitempty" yaml:"truststore_file,omitempty"`
	RequireClientAuth bool   `json:"require_client_auth,omitempty" yaml:"require_client_auth,omitempty"`
}

// allListenersTemplateSpec defines all the external listeners.
// The encoded value will be passed to Configurator via ADDITIONAL_LISTENERS env for configuring additional listeners.
type allListenersTemplateSpec struct {
	KafkaListeners           []listenerTemplateSpec `json:"redpanda.kafka_api,omitempty" yaml:"redpanda.kafka_api,omitempty"`
	KafkaAdvertisedListeners []listenerTemplateSpec `json:"redpanda.advertised_kafka_api,omitempty" yaml:"redpanda.advertised_kafka_api,omitempty"`
	KafkaTLSSpec             []tlsTemplateSpec      `json:"redpanda.kafka_api_tls,omitempty" yaml:"redpanda.kafka_api_tls,omitempty"`
	ProxyListeners           []listenerTemplateSpec `json:"pandaproxy.pandaproxy_api,omitempty" yaml:"pandaproxy.pandaproxy_api,omitempty"`
	ProxyAdvertisedListeners []listenerTemplateSpec `json:"pandaproxy.advertised_pandaproxy_api,omitempty" yaml:"pandaproxy.advertised_pandaproxy_api,omitempty"`
	ProxyTLSSpec             []tlsTemplateSpec      `json:"pandaproxy.pandaproxy_api_tls,omitempty" yaml:"pandaproxy.pandaproxy_api_tls,omitempty"`
	SchemaRegistryListeners  []listenerTemplateSpec `json:"schema_registry.schema_registry_api,omitempty" yaml:"schema_registry.schema_registry_api,omitempty"`
	SchemaRegistryTLSSpec    []tlsTemplateSpec      `json:"schema_registry.schema_registry_api_tls,omitempty" yaml:"schema_registry.schema_registry_api_tls,omitempty"`
}

// Encode returns the allListenersTemplateSpec as a string in the format as below:
// The port value can be a templated integer in string without quotes, like {{39002 | add .Index}}
//
//	{
//	   "redpanda.advertised_kafka_api":[{"name":"pl-kafka-external","port":{{32092 | add .Index | add .HostIndexOffset}}},{"name":"kafka-mtls","port": 30094}],
//	   "redpanda.kafka_api":[{"name":"pl-kafka-external","address":"0.0.0.0","port":32092,{"name":"kafka-mtls","address":"0.0.0.0","port":30094}],
//	   "pandaproxy.advertised_pandaproxy_api":[{"name":"pl-proxy-external","port":{{35082 | add .Index | add .HostIndexOffset}}},{"name":"proxy-mtls","port":30084}],
//	   "pandaproxy.pandaproxy_api":[{"name":"pl-proxy-external","address":"0.0.0.0","port":31082},{"name":"proxy-mtls","address":"0.0.0.0","port":30084}],
//	   "schema_registry.schema_registry_api":[{"name":"schema-registry","address":"0.0.0.0","port": 30081},{"name":"schema-registry-mtls","address":"0.0.0.0","port":30083}]
//	   "redpanda.kafka_api_tls":[{"name":"pl-kafka-external","key_file":"/etc/tls/certs/schema-registry/tls.key","cert_file":"/etc/tls/certs/schema-registry/tls.crt"},{"name":"kafka-mtls","truststore_file":"/etc/tls/certs/schema-registry/ca.crt"],
//	   "pandaproxy.pandaproxy_api_tls":[{"name":"pl-proxy-external","key_file":"/etc/tls/certs/schema-registry/tls.key","cert_file":"/etc/tls/certs/schema-registry/tls.crt"},{"name":"schema-registry-mtls","truststore_file":"/etc/tls/certs/schema-registry/ca.crt"],
//	   "schema_registry.schema_registry_api_tls":[{"name":"schema-registry","key_file":"/etc/tls/certs/schema-registry/tls.key","cert_file":"/etc/tls/certs/schema-registry/tls.crt"},{"name":"schema-registry-mtls","truststore_file":"/etc/tls/certs/schema-registry/ca.crt"]
//	}
func (a *allListenersTemplateSpec) Encode() (string, error) {
	s, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return regexRemoveQuotes.ReplaceAllString(string(s), ""), nil
}

// IsEmpty returns true if the allListenersTemplateSpec is empty.
func (a *allListenersTemplateSpec) IsEmpty() bool {
	return len(a.KafkaListeners) == 0 && len(a.KafkaAdvertisedListeners) == 0 && len(a.KafkaTLSSpec) == 0 &&
		len(a.ProxyListeners) == 0 && len(a.ProxyAdvertisedListeners) == 0 && len(a.ProxyTLSSpec) == 0 &&
		len(a.SchemaRegistryListeners) == 0 && len(a.SchemaRegistryTLSSpec) == 0
}

// Append adds the listener configs in the given spec1 to the current spec.
//
//	input = [{'name':'mtls-kafka','port':{{9094 | add .Index | add .HostIndexOffset}}}]
//	current = [{"name":"sasl-kafka","port": 9092}]
//	Final value = [{"name":"mtls-kafka","port":{{9094 | add .Index | add .HostIndexOffset}},{"name":"sasl-kafka","port": 9092}]
func (a *allListenersTemplateSpec) Append(spec1 map[string]string) (string, error) {
	inputSpec := &allListenersTemplateSpec{}
	var err error
	if a.IsEmpty() {
		result, err := json.Marshal(spec1)
		return string(result), err
	}

	for _, cfgName := range AdditionalListenerCfgNames {
		v, ok := spec1[cfgName]
		if !ok {
			continue
		}
		// Replace 'port': {{ ... }} with 'port': '{{ ... }}' for working with yaml.Unmarshal
		v = regexAddSingleQuotes.ReplaceAllStringFunc(v, func(match string) string {
			index := strings.Index(match, "{{")
			return "'port':'" + match[index:] + "'"
		})

		switch cfgName {
		case KafkaAPIConfigPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.KafkaListeners)
		case AdvertisedKafkaAPIConfigPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.KafkaAdvertisedListeners)
		case KafkaAPIConfigTLSPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.KafkaTLSSpec)
		case PandaproxyAPIConfigPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.ProxyListeners)
		case AdvertisedPandaproxyAPIConfigPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.ProxyAdvertisedListeners)
		case PandaproxyAPIConfigTLSPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.ProxyTLSSpec)
		case SchemaRegistryAPIConfigPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.SchemaRegistryListeners)
		case SchemaRegistryAPIConfigTLSPath:
			err = yaml.Unmarshal([]byte(v), &inputSpec.SchemaRegistryTLSSpec)
		}
		if err != nil {
			return "", err
		}
	}
	a.KafkaListeners = append(a.KafkaListeners, inputSpec.KafkaListeners...)
	a.KafkaAdvertisedListeners = append(a.KafkaAdvertisedListeners, inputSpec.KafkaAdvertisedListeners...)
	a.KafkaTLSSpec = append(a.KafkaTLSSpec, inputSpec.KafkaTLSSpec...)
	a.ProxyListeners = append(a.ProxyListeners, inputSpec.ProxyListeners...)
	a.ProxyAdvertisedListeners = append(a.ProxyAdvertisedListeners, inputSpec.ProxyAdvertisedListeners...)
	a.ProxyTLSSpec = append(a.ProxyTLSSpec, inputSpec.ProxyTLSSpec...)
	a.SchemaRegistryListeners = append(a.SchemaRegistryListeners, inputSpec.SchemaRegistryListeners...)
	a.SchemaRegistryTLSSpec = append(a.SchemaRegistryTLSSpec, inputSpec.SchemaRegistryTLSSpec...)

	s, err := a.Encode()
	if err != nil {
		return "", err
	}
	return string(s), nil
}
