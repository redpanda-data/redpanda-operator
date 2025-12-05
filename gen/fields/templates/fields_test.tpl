// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ .Pkg }}

import (
  "fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"k8s.io/utils/ptr"
	"github.com/stretchr/testify/require"

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

      {{ range $property := .Properties -}}
      {{- range $i, $alias := $property.Aliases }}
      if configurationProperties.Deprecated{{ goName $alias }} != nil {
        require.NotNil(t, roundTripped.{{ $property.Name }})

        if configurationProperties.{{ $property.Name }} == nil {
          // make sure we the values are equal with the roundTripped non-aliases
          require.Equal(t, *roundTripped.{{ $property.Name }}, *configurationProperties.Deprecated{{ goName $alias }})
        } else {
          require.Equal(t, *roundTripped.{{ $property.Name }}, *configurationProperties.{{ $property.Name }})
        }
      }      
      require.Nil(t, roundTripped.Deprecated{{ goName $alias }})
      {{ end -}}
      {{- end }}

      var secondRoundTripped ConfigurationProperties
      require.NoError(t, properties.UnmarshalProperties(data, &secondRoundTripped))
      require.True(t, roundTripped.Equals(&secondRoundTripped))
      secondRoundTripped.AdminApiRequireAuth = ptr.To(false)
      roundTripped.AdminApiRequireAuth = ptr.To(true)
      require.False(t, roundTripped.Equals(&secondRoundTripped))
    })
  }
}
