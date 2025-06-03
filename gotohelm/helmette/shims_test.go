package helmette

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestSortedMap(t *testing.T) {
	var keys []string
	for key := range SortedMap(map[string]bool{
		"0": true,
		"z": true,
		"a": true,
	}) {
		keys = append(keys, key)
	}

	require.Equal(t, []string{"0", "a", "z"}, keys)
}

func TestUnwrap(t *testing.T) {
	// A type with fields that shown to be difficult for Unwrap to handled
	type S struct {
		Quantity    *resource.Quantity
		IntOrString *intstr.IntOrString
		Time        *metav1.Time
		Duration    metav1.Duration
	}

	s := Unwrap[S](map[string]any{
		"Quantity":    "10Gi",
		"IntOrString": 10,
		"Time":        "2025-06-04T05:46:00Z",
		"Duration":    "10s",
	})

	require.Equal(t, s.IntOrString.IntVal, int32(10))
	require.Equal(t, s.Quantity.String(), "10Gi")
	require.Equal(t, s.Time.Time.UTC().String(), "2025-06-04 05:46:00 +0000 UTC")
	require.Equal(t, s.Duration.Duration.String(), "10s")
}
