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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/sr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	validAvroSchema = `
{
	"type": "record",
	"name": "test",
	"fields":
	[
		{
			"type": "string",
			"name": "field1"
		},
		{
			"type": "int",
			"name": "field2"
		}
	]
}
`
	validJSONSchema = `
{
	"$schema": "http://json-schema.org/draft-07/schema#",
	"type": "object",
	"properties": {
	"order_id": { "type": "string" },
	"total": { "type": "number" }
	},
	"required": ["order_id", "total"],
	"additionalProperties": false
}`
)

func normalizeSchema(t *testing.T, ctx context.Context, syncer *Syncer, schema *redpandav1alpha2.Schema) {
	actualSchema, err := syncer.getLatest(ctx, schema)
	require.NoError(t, err)
	schema.Spec.Text = actualSchema.Schema
	hash, err := schema.Spec.SchemaHash()
	require.NoError(t, err)
	schema.Status.SchemaHash = hash
}

func expectSchemasMatch(t *testing.T, ctx context.Context, syncer *Syncer, schema *redpandav1alpha2.Schema) {
	normalizeSchema(t, ctx, syncer, schema)

	expectedSchema, err := schemaFromV1Alpha2Schema(schema)
	require.NoError(t, err)

	actualSchema, err := syncer.getLatest(ctx, schema)
	require.NoError(t, err)

	require.Equal(t, expectedSchema.CompatibilityLevel, actualSchema.CompatibilityLevel, "Compatibility levels not equal %+v != %+v", actualSchema.CompatibilityLevel, expectedSchema.CompatibilityLevel)
	require.Equal(t, expectedSchema.Schema, actualSchema.Schema)
	require.True(t, expectedSchema.SchemaEquals(actualSchema), "Schemas not equal %+v != %+v", actualSchema, expectedSchema)
}

func expectSchemaUpdate(t *testing.T, ctx context.Context, syncer *Syncer, schema *redpandav1alpha2.Schema, update bool) {
	t.Helper()

	result, err := syncer.Sync(ctx, schema)
	require.NoError(t, err)

	if !update {
		require.EqualValues(t, schema.Status.Versions, result.Versions)
		require.Equal(t, schema.Status.SchemaID, result.SchemaID)
	} else {
		require.Len(t, result.Versions, len(schema.Status.Versions)+1, "update expected, but didn't create another schema version")
		require.NotZero(t, result.SchemaID)
		schema.Status.Versions = result.Versions
		schema.Status.SchemaID = result.SchemaID
	}

	expectSchemasMatch(t, ctx, syncer, schema)

	if update {
		// check to make sure we don't update again
		expectSchemaUpdate(t, ctx, syncer, schema, false)
	}
}

func TestSyncWithUnsupportedSubjectMode(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "missing endpoint", status: http.StatusNotFound},
		{name: "method not allowed", status: http.StatusMethodNotAllowed},
		{name: "not implemented", status: http.StatusNotImplemented},
		{name: "forbidden", status: http.StatusForbidden, wantErr: true},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := &redpandav1alpha2.Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec:       redpandav1alpha2.SchemaSpec{Text: validAvroSchema},
			}
			hash, err := schema.Spec.SchemaHash()
			require.NoError(t, err)
			schema.Status = redpandav1alpha2.SchemaStatus{SchemaHash: hash, SchemaID: 42, Versions: []int{1}}

			subjectSchema, err := json.Marshal(sr.SubjectSchema{
				Subject: schema.Name,
				Version: 1,
				ID:      schema.Status.SchemaID,
				Schema:  sr.Schema{Schema: schema.Spec.Text, Type: sr.TypeAvro},
			})
			require.NoError(t, err)

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/subjects/test/versions/latest":
					_, _ = writer.Write(subjectSchema)
				case "/config/test":
					_, _ = writer.Write([]byte(`{"compatibilityLevel":"BACKWARD"}`))
				case "/mode/test":
					http.Error(writer, http.StatusText(test.status), test.status)
				default:
					t.Errorf("unexpected registry request: %s %s", request.Method, request.URL.Path)
					http.Error(writer, "unexpected request", http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)

			client, err := sr.NewClient(sr.URLs(server.URL))
			require.NoError(t, err)
			result, err := NewSyncer(client).Sync(t.Context(), schema)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, schema.Status.SchemaID, result.SchemaID)
			require.Equal(t, schema.Status.Versions, result.Versions)
		})
	}
}

func TestRestoreSkipsChangedSchemaText(t *testing.T) {
	want, err := schemaFromV1Alpha2Schema(&redpandav1alpha2.Schema{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       redpandav1alpha2.SchemaSpec{Text: validAvroSchema},
	})
	require.NoError(t, err)

	old := redpandav1alpha2.SchemaSpec{Text: validJSONSchema}
	oldHash, err := old.SchemaHash()
	require.NoError(t, err)

	for _, hash := range []string{oldHash, ""} {
		status := redpandav1alpha2.SchemaStatus{SchemaHash: hash, SchemaID: 42, Versions: []int{2}}
		restored, err := NewSyncer(nil).restore(t.Context(), want, status)
		require.NoError(t, err)
		require.Nil(t, restored)
	}
}

// TestSyncWithMissingSubject drives Sync against a registry that has lost the
// subject, covering the outcomes of a restore attempt. The status must only
// change once the schema is registered again, so that a transient failure never
// forfeits the chance to restore the previous ID.
func TestSyncWithMissingSubject(t *testing.T) {
	schema := &redpandav1alpha2.Schema{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       redpandav1alpha2.SchemaSpec{Text: validAvroSchema},
	}
	hash, err := schema.Spec.SchemaHash()
	require.NoError(t, err)
	recorded := redpandav1alpha2.SchemaStatus{SchemaHash: hash, SchemaID: 42, Versions: []int{1, 2}}
	unchanged := SyncResult{Hash: hash, SchemaID: 42, Versions: []int{1, 2}}

	// Statuses are HTTP status codes, Schema Registry error codes, or 0 when
	// the request must not be made.
	for _, test := range []struct {
		name     string
		mode     int // PUT /mode/{subject}
		imported int // POST /subjects/{subject}/versions with an explicit ID
		reset    int // DELETE /mode/{subject}
		config   int // PUT /config/{subject}
		created  int // POST /subjects/{subject}/versions without an ID
		want     SyncResult
		wantErr  bool
	}{
		{
			name: "restores under the previous ID",
			mode: http.StatusOK, imported: http.StatusOK, reset: http.StatusOK, config: http.StatusOK,
			want: SyncResult{Hash: hash, SchemaID: 42, Versions: []int{2}},
		},
		{
			name: "registers afresh when the ID is refused",
			mode: http.StatusOK, imported: sr.ErrOperationNotPermitted.Code, reset: http.StatusOK, config: http.StatusOK, created: http.StatusOK,
			want: SyncResult{Hash: hash, SchemaID: 7, Versions: []int{1}},
		},
		{
			name: "registers afresh without subject mode support",
			mode: http.StatusNotFound, config: http.StatusOK, created: http.StatusOK,
			want: SyncResult{Hash: hash, SchemaID: 7, Versions: []int{1}},
		},
		{
			name: "keeps the status when the mode change is rate limited",
			mode: http.StatusTooManyRequests,
			want: unchanged, wantErr: true,
		},
		{
			name: "keeps the status when the mode change is unauthorized",
			mode: http.StatusUnauthorized,
			want: unchanged, wantErr: true,
		},
		{
			name: "keeps the status when leaving import mode fails",
			mode: http.StatusOK, imported: http.StatusOK, reset: http.StatusInternalServerError,
			want: unchanged, wantErr: true,
		},
		{
			name: "keeps the status when setting compatibility fails",
			mode: http.StatusNotFound, config: http.StatusInternalServerError,
			want: unchanged, wantErr: true,
		},
		{
			name: "keeps the status when registering afresh fails",
			mode: http.StatusNotFound, config: http.StatusOK, created: http.StatusInternalServerError,
			want: unchanged, wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.Method + " " + request.URL.Path {
				case "GET /subjects/test/versions/latest":
					respond(t, writer, request, sr.ErrSubjectNotFound.Code, "")
				case "PUT /mode/test":
					respond(t, writer, request, test.mode, `{"mode":"IMPORT"}`)
				case "DELETE /mode/test":
					respond(t, writer, request, test.reset, `{"mode":"IMPORT"}`)
				case "PUT /config/test":
					respond(t, writer, request, test.config, `{"compatibility":"BACKWARD"}`)
				case "POST /subjects/test/versions":
					var body sr.SubjectSchema
					require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
					if body.ID != 0 {
						require.Equal(t, recorded.SchemaID, body.ID)
						require.Equal(t, recorded.Versions[1], body.Version)
						respond(t, writer, request, test.imported, `{"id":42}`)
					} else {
						respond(t, writer, request, test.created, `{"id":7}`)
					}
				case "GET /schemas/ids/42/versions":
					respond(t, writer, request, http.StatusOK, `[{"subject":"test","version":2}]`)
				case "GET /schemas/ids/7/versions":
					respond(t, writer, request, http.StatusOK, `[{"subject":"test","version":1}]`)
				case "GET /subjects/test/versions/2":
					respond(t, writer, request, http.StatusOK, `{"subject":"test","version":2,"id":42,"schema":"{}"}`)
				case "GET /subjects/test/versions/1":
					respond(t, writer, request, http.StatusOK, `{"subject":"test","version":1,"id":7,"schema":"{}"}`)
				default:
					respond(t, writer, request, 0, "")
				}
			}))
			t.Cleanup(server.Close)

			client, err := sr.NewClient(sr.URLs(server.URL))
			require.NoError(t, err)

			schema := schema.DeepCopy()
			schema.Status = recorded
			result, err := NewSyncer(client).Sync(t.Context(), schema)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, result)
		})
	}
}

func TestSyncer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v25.3.17",
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("user"),
		redpanda.WithNewServiceAccount("user", "password"),
	)

	require.NoError(t, err)

	schemaRegistry, err := container.SchemaRegistryAddress(ctx)
	require.NoError(t, err)

	schemaRegistryClient, err := sr.NewClient(sr.BasicAuth("user", "password"), sr.URLs(schemaRegistry))
	require.NoError(t, err)

	syncer := NewSyncer(schemaRegistryClient)

	// a schema of the other type is guaranteed to differ from the one under test
	otherSchema := map[redpandav1alpha2.SchemaType]sr.Schema{
		redpandav1alpha2.SchemaTypeAvro: {Schema: validJSONSchema, Type: sr.TypeJSON},
		redpandav1alpha2.SchemaTypeJSON: {Schema: validAvroSchema, Type: sr.TypeAvro},
	}
	updatedSchemaText := map[redpandav1alpha2.SchemaType]string{
		redpandav1alpha2.SchemaTypeAvro: strings.Replace(validAvroSchema, "field1", "changed_field", 1),
		redpandav1alpha2.SchemaTypeJSON: strings.ReplaceAll(validJSONSchema, "order_id", "purchase_id"),
	}

	for schemaType, schemaText := range map[redpandav1alpha2.SchemaType]string{
		redpandav1alpha2.SchemaTypeAvro: validAvroSchema,
		redpandav1alpha2.SchemaTypeJSON: validJSONSchema,
	} {
		t.Run(string(schemaType), func(t *testing.T) {
			schema := &redpandav1alpha2.Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name: "schema-" + string(schemaType),
				},
				Spec: redpandav1alpha2.SchemaSpec{
					Type: ptr.To(schemaType),
					Text: schemaText,
				},
			}

			reference := &redpandav1alpha2.Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name: "reference" + string(schemaType),
				},
				Spec: redpandav1alpha2.SchemaSpec{
					Type: ptr.To(schemaType),
					Text: schemaText,
				},
			}

			// create initial schema and reference
			expectSchemaUpdate(t, ctx, syncer, schema, true)
			expectSchemaUpdate(t, ctx, syncer, reference, true)

			// update references
			schema.Spec.References = []redpandav1alpha2.SchemaReference{
				{
					Subject: reference.Name,
					Name:    "test",
					Version: 1,
				},
			}
			expectSchemaUpdate(t, ctx, syncer, schema, true)

			// update compatibility level
			schema.Spec.CompatibilityLevel = ptr.To(redpandav1alpha2.CompatabilityLevelFull)
			expectSchemaUpdate(t, ctx, syncer, schema, false)

			// TODO: Request from core support for the following
			// https://github.com/redpanda-data/redpanda/issues/23548
			//   - update schema rules: rules not supported
			//   - update metadata: metadata is not supported
			// update normalization: normalization is not supported

			// the subject disappearing out of band, e.g. because the cluster's
			// data directory was wiped, brings the schema back under its previous
			// ID and version so that clients with cached IDs keep working
			previousID := schema.Status.SchemaID
			previousVersion := schema.Status.Versions[len(schema.Status.Versions)-1]
			hardDeleteSubject(t, ctx, schemaRegistryClient, schema.Name)
			result := expectSchemaRestored(t, ctx, syncer, schema)
			require.Equal(t, previousID, result.SchemaID)
			require.Equal(t, []int{previousVersion}, result.Versions)

			hardDeleteSubject(t, ctx, schemaRegistryClient, schema.Name)
			schema.Spec.Text = updatedSchemaText[schemaType]
			result = expectSchemaRestored(t, ctx, syncer, schema)
			require.NotEqual(t, previousID, result.SchemaID)
			require.Len(t, result.Versions, 1)
			previousID = result.SchemaID

			// a previous ID that has since been taken by a different schema
			// cannot be reused, so a new one gets assigned
			hardDeleteSubject(t, ctx, schemaRegistryClient, schema.Name)
			importSchema(t, ctx, schemaRegistryClient, "squatter-"+string(schemaType), otherSchema[schemaType], previousID, 1)
			result = expectSchemaRestored(t, ctx, syncer, schema)
			require.NotEqual(t, previousID, result.SchemaID)
			require.Len(t, result.Versions, 1)

			// a status that predates ID tracking can only be registered afresh
			hardDeleteSubject(t, ctx, schemaRegistryClient, schema.Name)
			schema.Status.SchemaID = 0
			result = expectSchemaRestored(t, ctx, syncer, schema)
			require.NotZero(t, result.SchemaID)
			require.Len(t, result.Versions, 1)

			// import mode and a missing compatibility level left behind by an
			// interrupted restore are repaired on the next sync
			hardDeleteSubject(t, ctx, schemaRegistryClient, schema.Name)
			want, err := schemaFromV1Alpha2Schema(schema)
			require.NoError(t, err)
			importSchema(t, ctx, schemaRegistryClient, schema.Name, want.toKafka(), schema.Status.SchemaID, schema.Status.Versions[0])
			expectSchemaUpdate(t, ctx, syncer, schema, false)
			expectNoSubjectMode(t, ctx, schemaRegistryClient, schema.Name)

			// delete
			err = syncer.Delete(ctx, schema)
			require.NoError(t, err)

			subjects, err := schemaRegistryClient.Subjects(ctx)
			require.NoError(t, err)
			require.NotContains(t, subjects, schema.Name)
		})
	}
}

// expectSchemaRestored syncs a schema whose subject is missing from the
// registry and checks that the sync converges and leaves no subject mode behind.
func expectSchemaRestored(t *testing.T, ctx context.Context, syncer *Syncer, schema *redpandav1alpha2.Schema) SyncResult {
	t.Helper()

	result, err := syncer.Sync(ctx, schema)
	require.NoError(t, err)
	require.NotZero(t, result.SchemaID)
	require.NotEmpty(t, result.Versions)
	expectNoSubjectMode(t, ctx, syncer.client, schema.Name)

	// persist the result as the controller would
	schema.Status.SchemaHash = result.Hash
	schema.Status.SchemaID = result.SchemaID
	schema.Status.Versions = result.Versions
	expectSchemaUpdate(t, ctx, syncer, schema, false)

	return result
}

func hardDeleteSubject(t *testing.T, ctx context.Context, client *sr.Client, subject string) {
	t.Helper()

	_, err := client.DeleteSubject(ctx, subject, sr.SoftDelete)
	require.NoError(t, err)
	_, err = client.DeleteSubject(ctx, subject, sr.HardDelete)
	require.NoError(t, err)

	_, err = client.SchemaByVersion(ctx, subject, -1)
	require.True(t, isSchemaError(err, sr.ErrSubjectNotFound), "expected subject %q to be gone, got: %v", subject, err)
}

// importSchema registers a schema under an explicit ID and version, leaving the
// subject in import mode.
func importSchema(t *testing.T, ctx context.Context, client *sr.Client, subject string, schema sr.Schema, id, version int) {
	t.Helper()

	results := client.SetMode(ctx, sr.ModeImport, subject)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)

	_, err := client.CreateSchemaWithIDAndVersion(ctx, subject, schema, id, version)
	require.NoError(t, err)
}

func expectNoSubjectMode(t *testing.T, ctx context.Context, client *sr.Client, subject string) {
	t.Helper()

	results := client.Mode(ctx, subject)
	require.Len(t, results, 1)
	require.True(t, isSchemaError(results[0].Err, sr.ErrSubjectLevelModeNotConfigured), "expected no subject-level mode for %q, got mode %v, err: %v", subject, results[0].Mode, results[0].Err)
}

// respond writes a fake Schema Registry response: a success body, an HTTP
// error, or a Schema Registry error whose HTTP status is encoded in the
// leading digits of its code. A status of 0 marks a request that must not be
// made.
func respond(t *testing.T, writer http.ResponseWriter, request *http.Request, status int, body string) {
	t.Helper()

	switch {
	case status == 0:
		t.Errorf("unexpected registry request: %s %s", request.Method, request.URL.Path)
		http.Error(writer, "unexpected request", http.StatusInternalServerError)
	case status >= 10000:
		writer.WriteHeader(status / 100)
		_, _ = fmt.Fprintf(writer, `{"error_code":%d,"message":"test"}`, status)
	case status >= http.StatusBadRequest:
		http.Error(writer, http.StatusText(status), status)
	default:
		_, _ = io.WriteString(writer, body)
	}
}
