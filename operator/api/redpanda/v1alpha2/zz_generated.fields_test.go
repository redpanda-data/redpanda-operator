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
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/pkg/properties"
)

var generatorSource *rand.Rand

func init() {
	generatorSource = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func RandomConfigurationProperties(t *testing.T) ConfigurationProperties {
	value, ok := quick.Value(reflect.TypeFor[ConfigurationProperties](), generatorSource)
	if !ok {
		t.Fatal("could not generate properties")
	}
	return value.Interface().(ConfigurationProperties)
}

func (c ConfigurationProperties) Generate(rand *rand.Rand, size int) reflect.Value {
	value := reflect.New(reflect.TypeOf(c)).Elem()
	initializePointers(rand, value, true)
	return value
}

func initializePointers(rand *rand.Rand, v reflect.Value, force bool) bool {
	if rand.Intn(10) > 6 && !force {
		// semi-random chance at generating a field
		return false
	}

	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)

			if field.Kind() != reflect.Pointer || !field.CanSet() {
				continue
			}

			value := reflect.New(field.Type().Elem())
			if initializePointers(rand, value.Elem(), false) {
				field.Set(reflect.New(field.Type().Elem()))
				field.Elem().Set(value.Elem())
			}
		}

	default:
		if value, ok := quick.Value(v.Type(), rand); ok {
			v.Set(value)
		}
	}

	return true
}

func TestGeneratedConfigurationProperties(t *testing.T) {
	for i := range 100 {
		t.Run(fmt.Sprintf("iteration %d", i+1), func(t *testing.T) {
			t.Parallel()

			configurationProperties := RandomConfigurationProperties(t)

			data := properties.MarshalProperties(configurationProperties)
			var roundTripped ConfigurationProperties
			require.NoError(t, properties.UnmarshalProperties(data, &roundTripped))

			if configurationProperties.DeprecatedWasmPerCoreMemoryReservation != nil {
				require.NotNil(t, roundTripped.DataTransformsPerCoreMemoryReservation)

				if configurationProperties.DataTransformsPerCoreMemoryReservation == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.DataTransformsPerCoreMemoryReservation, *configurationProperties.DeprecatedWasmPerCoreMemoryReservation)
				} else {
					require.Equal(t, *roundTripped.DataTransformsPerCoreMemoryReservation, *configurationProperties.DataTransformsPerCoreMemoryReservation)
				}
			}
			require.Nil(t, roundTripped.DeprecatedWasmPerCoreMemoryReservation)

			if configurationProperties.DeprecatedWasmPerFunctionMemoryLimit != nil {
				require.NotNil(t, roundTripped.DataTransformsPerFunctionMemoryLimit)

				if configurationProperties.DataTransformsPerFunctionMemoryLimit == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.DataTransformsPerFunctionMemoryLimit, *configurationProperties.DeprecatedWasmPerFunctionMemoryLimit)
				} else {
					require.Equal(t, *roundTripped.DataTransformsPerFunctionMemoryLimit, *configurationProperties.DataTransformsPerFunctionMemoryLimit)
				}
			}
			require.Nil(t, roundTripped.DeprecatedWasmPerFunctionMemoryLimit)

			if configurationProperties.DeprecatedIcebergRestCatalogAwsCredentialsSource != nil {
				require.NotNil(t, roundTripped.IcebergRestCatalogCredentialsSource)

				if configurationProperties.IcebergRestCatalogCredentialsSource == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.IcebergRestCatalogCredentialsSource, *configurationProperties.DeprecatedIcebergRestCatalogAwsCredentialsSource)
				} else {
					require.Equal(t, *roundTripped.IcebergRestCatalogCredentialsSource, *configurationProperties.IcebergRestCatalogCredentialsSource)
				}
			}
			require.Nil(t, roundTripped.DeprecatedIcebergRestCatalogAwsCredentialsSource)

			if configurationProperties.DeprecatedIcebergRestCatalogPrefix != nil {
				require.NotNil(t, roundTripped.IcebergRestCatalogWarehouse)

				if configurationProperties.IcebergRestCatalogWarehouse == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.IcebergRestCatalogWarehouse, *configurationProperties.DeprecatedIcebergRestCatalogPrefix)
				} else {
					require.Equal(t, *roundTripped.IcebergRestCatalogWarehouse, *configurationProperties.IcebergRestCatalogWarehouse)
				}
			}
			require.Nil(t, roundTripped.DeprecatedIcebergRestCatalogPrefix)

			if configurationProperties.DeprecatedDeleteRetentionMs != nil {
				require.NotNil(t, roundTripped.LogRetentionMs)

				if configurationProperties.LogRetentionMs == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.LogRetentionMs, *configurationProperties.DeprecatedDeleteRetentionMs)
				} else {
					require.Equal(t, *roundTripped.LogRetentionMs, *configurationProperties.LogRetentionMs)
				}
			}
			require.Nil(t, roundTripped.DeprecatedDeleteRetentionMs)

			if configurationProperties.DeprecatedSchemaRegistryNormalizeOnStartup != nil {
				require.NotNil(t, roundTripped.SchemaRegistryAlwaysNormalize)

				if configurationProperties.SchemaRegistryAlwaysNormalize == nil {
					// make sure we the values are equal with the roundTripped non-aliases
					require.Equal(t, *roundTripped.SchemaRegistryAlwaysNormalize, *configurationProperties.DeprecatedSchemaRegistryNormalizeOnStartup)
				} else {
					require.Equal(t, *roundTripped.SchemaRegistryAlwaysNormalize, *configurationProperties.SchemaRegistryAlwaysNormalize)
				}
			}
			require.Nil(t, roundTripped.DeprecatedSchemaRegistryNormalizeOnStartup)

			var secondRoundTripped ConfigurationProperties
			require.NoError(t, properties.UnmarshalProperties(data, &secondRoundTripped))
			require.True(t, roundTripped.Equals(&secondRoundTripped))
			secondRoundTripped.AdminApiRequireAuth = ptr.To(false)
			roundTripped.AdminApiRequireAuth = ptr.To(true)
			require.False(t, roundTripped.Equals(&secondRoundTripped))
		})
	}
}
