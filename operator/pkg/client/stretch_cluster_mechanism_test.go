// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// TestScramMechanismForStretch guards against K8S-899: the operator's stretch
// Kafka client must authenticate with the SASL mechanism configured on the
// StretchCluster (matching the mechanism Redpanda used to create the bootstrap
// user), not a hardcoded one. The default mechanism is SCRAM-SHA-512.
func TestScramMechanismForStretch(t *testing.T) {
	auth := scram.Auth{User: "kubernetes-controller", Pass: "secret"}

	testCases := []struct {
		name      string
		mechanism string
		wantName  string
		wantErr   bool
	}{
		{
			name:      "default mechanism is SCRAM-SHA-512",
			mechanism: "SCRAM-SHA-512",
			wantName:  "SCRAM-SHA-512",
		},
		{
			name:      "SCRAM-SHA-256 is honored",
			mechanism: "SCRAM-SHA-256",
			wantName:  "SCRAM-SHA-256",
		},
		{
			name:      "mechanism is case-insensitive",
			mechanism: "scram-sha-512",
			wantName:  "SCRAM-SHA-512",
		},
		{
			name:      "unsupported mechanism is rejected",
			mechanism: "PLAIN",
			wantErr:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mechanism, err := scramMechanismForStretch(auth, tc.mechanism)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantName, mechanism.Name())
		})
	}
}
