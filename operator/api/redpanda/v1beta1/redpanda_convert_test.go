package v1beta1_test

import (
	// "reflect"
	// "reflect"
	"bytes"
	"testing"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	redpandav1beta1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1beta1"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	// corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"
)

func TestConvert(t *testing.T) {
	cfg := rapidutil.KubernetesTypes
	// cfg := rapid.MakeConfig{
	// 	Types: map[reflect.Type]*rapid.Generator[any]{
	// 		reflect.TypeFor[any](): rapid.Just[any](nil), // Return nil for all untyped (any, interface{}) fields.
	// 	},
	// }

	rapid.Check(t, func(t *rapid.T) {
		original := rapid.MakeCustom[redpandav1beta1.Redpanda](cfg).Draw(t, "v1beta1")

		// We don't care about TypeMeta for this test case.
		original.TypeMeta = v1.TypeMeta{}

		// Special case so we don't have to further make our own type.
		if license := original.Spec.Enterprise.License; license != nil && license.ValueFrom != nil {
			license.ValueFrom.SecretKeyRef.Optional = nil
		}

		var v1alpha2 redpandav1alpha2.Redpanda
		assert.NoError(t, original.ConvertTo(&v1alpha2))

		var v1beta1 redpandav1beta1.Redpanda
		assert.NoError(t, v1beta1.ConvertFrom(&v1alpha2))

		if !assert.Equal(t, original, v1beta1) {
			originalYAML, err := yaml.Marshal(original)
			require.NoError(t, err)

			intermediateYAML, err := yaml.Marshal(v1alpha2)
			require.NoError(t, err)

			rtYAML, err := yaml.Marshal(v1beta1)
			require.NoError(t, err)

			t.Logf(`Roundtripping of %T through %T failed.
original:
	%s

intermediate:
	%s

roundtripped:
	%s
`, original, v1alpha2, indent(originalYAML), indent(intermediateYAML), indent(rtYAML))
		}
	})
}

func indent(s []byte) []byte {
	return bytes.Join(bytes.Split(s, []byte{'\n'}), []byte{'\n', '\t'})
}
