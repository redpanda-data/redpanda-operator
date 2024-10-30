// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSchemaReconcile(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	environment := InitializeResourceReconcilerTest(t, ctx, &SchemaReconciler{})

	baseSchema := &redpandav1alpha2.Schema{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: redpandav1alpha2.SchemaSpec{
			ClusterSource: environment.ClusterSourceValid,
			Text: `{
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
			}`,
		},
	}

	for name, tt := range map[string]struct {
		mutate            func(schema *redpandav1alpha2.Schema)
		expectedCondition metav1.Condition
	}{
		"success": {
			expectedCondition: environment.SyncedCondition,
		},
		"error - invalid cluster ref": {
			mutate: func(schema *redpandav1alpha2.Schema) {
				schema.Spec.ClusterSource = environment.ClusterSourceInvalidRef
			},
			expectedCondition: environment.InvalidClusterRefCondition,
		},
		"error - client error no SASL": {
			mutate: func(schema *redpandav1alpha2.Schema) {
				schema.Spec.ClusterSource = environment.ClusterSourceNoSASL
			},
			expectedCondition: environment.ClientErrorCondition,
		},
		"error - client error invalid credentials": {
			mutate: func(schema *redpandav1alpha2.Schema) {
				schema.Spec.ClusterSource = environment.ClusterSourceBadPassword
			},
			expectedCondition: environment.ClientErrorCondition,
		},
	} {
		t.Run(name, func(t *testing.T) {
			schema := baseSchema.DeepCopy()
			schema.Name = "schema" + strconv.Itoa(int(time.Now().UnixNano()))

			if tt.mutate != nil {
				tt.mutate(schema)
			}

			key := client.ObjectKeyFromObject(schema)
			req := ctrl.Request{NamespacedName: key}

			require.NoError(t, environment.Factory.Create(ctx, schema))
			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.NoError(t, environment.Factory.Get(ctx, key, schema))
			require.Equal(t, []string{FinalizerKey}, schema.Finalizers)
			require.Len(t, schema.Status.Conditions, 1)
			require.Equal(t, tt.expectedCondition.Type, schema.Status.Conditions[0].Type)
			require.Equal(t, tt.expectedCondition.Reason, schema.Status.Conditions[0].Reason)
			require.Equal(t, tt.expectedCondition.Status, schema.Status.Conditions[0].Status)

			if tt.expectedCondition.Status == metav1.ConditionTrue { //nolint:nestif // ignore
				schemaClient, err := environment.Factory.SchemaRegistryClient(ctx, schema)
				require.NoError(t, err)
				require.NotNil(t, schemaClient)

				_, err = schemaClient.SchemaByVersion(ctx, schema.Name, -1)
				require.NoError(t, err)

				// clean up and make sure we properly delete everything
				require.NoError(t, environment.Factory.Delete(ctx, schema))
				_, err = environment.Reconciler.Reconcile(ctx, req)
				require.NoError(t, err)
				require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, schema)))

				// make sure we no longer have a schema
				_, err = schemaClient.SchemaByVersion(ctx, schema.Name, -1)
				require.EqualError(t, err, fmt.Sprintf("Subject '%s' not found.", schema.Name))

				return
			}

			// clean up and make sure we properly delete everything
			require.NoError(t, environment.Factory.Delete(ctx, schema))
			_, err = environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(environment.Factory.Get(ctx, key, schema)))
		})
	}
}
