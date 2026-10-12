// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

// TestCRDs runs every test that needs a real apiserver to exercise CRD
// validation and defaulting. They share one control plane: starting an
// apiserver per test cost ~9s each against bodies that take milliseconds.
//
// Each subtest gets its own namespace. envtest has no namespace controller, so
// nothing reclaims them -- uniqueness is the isolation, not deletion.
func TestCRDs(t *testing.T) {
	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)

	t.Cleanup(func() { _ = testEnv.Stop() })

	require.NoError(t, AddToScheme(scheme.Scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	for name, fn := range map[string]func(*testing.T, client.Client, string){
		"role/defaults":         testRoleDefaults,
		"role/immutable":        testRoleImmutableFields,
		"role/validation":       testRoleValidation,
		"schema/defaults":       testSchemaDefaults,
		"schema/validation":     testSchemaValidation,
		"shadowlink/defaults":   testShadowLinkDefaults,
		"shadowlink/validation": testShadowLinkValidation,
		"topic/validation":      testTopicValidation,
		"user/defaults":         testUserDefaults,
		"user/immutable":        testUserImmutableFields,
		"user/validation":       testUserValidation,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ns := strings.ReplaceAll(name, "/", "-")
			require.NoError(t, c.Create(t.Context(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: ns},
			}))

			fn(t, c, ns)
		})
	}
}

func TestClusterRefGetNamespace(t *testing.T) {
	cases := []struct {
		name     string
		ref      ClusterRef
		fallback string
		expected string
	}{
		{
			name:     "unset falls back to referencing object namespace",
			ref:      ClusterRef{Name: "redpanda-source"},
			fallback: "b",
			expected: "b",
		},
		{
			name:     "explicit namespace takes precedence over fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("a")},
			fallback: "b",
			expected: "a",
		},
		{
			name:     "explicit namespace equal to fallback",
			ref:      ClusterRef{Name: "redpanda-source", Namespace: ptr.To("b")},
			fallback: "b",
			expected: "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.ref.GetNamespace(tc.fallback))
		})
	}
}
