// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package users

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasswordGenerator(t *testing.T) {
	generator := newPasswordGenerator()
	password, err := generator.Generate()
	require.NoError(t, err)

	require.Len(t, password, 32)
}
