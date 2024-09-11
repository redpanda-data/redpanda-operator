// Copyright 2024 Redpanda Data, Inc.
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

func stubGiven(ctx context.Context) error {
	T(ctx).ApplyFixture(ctx, "stub")

	return nil
}

func stubWhen(ctx context.Context, key, value string) error {
	t := T(ctx)

	var configMap corev1.ConfigMap
	require.NoError(t, t.Get(ctx, t.ResourceKey("stub-config-map"), &configMap))

	configMap.Data = map[string]string{key: value}

	require.NoError(t, t.Update(ctx, &configMap))

	return nil
}

func stubThen(ctx context.Context, key, value string) error {
	t := T(ctx)

	var configMap corev1.ConfigMap
	require.NoError(t, t.Get(ctx, t.ResourceKey("stub-config-map"), &configMap))

	require.Equal(t, value, configMap.Data[key])

	return nil
}

func init() {
	RegisterStep(`^there is a stub$`, stubGiven)
	RegisterStep(`^a user updates the stub key "([^"]*)" to "([^"]*)"$`, stubWhen)
	RegisterStep(`^the stub should have "([^"]*)" equal "([^"]*)"$`, stubThen)
}
