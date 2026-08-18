// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
)

func saslStretchCluster() *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "stretch", Namespace: "ns"},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Auth: &redpandav1alpha2.Auth{
				SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)},
			},
		},
	}
}

// K8S-900: the renderer must NOT generate its own bootstrap-user password when
// no secret exists yet. syncBootstrapUser() is the single writer that creates
// and synchronizes the immutable secret across all member clusters. A second,
// independent RandAlphaNum(32) generator here races that path — when the
// manifest is applied to all clusters at once, the two generators can persist
// divergent immutable passwords on different clusters, tripping the leader's
// consistency check and permanently halting reconciliation. The renderer must
// therefore treat a missing secret as "not ready yet" and emit nothing.
func TestSecretBootstrapUser_DoesNotSelfGenerate(t *testing.T) {
	sc := saslStretchCluster()
	state, err := NewRenderState(nil, sc, nil, nil, "test")
	require.NoError(t, err)
	require.Nil(t, state.bootstrapUserSecret, "no existing secret should be discovered with a nil client")

	require.Nil(t, secretBootstrapUser(state),
		"renderer must treat a missing bootstrap-user secret as not-ready, not generate its own")
}

// When the secret already exists it must be re-emitted verbatim so the password
// is preserved across reconciliations.
func TestSecretBootstrapUser_ReEmitsExisting(t *testing.T) {
	sc := saslStretchCluster()
	state, err := NewRenderState(nil, sc, nil, nil, "test")
	require.NoError(t, err)

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sc.BootstrapUserSecretName(),
			Namespace: sc.Namespace,
		},
		Immutable: ptr.To(true),
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			redpandav1alpha2.StretchClusterBootstrapPasswordKey: []byte("preserved-password"),
		},
	}
	state.bootstrapUserSecret = existing

	require.Same(t, existing, secretBootstrapUser(state),
		"an existing bootstrap-user secret must be re-emitted unchanged")
}

// When the user owns the secret via an explicit SecretKeyRef, the renderer
// emits nothing regardless of discovery state (the reconciler's
// syncBootstrapUser is the single writer of that Secret).
func TestSecretBootstrapUser_UserOwnedSecretKeyRef(t *testing.T) {
	sc := saslStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "user-owned"},
			Key:                  "password",
		},
	}
	state, err := NewRenderState(nil, sc, nil, nil, "test")
	require.NoError(t, err)

	require.Nil(t, secretBootstrapUser(state),
		"a user-provided SecretKeyRef must suppress operator-managed secret emission")
}

// The StatefulSet's RPK_PASS env var must source from the user-pinned
// bootstrapUser.secretKeyRef location so the pod authenticates with the same
// credential the reconciler created/synced (K8S-900).
func TestBootstrapUserEnvVars_HonorsSecretKeyRef(t *testing.T) {
	findRPKPass := func(vars []corev1.EnvVar) *corev1.EnvVar {
		for i := range vars {
			if vars[i].Name == "RPK_PASS" {
				return &vars[i]
			}
		}
		return nil
	}

	t.Run("default operator-managed location", func(t *testing.T) {
		sc := saslStretchCluster()
		state, err := NewRenderState(nil, sc, nil, nil, "test")
		require.NoError(t, err)

		rpkPass := findRPKPass(bootstrapUserEnvVars(state))
		require.NotNil(t, rpkPass)
		require.Equal(t, sc.BootstrapUserSecretName(), rpkPass.ValueFrom.SecretKeyRef.Name)
		require.Equal(t, redpandav1alpha2.StretchClusterBootstrapPasswordKey, rpkPass.ValueFrom.SecretKeyRef.Key)
	})

	t.Run("user-pinned secretKeyRef", func(t *testing.T) {
		sc := saslStretchCluster()
		sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
				Key:                  "custom-key",
			},
		}
		state, err := NewRenderState(nil, sc, nil, nil, "test")
		require.NoError(t, err)

		rpkPass := findRPKPass(bootstrapUserEnvVars(state))
		require.NotNil(t, rpkPass)
		require.Equal(t, "custom-bootstrap", rpkPass.ValueFrom.SecretKeyRef.Name)
		require.Equal(t, "custom-key", rpkPass.ValueFrom.SecretKeyRef.Key)
	})
}
