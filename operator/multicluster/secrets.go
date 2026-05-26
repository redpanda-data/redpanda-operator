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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// secrets returns all Secrets for the given RenderState. Object order matches
// the pre-split legacy emission as closely as possible: cluster SASL/Users
// first, then per-pool (lifecycle + configurator + fs-validator), then
// bootstrap user.
func secrets(state *RenderState) ([]*corev1.Secret, error) {
	var out []*corev1.Secret
	saslUsers, err := secretSASLUsers(state)
	if err != nil {
		return nil, err
	}
	if saslUsers != nil {
		out = append(out, saslUsers)
	}
	for _, pool := range state.inClusterPools {
		out = append(out, poolSecrets(state, pool)...)
	}
	if bootstrapUser := secretBootstrapUser(state); bootstrapUser != nil {
		out = append(out, bootstrapUser)
	}
	return out, nil
}

// clusterSecrets returns the Secrets whose name and content are scoped to
// the StretchCluster as a whole (SASL user list, bootstrap user). Lifecycle
// scripts have moved to poolSecrets because their content (admin URL, TLS
// flags) is derived from the pool's spec.
func clusterSecrets(state *RenderState) ([]*corev1.Secret, error) {
	var out []*corev1.Secret
	saslUsers, err := secretSASLUsers(state)
	if err != nil {
		return nil, err
	}
	if saslUsers != nil {
		out = append(out, saslUsers)
	}
	if bootstrapUser := secretBootstrapUser(state); bootstrapUser != nil {
		out = append(out, bootstrapUser)
	}
	return out, nil
}

// poolSecrets returns the Secrets emitted for a single local BrokerPool
// (lifecycle scripts, configurator script, optional fs-validator). Caller
// iterates state.InClusterPools().
func poolSecrets(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []*corev1.Secret {
	out := []*corev1.Secret{
		secretSTSLifecycle(state, pool),
		secretConfigurator(state, pool),
	}
	if fsValidator := secretFSValidator(state, pool); fsValidator != nil {
		out = append(out, fsValidator)
	}
	return out
}

// secretSTSLifecycle returns the lifecycle scripts Secret for a pool's
// StatefulSet. Named <cluster>-<pool>-sts-lifecycle; content (postStart,
// preStop scripts) reads admin URL / TLS flags from the pool's spec.
func secretSTSLifecycle(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.Secret {
	p := scriptParamsForLifecycle(state, pool)

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-sts-lifecycle", state.poolFullname(pool)),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"common.sh":    lifecycleCommonSh(p),
			"postStart.sh": lifecyclePostStartSh(p),
			"preStop.sh":   lifecyclePreStopSh(p),
		},
	}
}

// secretSASLUsers returns the SASL users secret, if applicable.
func secretSASLUsers(state *RenderState) (*corev1.Secret, error) {
	if !state.Spec().Auth.IsSASLEnabled() {
		return nil, nil
	}

	sasl := state.Spec().Auth.SASL
	secretRef := ptr.Deref(sasl.SecretRef, "")

	if secretRef == "" {
		return nil, fmt.Errorf("auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true")
	}
	if len(sasl.Users) == 0 {
		return nil, nil
	}

	defaultMechanism := sasl.GetMechanism()

	var usersTxt []string
	for _, user := range sasl.Users {
		mechanism := ptr.Deref(user.Mechanism, defaultMechanism)
		usersTxt = append(usersTxt, fmt.Sprintf("%s:%s:%s",
			ptr.Deref(user.Name, ""),
			ptr.Deref(user.Password, ""),
			mechanism,
		))
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretRef,
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"users.txt": strings.Join(usersTxt, "\n"),
		},
	}, nil
}

// secretBootstrapUser returns the bootstrap user Secret. If an existing secret
// was found during state construction (fetchBootstrapUser), it's returned as-is
// to preserve the password across reconciliations. Otherwise a new secret with a
// random 32-char password is created. The secret is marked Immutable so that
// Kubernetes rejects any future mutations — password rotation requires deleting
// and re-creating the secret.
func secretBootstrapUser(state *RenderState) *corev1.Secret {
	if !state.Spec().Auth.IsSASLEnabled() {
		return nil
	}

	sasl := state.Spec().Auth.SASL
	if sasl.BootstrapUser != nil && sasl.BootstrapUser.SecretKeyRef != nil {
		return nil
	}

	// Re-emit the existing secret to preserve the password.
	if state.bootstrapUserSecret != nil {
		return state.bootstrapUserSecret
	}

	password := tplutil.RandAlphaNum(32)

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.cluster.BootstrapUserSecretName(),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Immutable: ptr.To(true),
		Type:      corev1.SecretTypeOpaque,
		StringData: map[string]string{
			redpandav1alpha2.StretchClusterBootstrapPasswordKey: password,
		},
	}
}

// secretFSValidator returns the filesystem validator Secret for a pool.
func secretFSValidator(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.Secret {
	if pool.Spec.InitContainers == nil || !pool.Spec.InitContainers.FSValidator.IsEnabled() {
		return nil
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%.49s-fs-validator", state.poolFullname(pool)),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"fsValidator.sh": fsValidatorSh,
		},
	}
}

// secretConfigurator returns the configurator script Secret for a pool.
func secretConfigurator(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *corev1.Secret {
	p := scriptParamsFromState(state, pool)

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%.51s-configurator", state.poolFullname(pool)),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"configurator.sh": configuratorSh(p),
		},
	}
}
