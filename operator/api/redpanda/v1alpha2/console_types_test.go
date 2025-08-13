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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
	"github.com/redpanda-data/redpanda-operator/pkg/valuesutil"
)

// TestConsoleConversion asserts that the ConsoleValues struct is a subset of the
// consolev3.PartialValues. Said another way, all ConsoleValues are valid
// PartialValues but not all PartialValues are valid ConsoleValues.
func TestConsoleConversion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		spec := rapid.MakeCustom[ConsoleValues](rapidutil.KubernetesTypes).Draw(t, "spec")

		rtd, err := valuesutil.RoundTripThrough[console.PartialRenderValues](spec)
		require.NoError(t, err)

		specJSON, err := json.Marshal(spec)
		require.NoError(t, err)

		rtdJSON, err := json.Marshal(rtd)
		require.NoError(t, err)

		require.JSONEq(t, string(specJSON), string(rtdJSON))
	})
}
