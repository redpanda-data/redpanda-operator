// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package decommissioning

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodOrdinalForStatefulSet(t *testing.T) {
	cases := []struct {
		name    string
		pod     string
		sts     string
		ordinal int
		belongs bool
	}{
		{name: "default pool pod belongs to default sts", pod: "redpanda-2", sts: "redpanda", ordinal: 2, belongs: true},
		{name: "nodepool pod belongs to its pool sts", pod: "redpanda-poolb-0", sts: "redpanda-poolb", ordinal: 0, belongs: true},
		// The crux: a NodePool Pod must NOT be attributed to the default
		// StatefulSet just because the names share a prefix.
		{name: "nodepool pod does NOT belong to default sts", pod: "redpanda-poolb-0", sts: "redpanda", belongs: false},
		{name: "default pod does NOT belong to nodepool sts", pod: "redpanda-2", sts: "redpanda-poolb", belongs: false},
		{name: "non-numeric suffix is not a pod of the sts", pod: "redpanda-console", sts: "redpanda", belongs: false},
		{name: "exact name without ordinal does not belong", pod: "redpanda", sts: "redpanda", belongs: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ordinal, ok := podOrdinalForStatefulSet(tc.pod, tc.sts)
			require.Equal(t, tc.belongs, ok)
			if tc.belongs {
				require.Equal(t, tc.ordinal, ordinal)
			}
		})
	}
}
