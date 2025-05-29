// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha3

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
)

// ClusterRef represents a reference to a cluster that is being targeted.
type ClusterRef struct {
	// Name specifies the name of the cluster being referenced.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

type YAMLRepresentation string

// ValueSource is a generic "value" type that permits sourcing the actual
// (runtime) value from a variety of sources.
// In most cases, this should always output a CEL expression that's resolved at runtime.
// Example use cases are:
// - ClusterConfig e.g. Secret values
// - NodeConfig e.g. Dynamic advertised_host
// - RPKConfig e.g. Runtime resolved listing of broker addresses via SRV records.
type ValueSource struct {
	Value           *YAMLRepresentation          `json:"value,omitempty"`
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	SecretKeyRef    *corev1.SecretKeySelector    `json:"secretKeyRef,omitempty"`

	// TODO: Renable once support is added
	// Expr            Expr                         `json:"expr,omitempty"`

	// TODO this is just to make conversions between ClusterConfigValue and this type work for now.
	// Ideally we'd shove things into the Expr field and parse it as need be.
	UseRawValue bool `json:"useRawValue,omitempty"`
}

// CEL Expr for more complex values
// Examples:
// - rack: Expr(node_annotation('k8s.io/failure-domain')),
// - addresses: Expr(srv_address('tcp', 'admin', 'redpanda.redpanda.cluster.svc.cluster.local'))
type Expr string

type ObjectMeta struct {
	// Labels is a map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

type PodTemplate struct {
	ObjectMeta `json:"metadata,omitempty"`
	Spec       *applycorev1.PodSpecApplyConfiguration `json:"spec,omitempty"`
}

func (t *PodTemplate) DeepCopy() *PodTemplate {
	// For some inexplicable reason, apply configs don't have deepcopy
	// generated for them.
	//
	// DeepCopyInto can be generated with just DeepCopy implemented. Sadly, the
	// easiest way to implement DeepCopy is to run this type through JSON. It's
	// highly unlikely that we'll hit a panic but it is possible to do so with
	// invalid values for resource.Quantity and the like.
	out := new(PodTemplate)
	data, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}
