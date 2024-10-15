// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package functional

func CompareMaps[T, U comparable](a, b map[T]U) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if a[key] != b[key] {
			return false
		}
	}
	return true
}

func CompareMapsFn[T comparable, U any](a, b map[T]U, fn func(U, U) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !fn(a[key], b[key]) {
			return false
		}
	}
	return true
}

func CompareConvertibleSlices[T, U any](a []T, b []U, fn func(T, U) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if !fn(a[i], b[i]) {
			return false
		}
	}
	return true
}
