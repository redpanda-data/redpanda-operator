// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestSchemaValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	baseSchema := Schema{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
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

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

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

func TestSchemaDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	require.NoError(t, c.Create(ctx, &Schema{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
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
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &schema))

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
