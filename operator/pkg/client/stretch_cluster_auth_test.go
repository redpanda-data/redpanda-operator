// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestStretchClusterAuth_HonorsSecretKeyRef ensures the operator's own
// admin/Kafka/schema-registry client factory reads the bootstrap password from
// the same location the reconciler writes to, including a user-pinned
// bootstrapUser.secretKeyRef. Without this the operator authenticates against a
// non-existent operator-managed secret when secretKeyRef is set (K8S-900).
func TestStretchClusterAuth_HonorsSecretKeyRef(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	newSC := func(bu *redpandav1alpha2.BootstrapUser) *redpandav1alpha2.StretchCluster {
		return &redpandav1alpha2.StretchCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "stretch", Namespace: "default"},
			Spec: redpandav1alpha2.StretchClusterSpec{
				Auth: &redpandav1alpha2.Auth{
					SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true), BootstrapUser: bu},
				},
			},
		}
	}

	secret := func(name, key, value string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Data:       map[string][]byte{key: []byte(value)},
			Type:       corev1.SecretTypeOpaque,
		}
	}

	t.Run("default operator-managed location", func(t *testing.T) {
		sc := newSC(nil)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(secret(sc.BootstrapUserSecretName(), redpandav1alpha2.StretchClusterBootstrapPasswordKey, "op-managed-pw")).
			Build()

		user, pw, err := (&Factory{}).stretchClusterAuth(context.Background(), sc, fakeClient)
		require.NoError(t, err)
		require.Equal(t, redpandav1alpha2.StretchClusterBootstrapUsername, user)
		require.Equal(t, "op-managed-pw", pw)
	})

	t.Run("user-pinned secretKeyRef", func(t *testing.T) {
		sc := newSC(&redpandav1alpha2.BootstrapUser{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
				Key:                  "custom-key",
			},
		})
		// Only the referenced secret exists — the operator-managed name is absent,
		// as the reconciler would leave it when secretKeyRef is set.
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(secret("custom-bootstrap", "custom-key", "user-pw")).
			Build()

		user, pw, err := (&Factory{}).stretchClusterAuth(context.Background(), sc, fakeClient)
		require.NoError(t, err)
		require.Equal(t, redpandav1alpha2.StretchClusterBootstrapUsername, user)
		require.Equal(t, "user-pw", pw)
	})

	t.Run("SASL disabled returns empty", func(t *testing.T) {
		sc := &redpandav1alpha2.StretchCluster{ObjectMeta: metav1.ObjectMeta{Name: "stretch", Namespace: "default"}}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		user, pw, err := (&Factory{}).stretchClusterAuth(context.Background(), sc, fakeClient)
		require.NoError(t, err)
		require.Empty(t, user)
		require.Empty(t, pw)
	})
}
