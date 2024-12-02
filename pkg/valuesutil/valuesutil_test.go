// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package valuesutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmarshalInto(t *testing.T) {
	// NB: Can't use table tests here due to the use of generics.
	{
		out, err := UnmarshalInto[int](10)
		assert.NoError(t, err)
		assert.Equal(t, 10, out)
	}

	{
		out, err := UnmarshalInto[any](struct {
			Foo string
			Bar int
		}{Foo: "hello world", Bar: 12})
		assert.NoError(t, err)
		assert.Equal(t, map[string]any{
			"Foo": "hello world",
			"Bar": float64(12),
		}, out)
	}
}
