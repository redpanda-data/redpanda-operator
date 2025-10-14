// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package functional_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

func TestMergeMaps(t *testing.T) {
	cases := []struct {
		First  map[string]any
		Second map[string]any
		Expect map[string]any
	}{
		{
			First:  map[string]any{},
			Second: map[string]any{},
			Expect: map[string]any{},
		},
		{
			First:  map[string]any{"a": 1},
			Second: map[string]any{"b": true},
			Expect: map[string]any{"a": 1, "b": true},
		},
		{
			First: map[string]any{
				"a": map[string]any{
					"b": true,
				},
			},
			Second: map[string]any{
				"a": map[string]any{
					"b": 2,
					"c": 1,
				},
			},
			Expect: map[string]any{
				"a": map[string]any{
					"b": 2,
					"c": 1,
				},
			},
		},
		{
			First: map[string]any{
				"statefulset": map[string]any{
					"replicas": 1,
					"sideCars": map[string]any{
						"image": map[string]any{
							"repository": "localhost/redpanda-operator",
							"tag":        "dev",
						},
					},
				},
			},
			Second: map[string]any{
				"statefulset": map[string]any{
					"replicas": 5,
				},
			},
			Expect: map[string]any{
				"statefulset": map[string]any{
					"replicas": 5,
					"sideCars": map[string]any{
						"image": map[string]any{
							"repository": "localhost/redpanda-operator",
							"tag":        "dev",
						},
					},
				},
			},
		},
		{
			First: map[string]any{
				"statefulset": map[string]any{
					"replicas": 5,
				},
			},
			Second: map[string]any{
				"statefulset": map[string]any{
					"replicas": 1,
					"sideCars": map[string]any{
						"image": map[string]any{
							"repository": "localhost/redpanda-operator",
							"tag":        "dev",
						},
					},
				},
			},
			Expect: map[string]any{
				"statefulset": map[string]any{
					"replicas": 1,
					"sideCars": map[string]any{
						"image": map[string]any{
							"repository": "localhost/redpanda-operator",
							"tag":        "dev",
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		actual := functional.MergeMaps(tc.First, tc.Second)
		require.Equal(t, tc.Expect, actual)
	}
}
