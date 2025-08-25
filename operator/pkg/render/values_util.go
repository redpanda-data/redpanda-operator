// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

import (
	"fmt"

	"github.com/invopop/jsonschema"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	SASLKafkaAuthenticationMethod         KafkaAuthenticationMethod = "sasl"
	MTLSIdentityKafkaAuthenticationMethod KafkaAuthenticationMethod = "mtls_identity"
	NoneKafkaAuthenticationMethod         KafkaAuthenticationMethod = "none"

	BasicHTTPAuthenticationMethod HTTPAuthenticationMethod = "http_basic"
	NoneHTTPAuthenticationMethod  HTTPAuthenticationMethod = "none"
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

type NoAuth string

func (NoAuth) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = []any{} // No valid options.
}

type HTTPAuthenticationMethod string

func (HTTPAuthenticationMethod) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = append(s.Enum, NoneHTTPAuthenticationMethod, BasicHTTPAuthenticationMethod)
}

type KafkaAuthenticationMethod string

func (KafkaAuthenticationMethod) JSONSchemaExtend(s *jsonschema.Schema) {
	s.Enum = append(s.Enum, SASLKafkaAuthenticationMethod, MTLSIdentityKafkaAuthenticationMethod, NoneKafkaAuthenticationMethod)
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
