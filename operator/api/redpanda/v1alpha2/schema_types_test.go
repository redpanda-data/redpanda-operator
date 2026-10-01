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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func testSchemaValidation(t *testing.T, c client.Client, ns string) {
	ctx := t.Context()

	baseSchema := Schema{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: ns,
		},
		Spec: SchemaSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			Text: "{}",
		},
	}

	for name, tt := range map[string]validationTestCase[*Schema]{
		"basic create": {},
		// connection params
		"no cluster source": {
			mutate: func(schema *Schema) {
				schema.Spec.ClusterSource = nil
			},
			errors: []string{`spec.cluster: required value`},
		},
		"cluster source - no cluster ref or static configuration": {
			mutate: func(schema *Schema) {
				schema.Spec.ClusterSource.ClusterRef = nil
			},
			errors: []string{`either clusterRef or staticConfiguration must be set`},
		},
		"clusterRef - static configuration no SchemaRegistry": {
			mutate: func(schema *Schema) {
				schema.Spec.ClusterSource.ClusterRef = nil
				schema.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{}
			},
			errors: []string{`spec.cluster.staticconfiguration.schemaRegistry: required value`},
		},
		"no schema text": {
			mutate: func(schema *Schema) {
				schema.Spec.Text = ""
			},
			errors: []string{`spec.text: Required value`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			runValidationTest(ctx, t, tt, c, &baseSchema)
		})
	}
}

func testSchemaDefaults(t *testing.T, c client.Client, ns string) {
	ctx := t.Context()

	require.NoError(t, c.Create(ctx, &Schema{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: ns,
		},
		Spec: SchemaSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
			Text: "{}",
		},
	}))

	var schema Schema
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "name"}, &schema))

	require.Len(t, schema.Status.Conditions, 1)
	require.Equal(t, ResourceConditionTypeSynced, schema.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionUnknown, schema.Status.Conditions[0].Status)
	require.Equal(t, ResourceConditionReasonPending, schema.Status.Conditions[0].Reason)

	require.NotNil(t, schema.Spec.Type)
	require.Equal(t, SchemaTypeAvro, *schema.Spec.Type)

	require.NotNil(t, schema.Spec.CompatibilityLevel)
	require.Equal(t, CompatabilityLevelBackward, *schema.Spec.CompatibilityLevel)
}

func TestSchemaTypeMapsInvertible(t *testing.T) {
	t.Run("schema types", func(t *testing.T) {
		requireMapInverts(t, schemaTypesFromKafka, schemaTypesToKafka)
	})
	t.Run("compatibility levels", func(t *testing.T) {
		requireMapInverts(t, compatibilityLevelsFromKafka, compatibilityLevelsToKafka)
	})
}
