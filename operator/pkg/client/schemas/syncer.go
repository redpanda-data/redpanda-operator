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
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/sr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// Syncer synchronizes Schemas for the given object to Redpanda.
type Syncer struct {
	client *sr.Client
}

// NewSyncer initializes a Syncer.
func NewSyncer(client *sr.Client) *Syncer {
	return &Syncer{
		client: client,
	}
}

// Sync synchronizes the schema in Redpanda.
func (s *Syncer) Sync(ctx context.Context, o *redpandav1alpha2.Schema) (string, []int, error) {
	versions := o.Status.Versions
	hash := o.Status.SchemaHash

	want, err := schemaFromV1Alpha2Schema(o)
	if err != nil {
		return hash, versions, err
	}

	// default to creating the schema
	createSchema := true
	// default to setting compatibility for the schema subject
	setCompatibility := true

	if !s.isInitial(o) {
		have, err := s.getLatest(ctx, o)
		if err != nil {
			return hash, versions, err
		}

		setCompatibility = have.CompatibilityLevel != want.CompatibilityLevel
		createSchema = !have.SchemaEquals(want)
	}

	if setCompatibility {
		if err := s.setCompatibility(ctx, want); err != nil {
			return hash, versions, err
		}
	}

	if createSchema {
		subjectSchema, err := s.client.CreateSchema(ctx, o.Name, want.toKafka())
		if err != nil {
			return hash, versions, err
		}
		hash = want.Hash
		versions = append(versions, subjectSchema.Version)
	}

	return hash, versions, nil
}

func (s *Syncer) isInitial(o *redpandav1alpha2.Schema) bool {
	return len(o.Status.Versions) == 0
}

func (s *Syncer) setCompatibility(ctx context.Context, sc *schema) error {
	results := s.client.SetCompatibility(ctx, sr.SetCompatibility{
		Level: sc.CompatibilityLevel,
	}, sc.Subject)
	if len(results) == 0 {
		return errors.New("empty results returned from syncing compatibility levels")
	}
	if err := results[0].Err; err != nil {
		return err
	}

	return nil
}

func (s *Syncer) getLatest(ctx context.Context, o *redpandav1alpha2.Schema) (*schema, error) {
	subjectSchema, err := s.client.SchemaByVersion(ctx, o.Name, -1)
	if err != nil {
		return nil, err
	}

	var compatibility sr.CompatibilityLevel

	results := s.client.Compatibility(ctx, o.Name)
	if len(results) > 0 {
		result := results[0]
		if err := result.Err; err != nil {
			return nil, err
		}
		compatibility = result.Level
	}

	return schemaFromRedpandaSubjectSchema(&subjectSchema, o.Status.SchemaHash, compatibility), nil
}

// Delete removes the schema in Redpanda.
func (s *Syncer) Delete(ctx context.Context, o *redpandav1alpha2.Schema) error {
	if _, err := s.client.DeleteSubject(ctx, o.Name, sr.SoftDelete); err != nil {
		return err
	}
	if _, err := s.client.DeleteSubject(ctx, o.Name, sr.HardDelete); err != nil {
		return err
	}
	return nil
}
