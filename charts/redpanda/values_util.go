// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:ignore=true
package redpanda

import (
	"fmt"

	"github.com/invopop/jsonschema"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResourceQuantity is an extension of [resource.Quantity] that implements
// JSONSchemaer. It's specifically for typing a key in [TieredStorageConfig].
type ResourceQuantity resource.Quantity

func (ResourceQuantity) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "integer"},
			{Type: "string", Pattern: "^[0-9]+(\\.[0-9]){0,1}(m|k|M|G|T|P|Ki|Mi|Gi|Ti|Pi)?$"},
		},
	}
}

type IssuerRefKind string

func (IssuerRefKind) JSONSchemaExtend(schema *jsonschema.Schema) {
	schema.Enum = append(schema.Enum, "ClusterIssuer", "Issuer")
}

type ExternalListeners[T any] map[string]T

func (ExternalListeners[T]) JSONSchemaExtend(schema *jsonschema.Schema) {
	schema.PatternProperties = map[string]*jsonschema.Schema{
		`^[A-Za-z_][A-Za-z0-9_]*$`: schema.AdditionalProperties,
	}
	minProps := uint64(1)
	schema.MinProperties = &minProps
	schema.AdditionalProperties = nil
}

type HTTPAuthenticationMethod string

func (HTTPAuthenticationMethod) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = append(s.Enum, "none", "http_basic")
}

type KafkaAuthenticationMethod string

func (KafkaAuthenticationMethod) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = append(s.Enum, "sasl", "none", "mtls_identity")
}

type SASLMechanism string

func (SASLMechanism) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = append(s.Enum, "SCRAM-SHA-256", "SCRAM-SHA-512")
}

func deprecate(schema *jsonschema.Schema, keys ...string) {
	for _, key := range keys {
		prop, ok := schema.Properties.Get(key)
		if !ok {
			panic(fmt.Sprintf("missing field %q on %T", key, schema.Title))
		}
		prop.Deprecated = true
	}
}

func makeNullable(schema *jsonschema.Schema, keys ...string) {
	for _, key := range keys {
		prop, ok := schema.Properties.Get(key)
		if !ok {
			panic(fmt.Sprintf("missing field %q on %T", key, schema.Title))
		}
		schema.Properties.Set(key, &jsonschema.Schema{
			OneOf: []*jsonschema.Schema{
				prop,
				{Type: "null"},
			},
		})
	}
}
