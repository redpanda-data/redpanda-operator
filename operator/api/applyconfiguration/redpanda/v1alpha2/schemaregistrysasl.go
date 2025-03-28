// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha2

import (
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// SchemaRegistrySASLApplyConfiguration represents an declarative configuration of the SchemaRegistrySASL type for use
// with apply.
type SchemaRegistrySASLApplyConfiguration struct {
	Username  *string                         `json:"username,omitempty"`
	Password  *SecretKeyRefApplyConfiguration `json:"passwordSecretRef,omitempty"`
	Mechanism *redpandav1alpha2.SASLMechanism `json:"mechanism,omitempty"`
	AuthToken *SecretKeyRefApplyConfiguration `json:"token,omitempty"`
}

// SchemaRegistrySASLApplyConfiguration constructs an declarative configuration of the SchemaRegistrySASL type for use with
// apply.
func SchemaRegistrySASL() *SchemaRegistrySASLApplyConfiguration {
	return &SchemaRegistrySASLApplyConfiguration{}
}

// WithUsername sets the Username field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Username field is set to the value of the last call.
func (b *SchemaRegistrySASLApplyConfiguration) WithUsername(value string) *SchemaRegistrySASLApplyConfiguration {
	b.Username = &value
	return b
}

// WithPassword sets the Password field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Password field is set to the value of the last call.
func (b *SchemaRegistrySASLApplyConfiguration) WithPassword(value *SecretKeyRefApplyConfiguration) *SchemaRegistrySASLApplyConfiguration {
	b.Password = value
	return b
}

// WithMechanism sets the Mechanism field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Mechanism field is set to the value of the last call.
func (b *SchemaRegistrySASLApplyConfiguration) WithMechanism(value redpandav1alpha2.SASLMechanism) *SchemaRegistrySASLApplyConfiguration {
	b.Mechanism = &value
	return b
}

// WithAuthToken sets the AuthToken field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the AuthToken field is set to the value of the last call.
func (b *SchemaRegistrySASLApplyConfiguration) WithAuthToken(value *SecretKeyRefApplyConfiguration) *SchemaRegistrySASLApplyConfiguration {
	b.AuthToken = value
	return b
}
