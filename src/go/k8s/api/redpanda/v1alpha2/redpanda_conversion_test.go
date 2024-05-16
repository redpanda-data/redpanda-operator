package v1alpha2_test

import (
	"encoding/json"
	"testing"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestCloudStorageEnabledBool(t *testing.T) {
	testCases := []struct {
		In  []byte
		Out *bool
	}{
		{In: []byte(`true`), Out: ptr.To(true)},
		{In: []byte(`false`), Out: ptr.To(false)},
		{In: []byte(`"true"`), Out: ptr.To(true)},
		{In: []byte(`"false"`), Out: ptr.To(false)},
		{In: []byte(`null`), Out: nil},
	}

	for _, tc := range testCases {
		var b *v1alpha2.CloudStorageEnabledBool

		require.NoError(t, json.Unmarshal(tc.In, &b))
		require.Equal(t, (*v1alpha2.CloudStorageEnabledBool)(tc.Out), b, "%q did not unmarshal to %v", tc.In, tc.Out)
	}
}
