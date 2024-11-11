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
	"testing"
	"time"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/sr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

func normalizeSchema(t *testing.T, ctx context.Context, syncer *Syncer, schema *v1alpha2.Schema) {
	actualSchema, err := syncer.getLatest(ctx, schema)
	require.NoError(t, err)
	schema.Spec.Text = actualSchema.Schema
	hash, err := schema.Spec.SchemaHash()
	require.NoError(t, err)
	schema.Status.SchemaHash = hash
}

func expectSchemasMatch(t *testing.T, ctx context.Context, syncer *Syncer, schema *v1alpha2.Schema) {
	normalizeSchema(t, ctx, syncer, schema)

	expectedSchema, err := schemaFromV1Alpha2Schema(schema)
	require.NoError(t, err)

	actualSchema, err := syncer.getLatest(ctx, schema)
	require.NoError(t, err)

	require.Equal(t, expectedSchema.CompatibilityLevel, actualSchema.CompatibilityLevel, "Compatibility levels not equal %+v != %+v", actualSchema.CompatibilityLevel, expectedSchema.CompatibilityLevel)
	require.Equal(t, expectedSchema.Schema, actualSchema.Schema)
	require.True(t, expectedSchema.SchemaEquals(actualSchema), "Schemas not equal %+v != %+v", actualSchema, expectedSchema)
}

func expectSchemaUpdate(t *testing.T, ctx context.Context, syncer *Syncer, schema *v1alpha2.Schema, update bool) {
	t.Helper()

	_, versions, err := syncer.Sync(ctx, schema)
	require.NoError(t, err)

	if !update {
		require.EqualValues(t, schema.Status.Versions, versions)
	} else {
		require.Len(t, versions, len(schema.Status.Versions)+1, "update expected, but didn't create another schema version")
		schema.Status.Versions = versions
	}

	expectSchemasMatch(t, ctx, syncer, schema)

	if update {
		// check to make sure we don't update again
		expectSchemaUpdate(t, ctx, syncer, schema, false)
	}
}

func TestSyncer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v24.2.10",
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

	for schemaType, schemaText := range map[v1alpha2.SchemaType]string{
		v1alpha2.SchemaTypeAvro: validAvroSchema,
		v1alpha2.SchemaTypeJSON: validJSONSchema,
	} {
		t.Run(string(schemaType), func(t *testing.T) {
			schema := &v1alpha2.Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name: "schema-" + string(schemaType),
				},
				Spec: v1alpha2.SchemaSpec{
					Type: ptr.To(schemaType),
					Text: schemaText,
				},
			}

			reference := &v1alpha2.Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name: "reference" + string(schemaType),
				},
				Spec: v1alpha2.SchemaSpec{
					Type: ptr.To(schemaType),
					Text: schemaText,
				},
			}

			// create initial schema and reference
			expectSchemaUpdate(t, ctx, syncer, schema, true)
			expectSchemaUpdate(t, ctx, syncer, reference, true)

			// update references
			schema.Spec.References = []v1alpha2.SchemaReference{
				{
					Subject: reference.Name,
					Name:    "test",
					Version: 1,
				},
			}
			expectSchemaUpdate(t, ctx, syncer, schema, true)

			// update compatibility level
			schema.Spec.CompatibilityLevel = ptr.To(v1alpha2.CompatabilityLevelFull)
			expectSchemaUpdate(t, ctx, syncer, schema, false)

			// TODO: Request from core support for the following
			// https://github.com/redpanda-data/redpanda/issues/23548
			//   - update schema rules: rules not supported
			//   - update metadata: metadata is not supported
			// update normalization: normalization is not supported

			// delete
			err = syncer.Delete(ctx, schema)
			require.NoError(t, err)

			subjects, err := schemaRegistryClient.Subjects(ctx)
			require.NoError(t, err)
			require.NotContains(t, subjects, schema.Name)
		})
	}
}
