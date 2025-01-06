// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:namespace=_directives
package directives

func Directives() bool {
	// Calling Noop does nothing but asserts that it's referenced correctly
	// with the correct namespacing.
	Noop()
	return true
}
