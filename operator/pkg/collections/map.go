// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package collections

// MapSet returns a slice of the values of the given set with a transformation
// function applied to them.
func MapSet[T comparable, U any](set Set[T], fn func(T) U) []U {
	var values []U
	for _, value := range set.Values() {
		values = append(values, fn(value))
	}
	return values
}
