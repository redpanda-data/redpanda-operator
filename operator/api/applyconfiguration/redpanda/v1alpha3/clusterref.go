// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha3

// ClusterRefApplyConfiguration represents an declarative configuration of the ClusterRef type for use
// with apply.
type ClusterRefApplyConfiguration struct {
	Name *string `json:"name,omitempty"`
}

// ClusterRefApplyConfiguration constructs an declarative configuration of the ClusterRef type for use with
// apply.
func ClusterRef() *ClusterRefApplyConfiguration {
	return &ClusterRefApplyConfiguration{}
}

// WithName sets the Name field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Name field is set to the value of the last call.
func (b *ClusterRefApplyConfiguration) WithName(value string) *ClusterRefApplyConfiguration {
	b.Name = &value
	return b
}
