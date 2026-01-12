// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package otelutil

import (
	"os"
	"slices"

	"github.com/redpanda-data/common-go/otelutil"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// TestingM abstracts *testing.M so it can be passed into TestMain for setup.
type TestingM interface {
	Run() int
}

// TestMain is a helper for configuring telemetry for the tests of a given
// package. Once configured, logs and traces can be directed to either a file
// or gRPC endpoint via the OTLP_DIR and OTLP_GRPC environment variables,
// respectively.
//
// Traces can be viewed with any OTLP compatible tooling.
// [otel-tui](https://github.com/ymtdzzz/otel-tui/tree/main) is an excellent
// choice for local environments.
//
// Usage:
//
//	import (
//		"testing"
//
//		"github.com/redpanda-data/common-go/otelutil"
//	)
//
//	func TestMain(m *testing.M) {
//		otelutil.TestMain(m, "integration-some-package-here")
//	}
func TestMain(m TestingM, name string, onTypes ...testutil.TestType) {
	if len(onTypes) == 0 {
		// default to setting up logging on integration and unit tests if unspecified
		onTypes = []testutil.TestType{testutil.TestTypeUnit, testutil.TestTypeIntegration}
	}

	shouldSetupLogger := slices.Contains(onTypes, testutil.Type())
	if !shouldSetupLogger {
		os.Exit(m.Run())
	}

	normalizedName := name
	if len(onTypes) != 1 {
		normalizedName = testutil.Type().String() + "-" + name
	}

	cleanup, err := otelutil.Setup(otelutil.WithBinaryName(normalizedName))
	if err != nil {
		panic(err)
	}

	code := m.Run()

	_ = cleanup()

	os.Exit(code)
}
