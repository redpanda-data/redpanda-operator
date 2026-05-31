// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestClusterRefGetNamespace(t *testing.T) {
	cases := []struct {
		name     string
		ref      ClusterRef
		fallback string
		expected string
	}{
		{
			name:     "unset falls back to referencing object namespace",
			ref:      ClusterRef{Name: "redpanda-source"},
			fallback: "b",
			expected: "b",
		},
		{
			name:     "explicit namespace takes precedence over fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("a")},
			fallback: "b",
			expected: "a",
		},
		{
			name:     "explicit namespace equal to fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("b")},
			fallback: "b",
			expected: "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.ref.GetNamespace(tc.fallback))
		})
	}
}
