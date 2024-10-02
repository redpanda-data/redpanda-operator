// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package functional

func MapFn[T any, U any](fn func(T) U, a []T) []U {
	s := make([]U, len(a))
	for i := 0; i < len(a); i++ {
		s[i] = fn(a[i])
	}
	return s
}
