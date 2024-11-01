// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package schemas

import (
	"reflect"
	"slices"

	"github.com/twmb/franz-go/pkg/sr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

type schema struct {
	Subject            string
	CompatibilityLevel sr.CompatibilityLevel
	Schema             string
	Type               sr.SchemaType
	References         []sr.SchemaReference
	SchemaMetadata     *sr.SchemaMetadata
	SchemaRuleSet      *sr.SchemaRuleSet
	Hash               string
}

func (s *schema) toKafka() sr.Schema {
	return sr.Schema{
		Schema:         s.Schema,
		Type:           s.Type,
		References:     s.References,
		SchemaMetadata: s.SchemaMetadata,
		SchemaRuleSet:  s.SchemaRuleSet,
	}
}

func schemaFromV1Alpha2Schema(s *redpandav1alpha2.Schema) (*schema, error) {
	hash, err := s.Spec.SchemaHash()
	if err != nil {
		return nil, err
	}
	return &schema{
		Subject:            s.Name,
		CompatibilityLevel: s.Spec.GetCompatibilityLevel().ToKafka(),
		Schema:             s.Spec.Text,
		Type:               s.Spec.GetType().ToKafka(),
		References:         functional.MapFn(redpandav1alpha2.SchemaReferenceToKafka, s.Spec.References),
		Hash:               hash,
	}, nil
}

func schemaFromRedpandaSubjectSchema(s *sr.SubjectSchema, hash string, compatibility sr.CompatibilityLevel) *schema {
	return &schema{
		Subject:            s.Subject,
		CompatibilityLevel: compatibility,
		Schema:             s.Schema.Schema,
		Type:               s.Type,
		References:         s.References,
		Hash:               hash,
	}
}

func (s *schema) SchemaEquals(other *schema) bool {
	// subject
	if s.Subject != other.Subject {
		return false
	}

	// type
	if s.Type != other.Type {
		return false
	}

	// schema
	// we cheat here, rather than trying to match the normalized schema in the cluster
	// we instead just check to see if we've changed at all in the CRD
	if s.Hash != other.Hash {
		return false
	}

	// references
	if !slices.Equal(s.References, other.References) {
		return false
	}

	// metadata
	if !reflect.DeepEqual(s.SchemaMetadata, other.SchemaMetadata) {
		return false
	}

	// rule set
	if !reflect.DeepEqual(s.SchemaRuleSet, other.SchemaRuleSet) {
		return false
	}

	return true
}
