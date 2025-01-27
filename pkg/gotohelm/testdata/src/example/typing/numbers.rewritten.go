//go:build rewrites
// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package typing

import "math"

type myfloat = float64
type myint int64

func numbers() []any {
	return []any{
		// NB: It's possible that this test will fail on machines who's `int`
		// size is 32 and not 64.
		math.MinInt64,
		math.MaxInt64,
		math.MinInt32,
		math.MaxInt32,
		math.MaxFloat32,
		math.MaxFloat64,
		-1 * math.MaxFloat64,
		len([]any{}) == 0,
		len([]any{""}) == 1,
		len([]any{"", ""}) != 1.0,
		1.1,
		-0.01,
		123.00,
		// math.MaxInt64 * math.MaxInt64, // Believe it or not, this actually causes an overflow error.
		getMyInt() == 10,
		getMyInt() == myint(getMyFloat()),
		myfloat(getMyInt()) == getMyFloat(),
		getMyFloat() == 1.1,
	}
}

func anInt() any {
	return int64(123)
}

func anFloat() any {
	return float64(123)
}

func getMyInt() myint {
	return 10
}

func getMyFloat() myfloat {
	return 1.1
}
