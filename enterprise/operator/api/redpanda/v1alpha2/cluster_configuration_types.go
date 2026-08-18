// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
)

// NOTE: these types are forked from the OSS operator's
// vectorized/v1alpha1 package (cluster_types.go), where the OSS
// redpanda/v1alpha2 package sources its ClusterConfiguration type from.
// ExternalSecretKeySelector is renamed to ConfigExternalSecretKeySelector
// here. Definitions, kubebuilder markers, json tags, and doc comments are
// otherwise copied verbatim so the generated CRD schemas remain
// byte-identical.

// ClusterConfiguration holds values (or references to values) that should be used
// to configure the cluster. Where the cluster schema defines a non-string type for a
// given key, the corresponding values here should be string-encoded (according to yaml
// rules)
type ClusterConfiguration map[string]ClusterConfigValue

// YAMLRepresentation holds a serialised form of a concrete value. We need this for
// a couple of reasons: firstly, "stringifying" numbers avoids loss of accuracy and
// rendering issues where intermediate values are represented as f64 values by
// external tooling. Secondly, the initial configuration of a bootstrap file has
// no running cluster - and therefore no online schema - available. Instead we use
// representations that can be inserted verbatim into a YAML document.
// Ideally, these will be JSON-encoded into a single line representation. They are
// decoded using YAML deserialisation (which has a little more flexibility around
// the representation of unambiguous string values).
type YAMLRepresentation string

// ClusterConfigValue represents a value of arbitrary type T. Values are string-encoded according to
// YAML rules in order to preserve numerical fidelity.
// Because these values must be embedded in a `.bootstrap.yaml` file - during the processing of
// which, the AdminAPI's schema is unavailable - we endeavour to use yaml-compatible representations
// throughout. The octet sequence of a representation will be inserted into a bootstrap template
// verbatim.
type ClusterConfigValue struct {
	// If the value is directly known, its YAML-compatible representation can be embedded here.
	// Use the string representation of a serialised value in order to preserve accuracy.
	// Prefer JSON-encoding for values that have multi-line representations in YAML.
	// Example:
	// The string "foo" should be the five octets "\"foo\""
	// A true value should be the four octets "true".
	// The number -123456 should be a seven-octet sequence, "-123456".
	Repr *YAMLRepresentation `json:"repr,omitempty"`
	// If the value is supplied by a kubernetes object reference, coordinates are embedded here.
	// For target values, the string value fetched from the source will be treated as
	// a raw string (and appropriately quoted for use in the bootstrap file) unless `useRawValue` is set.
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Should the value be contained in a k8s secret rather than configmap, we can refer
	// to it here.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// Deprecated: replaced by "externalSecretRefSelector"; this field should be *REMOVED* ASAP since it's barely
	// made it into service.
	ExternalSecretRef *string `json:"externalSecretRef,omitempty"`
	// If the value is supplied by an external source, coordinates are embedded here.
	// Note: we interpret all fetched external secrets as raw string values by default
	// and yam-encode them prior to embedding. To disable that behaviour, set `useRawValue`.
	ExternalSecretRefSelector *ConfigExternalSecretKeySelector `json:"externalSecretRefSelector,omitempty"`
	// Any referenced value (from kubernetes or external lookup) is typically considered to be a raw string;
	// by default it'll be quoted as a string after its lookup is resolved. To skip that behaviour,
	// and to consider the external value verbatim (ie, if it's already in an appropriate serialized form
	// for use in the bootstrap configuration), set useRawValue to true.
	// In particular, this value should be set to `true` if your external source contains a numeric value.
	UseRawValue bool `json:"useRawValue,omitempty"`
}

// ConfigExternalSecretKeySelector selects a key of an external Secret.
// +structType=atomic
type ConfigExternalSecretKeySelector struct {
	Name string `json:"name"`
	// Specify whether the Secret or its key must be defined
	// +optional
	Optional *bool `json:"optional,omitempty"`
}
