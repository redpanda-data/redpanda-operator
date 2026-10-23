// Copyright 2026 Redpanda Data, Inc.
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
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/twmb/franz-go/pkg/sr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// Syncer synchronizes Schemas for the given object to Redpanda.
type Syncer struct {
	client *sr.Client
}

// SyncResult holds the status fields that Sync maintains for a Schema.
type SyncResult struct {
	Hash     string
	SchemaID int
	Versions []int
}

// NewSyncer initializes a Syncer.
func NewSyncer(client *sr.Client) *Syncer {
	return &Syncer{
		client: client,
	}
}

// Sync synchronizes the schema in Redpanda. The returned result describes what
// is registered after the call, including when an error is returned.
func (s *Syncer) Sync(ctx context.Context, o *redpandav1alpha2.Schema) (SyncResult, error) {
	result := SyncResult{
		Hash:     o.Status.SchemaHash,
		SchemaID: o.Status.SchemaID,
		Versions: o.Status.Versions,
	}

	want, err := schemaFromV1Alpha2Schema(o)
	if err != nil {
		return result, err
	}

	// default to creating the schema
	createSchema := true
	// default to setting compatibility for the schema subject
	setCompatibility := true
	// The recorded versions are stale once the subject is gone, but they are
	// what a later restore attempt needs, so they are only discarded once a
	// fresh registration has succeeded.
	discardVersions := false

	if !s.isInitial(o) {
		have, err := s.getLatest(ctx, o)
		switch {
		case isSchemaError(err, sr.ErrSubjectNotFound):
			// The subject was registered previously but no longer exists in the
			// registry, e.g. because the cluster's data directory was wiped.
			// Clients cache schema IDs, so bring the schema back under its
			// previous ID where the schema text is unchanged and the registry
			// allows it.
			restored, err := s.restore(ctx, want, o.Status)
			switch {
			case err != nil:
				return result, err
			case restored == nil:
				discardVersions = true
			default:
				// The ID and hash are unchanged by construction.
				createSchema = false
				result.Versions = []int{restored.Version}
			}
		case err != nil:
			return result, err
		default:
			setCompatibility = have.CompatibilityLevel != want.CompatibilityLevel
			createSchema = !have.SchemaEquals(want)
			result.SchemaID = have.ID
			// Import mode is left behind when a restore could not reset it and
			// rejects any registration until it is cleared.
			if have.ImportMode {
				if err := s.resetMode(ctx, want.Subject); err != nil {
					return result, err
				}
			}
		}
	}

	if setCompatibility {
		if err := s.setCompatibility(ctx, want); err != nil {
			return result, err
		}
	}

	if createSchema {
		subjectSchema, err := s.client.CreateSchema(ctx, o.Name, want.toKafka())
		if err != nil {
			return result, err
		}
		if discardVersions {
			result.Versions = nil
		}
		result.Hash = want.Hash
		result.SchemaID = subjectSchema.ID
		result.Versions = append(result.Versions, subjectSchema.Version)
	}

	return result, nil
}

func (s *Syncer) isInitial(o *redpandav1alpha2.Schema) bool {
	return len(o.Status.Versions) == 0
}

// restore registers a schema whose subject has vanished from the registry under
// the ID and version recorded in status. It returns nil, without an error, when
// the schema cannot be restored: the recorded ID identified different schema
// text, the registry refuses the ID, or the cluster predates import mode. The
// caller then has to register the schema afresh.
func (s *Syncer) restore(ctx context.Context, want *schema, status redpandav1alpha2.SchemaStatus) (*sr.SubjectSchema, error) {
	if status.SchemaID == 0 || len(status.Versions) == 0 || status.SchemaHash != want.Hash {
		return nil, nil
	}
	version := status.Versions[len(status.Versions)-1]

	// Explicit IDs are only accepted in import mode.
	if err := s.setMode(ctx, sr.ModeImport, want.Subject); err != nil {
		if isSchemaError(err, sr.ErrInvalidMode) || isUnsupportedMode(err) {
			return nil, nil
		}
		return nil, err
	}

	restored, err := s.client.CreateSchemaWithIDAndVersion(ctx, want.Subject, want.toKafka(), status.SchemaID, version)
	// Registration without an explicit ID is rejected while import mode is
	// set, so leave it whatever the outcome.
	if resetErr := s.resetMode(ctx, want.Subject); resetErr != nil {
		return nil, errors.CombineErrors(err, resetErr)
	}
	switch {
	case isSchemaError(err, sr.ErrOperationNotPermitted), isSchemaError(err, sr.ErrIDDoesNotMatch):
		// The ID now identifies a different schema.
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &restored, nil
}

func (s *Syncer) setCompatibility(ctx context.Context, sc *schema) error {
	result, err := firstResult(s.client.SetCompatibility(ctx, sr.SetCompatibility{
		Level: sc.CompatibilityLevel,
	}, sc.Subject), "syncing compatibility levels")
	if err != nil {
		return err
	}
	return result.Err
}

func (s *Syncer) setMode(ctx context.Context, mode sr.Mode, subject string) error {
	return modeErr(s.client.SetMode(ctx, mode, subject), "syncing subject mode")
}

func (s *Syncer) resetMode(ctx context.Context, subject string) error {
	return modeErr(s.client.ResetMode(ctx, subject), "resetting subject mode")
}

func modeErr(results []sr.ModeResult, what string) error {
	result, err := firstResult(results, what)
	if err != nil {
		return err
	}
	return result.Err
}

func (s *Syncer) getLatest(ctx context.Context, o *redpandav1alpha2.Schema) (*schema, error) {
	subjectSchema, err := s.client.SchemaByVersion(ctx, o.Name, -1)
	if err != nil {
		return nil, err
	}

	// A subject without subject-level compatibility falls back to the global
	// level, which is never what the Schema asks for, so leaving the level
	// unset here makes Sync configure it.
	var compatibility sr.CompatibilityLevel

	results := s.client.Compatibility(ctx, o.Name)
	if len(results) > 0 {
		result := results[0]
		if err := result.Err; err != nil && !isSchemaError(err, sr.ErrSubjectLevelCompatibilityNotConfigured) {
			return nil, err
		}
		compatibility = result.Level
	}

	importMode, err := s.inImportMode(ctx, o.Name)
	if err != nil {
		return nil, err
	}

	return schemaFromRedpandaSubjectSchema(&subjectSchema, o.Status.SchemaHash, compatibility, importMode), nil
}

func (s *Syncer) inImportMode(ctx context.Context, subject string) (bool, error) {
	result, err := firstResult(s.client.Mode(ctx, subject), "fetching subject mode")
	if err != nil {
		return false, err
	}
	if isSchemaError(result.Err, sr.ErrSubjectLevelModeNotConfigured) || isUnsupportedMode(result.Err) {
		return false, nil
	}
	if result.Err != nil {
		return false, result.Err
	}
	return result.Mode == sr.ModeImport, nil
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

// firstResult unwraps the per-subject results that franz-go returns for a
// request made for a single subject.
func firstResult[T any](results []T, what string) (T, error) {
	if len(results) == 0 {
		var zero T
		return zero, errors.Newf("empty results returned from %s", what)
	}
	return results[0], nil
}

func isSchemaError(err error, target *sr.Error) bool {
	var responseErr *sr.ResponseError
	return errors.As(err, &responseErr) && responseErr.SchemaError() == target
}

// isUnsupportedMode reports whether the registry lacks the subject mode
// endpoint, as Redpanda did before subject-level modes were introduced.
func isUnsupportedMode(err error) bool {
	var responseErr *sr.ResponseError
	if !errors.As(err, &responseErr) {
		return false
	}
	switch responseErr.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}
