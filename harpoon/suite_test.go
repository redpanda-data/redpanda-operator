// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"context"
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/require"
)

var suite *Suite

func TestMain(m *testing.M) {
	var err error

	suite, err = SuiteBuilderFromFlags().
		RegisterProvider("stub", NoopProvider).
		WithDefaultProvider("stub").
		ExitOnCleanupFailures().
		Build()
	if err != nil {
		fmt.Printf("error running test suite: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestSuite(t *testing.T) {
	suite.RunT(t)
}

func stubGiven(ctx context.Context, t TestingT) {
	t.ApplyFixture(ctx, "stub")
}

func stubWhen(ctx context.Context, t TestingT, key, value string) {
	var configMap corev1.ConfigMap
	require.NoError(t, t.Get(ctx, t.ResourceKey("stub-config-map"), &configMap))

	configMap.Data = map[string]string{key: value}

	require.NoError(t, t.Update(ctx, &configMap))
}

func stubThen(ctx context.Context, t TestingT, key, value string) {
	var configMap corev1.ConfigMap
	require.NoError(t, t.Get(ctx, t.ResourceKey("stub-config-map"), &configMap))

	require.Equal(t, value, configMap.Data[key])
}

func stubAnd(t TestingT) {
	require.Equal(t, 1, 1)
}

func init() {
	RegisterStep(`^there is a stub$`, stubGiven)
	RegisterStep(`^a user updates the stub key "([^"]*)" to "([^"]*)"$`, stubWhen)
	RegisterStep(`^the stub should have "([^"]*)" equal "([^"]*)"$`, stubThen)
	RegisterStep(`^there is no error$`, stubAnd)
}
