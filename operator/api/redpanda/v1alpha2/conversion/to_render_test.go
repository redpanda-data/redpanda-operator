// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/fuzzing"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
)

func TestNodepoolConversion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		values := rapid.MakeCustom[redpanda.Values](rapidutil.KubernetesTypes).Draw(t, "values")
		pools := rapid.SliceOf(rapid.MakeCustom[redpandav1alpha2.NodePool](rapidutil.KubernetesTypes)).Draw(t, "pools")
		_, err := convertV2NodepoolsToPools(values, functional.MapFn(ptr.To, pools), &V2Defaulters{})
		require.NoError(t, err)
	})
}

func TestConvertV2Fields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		partialValues := rapid.MakeCustom[redpanda.PartialValues](fuzzing.ClusterSpecConfig()).Draw(t, "values")
		if partialValues.Storage != nil && partialValues.Storage.Tiered != nil && partialValues.Storage.Tiered.PersistentVolume != nil {
			partialValues.Storage.Tiered.PersistentVolume.Size = nil
		}
		values := redpanda.Values{}
		clusterSpec := &redpandav1alpha2.RedpandaClusterSpec{
			Affinity: &corev1.Affinity{},
		}
		marshaled, err := json.Marshal(partialValues)
		require.NoError(t, err)

		require.NoError(t, json.Unmarshal(marshaled, clusterSpec))
		require.NoError(t, json.Unmarshal(marshaled, &values))
		state := &redpanda.RenderState{
			Values: values,
		}
		err = convertV2Fields(state, &state.Values, clusterSpec)
		require.NoError(t, err)
	})
}
