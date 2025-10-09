// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaybeExpand(t *testing.T) {
	t.Parallel()

	expander := NewCloudExpanderFromAPI(&FakeSecretAPI{})
	tests := []struct {
		name           string
		value          string
		expectedError  bool
		expectedString string
	}{
		{
			name:           "secret exists",
			value:          "${secrets.test-secret}",
			expectedError:  false,
			expectedString: "fake-secret-value",
		},
		{
			name:           "not interpolated value",
			value:          "secret",
			expectedError:  false,
			expectedString: "secret",
		},
		{
			name:           "differently interpolated value",
			value:          "${test-secret}",
			expectedError:  false,
			expectedString: "${test-secret}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := expander.MaybeExpand(context.Background(), tt.value)
			if actual != tt.expectedString {
				t.Errorf("got %s, want %s", actual, tt.expectedString)
			}
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

type FakeSecretAPI struct{}

func (t *FakeSecretAPI) GetSecretValue(ctx context.Context, name string) (string, bool) {
	return "fake-secret-value", true
}

func (t *FakeSecretAPI) CheckSecretExists(ctx context.Context, name string) bool {
	return true
}
