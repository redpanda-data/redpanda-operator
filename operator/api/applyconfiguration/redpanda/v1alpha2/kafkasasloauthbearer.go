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

// KafkaSASLOAuthBearerApplyConfiguration represents an declarative configuration of the KafkaSASLOAuthBearer type for use
// with apply.
type KafkaSASLOAuthBearerApplyConfiguration struct {
	Token *SecretKeyRefApplyConfiguration `json:"tokenSecretRef,omitempty"`
}

// KafkaSASLOAuthBearerApplyConfiguration constructs an declarative configuration of the KafkaSASLOAuthBearer type for use with
// apply.
func KafkaSASLOAuthBearer() *KafkaSASLOAuthBearerApplyConfiguration {
	return &KafkaSASLOAuthBearerApplyConfiguration{}
}

// WithToken sets the Token field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Token field is set to the value of the last call.
func (b *KafkaSASLOAuthBearerApplyConfiguration) WithToken(value *SecretKeyRefApplyConfiguration) *KafkaSASLOAuthBearerApplyConfiguration {
	b.Token = value
	return b
}