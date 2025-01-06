// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import "sigs.k8s.io/controller-runtime/pkg/client"

// The interfaces below are leveraged for consistently
// creating clients to the internal cluster APIs in
// our internal client factory code. Any CRD for which
// reconciliation requires configuring a cluster should
// implement some subset of these interfaces.

// KafkaConnectedObject is an interface for an object
// that specifies connection parameters to a Kafka API
// in the form of a KafkaAPISpec somewhere in its CRD definition.
// +kubebuilder:object:generate=false
type KafkaConnectedObject interface {
	client.Object
	GetKafkaAPISpec() *KafkaAPISpec
}

// KafkaConnectedObjectWithMetrics is an interface for an object
// that specifies connection parameters to a Kafka API in the form
// of a KafkaAPISpec somewhere in its CRD definition as well as
// an overriding namespace for Kafka client metrics.
// +kubebuilder:object:generate=false
type KafkaConnectedObjectWithMetrics interface {
	KafkaConnectedObject
	GetMetricsNamespace() *string
}

// ClusterReferencingObject is an interface for an object
// that specifies connection parameters to a Redpanda cluster,
// (both the Kafka and Admin APIs) in the form of an ClusterRef
// somewhere in its CRD definition.
// +kubebuilder:object:generate=false
type ClusterReferencingObject interface {
	client.Object
	GetClusterSource() *ClusterSource
}

// AuthorizedObject is an interface for an object
// that specifies ACLs, currently only Users are supported,
// but this can also be used for groups.
// +kubebuilder:object:generate=false
type AuthorizedObject interface {
	client.Object
	GetACLs() []ACLRule
	GetPrincipal() string
}
