// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha2

// SchemaRegistrySpecApplyConfiguration represents an declarative configuration of the SchemaRegistrySpec type for use
// with apply.
type SchemaRegistrySpecApplyConfiguration struct {
	URLs []string                              `json:"urls,omitempty"`
	TLS  *CommonTLSApplyConfiguration          `json:"tls,omitempty"`
	SASL *SchemaRegistrySASLApplyConfiguration `json:"sasl,omitempty"`
}

// SchemaRegistrySpecApplyConfiguration constructs an declarative configuration of the SchemaRegistrySpec type for use with
// apply.
func SchemaRegistrySpec() *SchemaRegistrySpecApplyConfiguration {
	return &SchemaRegistrySpecApplyConfiguration{}
}

// WithURLs adds the given value to the URLs field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the URLs field.
func (b *SchemaRegistrySpecApplyConfiguration) WithURLs(values ...string) *SchemaRegistrySpecApplyConfiguration {
	for i := range values {
		b.URLs = append(b.URLs, values[i])
	}
	return b
}

// WithTLS sets the TLS field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the TLS field is set to the value of the last call.
func (b *SchemaRegistrySpecApplyConfiguration) WithTLS(value *CommonTLSApplyConfiguration) *SchemaRegistrySpecApplyConfiguration {
	b.TLS = value
	return b
}

// WithSASL sets the SASL field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the SASL field is set to the value of the last call.
func (b *SchemaRegistrySpecApplyConfiguration) WithSASL(value *SchemaRegistrySASLApplyConfiguration) *SchemaRegistrySpecApplyConfiguration {
	b.SASL = value
	return b
}