// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_secret.go.tpl
package console

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Secret(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Secret.Create {
		return nil
	}

	jwtSigningKey := values.Secret.Authentication.JWTSigningKey
	if jwtSigningKey == "" {
		jwtSigningKey = helmette.RandAlphaNum(32)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      Fullname(dot),
			Labels:    Labels(dot),
			Namespace: dot.Release.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			// Set empty defaults, so that we can always mount them as env variable even if they are not used.
			// For this reason we can't use `with` to change the scope.

			// Kafka
			"kafka-sasl-password":               ptr.Deref(values.Secret.Kafka.SASLPassword, ""),
			"kafka-sasl-aws-msk-iam-secret-key": ptr.Deref(values.Secret.Kafka.AWSMSKIAMSecretKey, ""),
			"kafka-tls-ca":                      ptr.Deref(values.Secret.Kafka.TLSCA, ""),
			"kafka-tls-cert":                    ptr.Deref(values.Secret.Kafka.TLSCert, ""),
			"kafka-tls-key":                     ptr.Deref(values.Secret.Kafka.TLSKey, ""),

			// schema registry
			"schema-registry-password":    ptr.Deref(values.Secret.SchemaRegistry.Password, ""),
			"schema-registry-bearertoken": ptr.Deref(values.Secret.SchemaRegistry.BearerToken, ""),
			"schemaregistry-tls-ca":       ptr.Deref(values.Secret.SchemaRegistry.TLSCA, ""),
			"schemaregistry-tls-cert":     ptr.Deref(values.Secret.SchemaRegistry.TLSCert, ""),
			"schemaregistry-tls-key":      ptr.Deref(values.Secret.SchemaRegistry.TLSKey, ""),

			// Authentication
			"authentication-jwt-signingkey":     jwtSigningKey,
			"authentication-oidc-client-secret": ptr.Deref(values.Secret.Authentication.OIDC.ClientSecret, ""),

			// License
			"license": values.Secret.License,

			// Redpanda
			"redpanda-admin-api-password": ptr.Deref(values.Secret.Redpanda.AdminAPI.Password, ""),
			"redpanda-admin-api-tls-ca":   ptr.Deref(values.Secret.Redpanda.AdminAPI.TLSCA, ""),
			"redpanda-admin-api-tls-cert": ptr.Deref(values.Secret.Redpanda.AdminAPI.TLSCert, ""),
			"redpanda-admin-api-tls-key":  ptr.Deref(values.Secret.Redpanda.AdminAPI.TLSKey, ""),

			// Serde
			"serde-protobuf-git-basicauth-password": ptr.Deref(values.Secret.Serde.ProtobufGitBasicAuthPassword, ""),
		},
	}
}
