// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Secret(state *RenderState) *corev1.Secret {
	if !state.Values.Secret.Create {
		return nil
	}

	jwtSigningKey := state.Values.Secret.Authentication.JWTSigningKey
	if jwtSigningKey == "" {
		jwtSigningKey = helmette.RandAlphaNum(32)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.FullName(),
			Labels:    state.Labels(nil),
			Namespace: state.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			// Set empty defaults, so that we can always mount them as env variable even if they are not used.
			// For this reason we can't use `with` to change the scope.

			// Kafka
			"kafka-sasl-password":               ptr.Deref(state.Values.Secret.Kafka.SASLPassword, ""),
			"kafka-sasl-aws-msk-iam-secret-key": ptr.Deref(state.Values.Secret.Kafka.AWSMSKIAMSecretKey, ""),
			"kafka-tls-ca":                      ptr.Deref(state.Values.Secret.Kafka.TLSCA, ""),
			"kafka-tls-cert":                    ptr.Deref(state.Values.Secret.Kafka.TLSCert, ""),
			"kafka-tls-key":                     ptr.Deref(state.Values.Secret.Kafka.TLSKey, ""),

			// schema registry
			"schema-registry-bearertoken": ptr.Deref(state.Values.Secret.SchemaRegistry.BearerToken, ""),
			"schema-registry-password":    ptr.Deref(state.Values.Secret.SchemaRegistry.Password, ""),
			"schemaregistry-tls-ca":       ptr.Deref(state.Values.Secret.SchemaRegistry.TLSCA, ""),
			"schemaregistry-tls-cert":     ptr.Deref(state.Values.Secret.SchemaRegistry.TLSCert, ""),
			"schemaregistry-tls-key":      ptr.Deref(state.Values.Secret.SchemaRegistry.TLSKey, ""),

			// Authentication
			"authentication-jwt-signingkey":     jwtSigningKey,
			"authentication-oidc-client-secret": ptr.Deref(state.Values.Secret.Authentication.OIDC.ClientSecret, ""),

			// License
			"license": state.Values.Secret.License,

			// Redpanda
			"redpanda-admin-api-password": ptr.Deref(state.Values.Secret.Redpanda.AdminAPI.Password, ""),
			"redpanda-admin-api-tls-ca":   ptr.Deref(state.Values.Secret.Redpanda.AdminAPI.TLSCA, ""),
			"redpanda-admin-api-tls-cert": ptr.Deref(state.Values.Secret.Redpanda.AdminAPI.TLSCert, ""),
			"redpanda-admin-api-tls-key":  ptr.Deref(state.Values.Secret.Redpanda.AdminAPI.TLSKey, ""),

			// Serde
			"serde-protobuf-git-basicauth-password": ptr.Deref(state.Values.Secret.Serde.ProtobufGitBasicAuthPassword, ""),
		},
	}
}
