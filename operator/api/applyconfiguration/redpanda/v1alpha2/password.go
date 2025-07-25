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

// PasswordApplyConfiguration represents an declarative configuration of the Password type for use
// with apply.
type PasswordApplyConfiguration struct {
	Value      *string                           `json:"value,omitempty"`
	ValueFrom  *PasswordSourceApplyConfiguration `json:"valueFrom,omitempty"`
	NoGenerate *bool                             `json:"noGenerate,omitempty"`
}

// PasswordApplyConfiguration constructs an declarative configuration of the Password type for use with
// apply.
func Password() *PasswordApplyConfiguration {
	return &PasswordApplyConfiguration{}
}

// WithValue sets the Value field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Value field is set to the value of the last call.
func (b *PasswordApplyConfiguration) WithValue(value string) *PasswordApplyConfiguration {
	b.Value = &value
	return b
}

// WithValueFrom sets the ValueFrom field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ValueFrom field is set to the value of the last call.
func (b *PasswordApplyConfiguration) WithValueFrom(value *PasswordSourceApplyConfiguration) *PasswordApplyConfiguration {
	b.ValueFrom = value
	return b
}

// WithNoGenerate sets the NoGenerate field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the NoGenerate field is set to the value of the last call.
func (b *PasswordApplyConfiguration) WithNoGenerate(value bool) *PasswordApplyConfiguration {
	b.NoGenerate = &value
	return b
}
