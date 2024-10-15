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
	"slices"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/twmb/franz-go/pkg/sr"
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

func (s *schema) CompatibilityEquals(other *schema) bool {
	return s.CompatibilityLevel == other.CompatibilityLevel
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
	if !functional.CompareConvertibleSlices(s.References, other.References, schemaReferencesEqual) {
		return false
	}

	// metadata
	if s.SchemaMetadata == nil && other.SchemaMetadata != nil {
		return false
	}
	if s.SchemaMetadata != nil && other.SchemaMetadata == nil {
		return false
	}
	if s.SchemaMetadata != nil && other.SchemaMetadata != nil {
		if !functional.CompareMaps(s.SchemaMetadata.Properties, other.SchemaMetadata.Properties) {
			return false
		}
		if !functional.CompareMapsFn(s.SchemaMetadata.Tags, other.SchemaMetadata.Tags, slices.Equal) {
			return false
		}
		if !slices.Equal(s.SchemaMetadata.Sensitive, other.SchemaMetadata.Sensitive) {
			return false
		}
	}

	// rule set
	if s.SchemaRuleSet == nil && other.SchemaRuleSet != nil {
		return false
	}
	if s.SchemaRuleSet != nil && other.SchemaRuleSet == nil {
		return false
	}
	if s.SchemaRuleSet != nil && other.SchemaRuleSet != nil {
		if !functional.CompareConvertibleSlices(s.SchemaRuleSet.DomainRules, other.SchemaRuleSet.DomainRules, schemaRulesEqual) {
			return false
		}
		if !functional.CompareConvertibleSlices(s.SchemaRuleSet.MigrationRules, other.SchemaRuleSet.MigrationRules, schemaRulesEqual) {
			return false
		}
	}

	return true
}

func schemaReferencesEqual(a, b sr.SchemaReference) bool {
	if a.Name != b.Name {
		return false
	}
	if a.Subject != b.Subject {
		return false
	}
	if a.Version != b.Version {
		return false
	}
	return true
}

func schemaRulesEqual(a, b sr.SchemaRule) bool {
	if a.Name != b.Name {
		return false
	}
	if a.Doc != b.Doc {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	if a.Mode != b.Mode {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if !slices.Equal(a.Tags, b.Tags) {
		return false
	}
	if !functional.CompareMaps(a.Params, b.Params) {
		return false
	}
	if a.Expr != b.Expr {
		return false
	}
	if a.OnSuccess != b.OnSuccess {
		return false
	}
	if a.OnFailure != b.OnFailure {
		return false
	}
	if a.Disabled != b.Disabled {
		return false
	}
	return true
}
