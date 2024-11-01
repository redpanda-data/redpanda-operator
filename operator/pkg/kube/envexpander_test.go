// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestEnvExpander(t *testing.T) {
	ctx := context.Background()

	var env envtest.Environment
	cfg, err := env.Start()
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{})
	require.NoError(t, err)

	require.NoError(t, c.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name:      "configs",
			Namespace: "default",
		},
		Data: map[string]string{
			"NODE_ENV": "PROD",
			"KEY":      "Value!",
		},
	}))

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      "secrets",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"ACCESS_KEY": []byte(`super!s3cret1`),
		},
	}))

	cases := []struct {
		In      string
		Out     string
		Err     error
		Env     []corev1.EnvVar
		EnvFrom []corev1.EnvFromSource
	}{
		{In: "${FOO}", Out: ""},
		{
			In:  "$BAR",
			Out: "Hello!",
			Env: []corev1.EnvVar{
				{
					Name:  "BAR",
					Value: "Hello!",
				},
			},
		},
		{
			In:  "$NODE_ENV uses ${ACCESS_KEY}",
			Out: "PROD uses super!s3cret1",
			Env: []corev1.EnvVar{
				{
					Name:  "BAR",
					Value: "Hello!",
				},
			},
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "configs"}}},
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "secrets"}}},
			},
		},
		{
			In:  "Precedence is LIFO: ${ACCESS_KEY}",
			Out: "Precedence is LIFO: Value!",
			Env: []corev1.EnvVar{},
			EnvFrom: []corev1.EnvFromSource{
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "secrets"}}},
				{Prefix: "ACCESS_", ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "configs"}}},
			},
		},
	}

	for _, tc := range cases {
		expander := EnvExpander{
			Client:    c,
			Namespace: "default",
			Env:       tc.Env,
			EnvFrom:   tc.EnvFrom,
		}

		expanded, err := expander.Expand(ctx, tc.In)
		assert.NoError(t, err)

		assert.Equal(t, tc.Out, expanded)
	}
}
