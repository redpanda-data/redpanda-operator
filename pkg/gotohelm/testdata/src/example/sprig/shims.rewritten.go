//go:build rewrites
// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package sprig

import "github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"

func numericTestInputs(dot *helmette.Dot) []any {
	return []any{
		"",
		int(0),
		float64(1),
		[]int{},
		map[string]any{},
		dot.Values["numeric"],
	}
}

func asNumeric(dot *helmette.Dot) any {
	// Inputs here are intentionally setup in a strange way. We need to test
	// going across function boundaries, having specifically typed inputs
	// within the same function, and doing the same for .Values.
	inputs := numericTestInputs(dot)
	inputs = append(inputs, int(10), 1.5, dot.Values["numeric"])

	outputs := []any{}
	for _, in := range inputs {
		value, isNumeric := helmette.AsNumeric(in)

		outputs = append(outputs, []any{in, value, isNumeric})
	}

	return outputs
}

func asIntegral(dot *helmette.Dot) any {
	// Inputs here are intentionally setup in a strange way. We need to test
	// going across function boundaries, having specifically typed inputs
	// within the same function, and doing the same for .Values.
	inputs := numericTestInputs(dot)
	inputs = append(inputs, int(10), 1.5, dot.Values["numeric"])

	outputs := []any{}
	for _, in := range inputs {
		value, isIntegral := helmette.AsIntegral[int](in)

		outputs = append(outputs, []any{in, value, isIntegral})
	}

	return outputs
}
