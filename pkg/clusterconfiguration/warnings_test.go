// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package clusterconfiguration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

func TestFormatWarnings(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, "", clusterconfiguration.FormatWarnings(nil))
		assert.Equal(t, "", clusterconfiguration.FormatWarnings([]error{}))
	})
	t.Run("nils_are_dropped", func(t *testing.T) {
		assert.Equal(t, "", clusterconfiguration.FormatWarnings([]error{nil, nil}))
	})
	t.Run("single", func(t *testing.T) {
		w := errors.New("warning: secret X not resolved")
		assert.Equal(t, "warning: secret X not resolved", clusterconfiguration.FormatWarnings([]error{w}))
	})
	t.Run("multiple_are_joined", func(t *testing.T) {
		a := errors.New("warning: secret A not resolved")
		b := errors.New("warning: secret B not resolved")
		got := clusterconfiguration.FormatWarnings([]error{a, b})
		assert.Equal(t, "warning: secret A not resolved; warning: secret B not resolved", got)
	})
}

// TestClusterCfg_Warnings_FromOptionalSecretFailure asserts that
// errorToWarning-wrapped failed expansions (the contract clusterCfg.compile
// emits for Optional external secret refs) are
// surfaced through the Warnings accessor so the v1 and v2 controllers
// can lift them into a status condition (and, on v2, Warning events)
// before failing the reconcile. Regression coverage for K8S-858.
func TestClusterCfg_Warnings_FromOptionalSecretFailure(t *testing.T) {
	cfg := clusterconfiguration.NewClusterCfg(clusterconfiguration.NewPodContext("namespace"))
	cfg.SetAdditionalConfiguration("iceberg_rest_catalog_client_secret", `""`)

	// Simulate the shape the cluster config emits for Optional external
	// secrets: a fixup that asks for a secret reference and silently
	// downgrades the resulting error to a Warning. Route through
	// `makeError` so the test doesn't need a real cloud expander — the
	// Warning-handling path in cel_patcher.go is the contract under
	// test.
	cfg.AddFixup(
		"iceberg_rest_catalog_client_secret",
		`errorToWarning(makeError("secret 'DATABRICKS_CLIENT_SECRET': access denied"))`,
	)

	_, err := cfg.Reify(context.TODO(), nil, nil, nil)
	require.NoError(t, err, "Reify itself must not fail for a Warning; the property is omitted from the result")

	warnings := cfg.Warnings()
	require.Len(t, warnings, 1, "expected one Warning for the simulated optional-secret failure")
	assert.Contains(t, warnings[0].Error(), "DATABRICKS_CLIENT_SECRET")
	// The warning must name the affected property, not just the underlying
	// secret failure, so the user can tell which config value is missing.
	assert.Contains(t, warnings[0].Error(), `could not resolve property "iceberg_rest_catalog_client_secret"`)

	// And the shared formatter should produce a user-facing summary that
	// the controller can drop straight into a status condition message.
	msg := clusterconfiguration.FormatWarnings(warnings)
	assert.Contains(t, msg, "DATABRICKS_CLIENT_SECRET")
}
