// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_certificates.go.tpl
package operator

import (
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func Certificate(dot *helmette.Dot) *certmanagerv1.Certificate {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Webhook.Enabled {
		return nil
	}

	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "redpanda-serving-cert",
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: certmanagerv1.CertificateSpec{
			DNSNames: []string{
				fmt.Sprintf("%s-webhook-service.%s.svc", RedpandaOperatorName(dot), dot.Release.Namespace),
				fmt.Sprintf("%s-webhook-service.%s.svc.%s", RedpandaOperatorName(dot), dot.Release.Namespace, values.ClusterDomain),
			},
			IssuerRef: cmmetav1.ObjectReference{
				Kind: "Issuer",
				Name: cleanForK8sWithSuffix(Fullname(dot), "selfsigned-issuer"),
			},
			SecretName: values.WebhookSecretName,
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				// There is an issue with gotohelm when RotationPolicyNever is used.
				// The conversion from constant string to helm template is failing.
				//
				// panic: interface conversion: types.Type is *types.Basic, not *types.Struct [recovered]
				RotationPolicy: "Never",
				// RotationPolicy: certv1.RotationPolicyNever,
			},
		},
	}
}

func Issuer(dot *helmette.Dot) *certmanagerv1.Issuer {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Webhook.Enabled {
		return nil
	}

	return &certmanagerv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cleanForK8sWithSuffix(Fullname(dot), "selfsigned-issuer"),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
}
