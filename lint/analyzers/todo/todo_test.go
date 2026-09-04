// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package todo_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/redpanda-data/redpanda-operator/lint/analyzers/todo"
)

// TestTodo runs the analyzer over testdata/src/a and matches each report to a
// `// want "regexp"` comment on the same line. That is how every analyzer in
// this directory should be tested.
func TestTodo(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), todo.Analyzer, "a")
}
