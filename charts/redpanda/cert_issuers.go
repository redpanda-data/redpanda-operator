// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_cert-issuers.go.tpl
package redpanda

import (
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func CertIssuers(dot *helmette.Dot) []*certmanagerv1.Issuer {
	issuers, _ := certIssuersAndCAs(dot)
	return issuers
}

func RootCAs(dot *helmette.Dot) []*certmanagerv1.Certificate {
	_, cas := certIssuersAndCAs(dot)
	return cas
}

func certIssuersAndCAs(dot *helmette.Dot) ([]*certmanagerv1.Issuer, []*certmanagerv1.Certificate) {
	values := helmette.Unwrap[Values](dot.Values)

	var issuers []*certmanagerv1.Issuer
	var certs []*certmanagerv1.Certificate

	if !TLSEnabled(dot) {
		return issuers, certs
	}

	for name, data := range helmette.SortedMap(values.TLS.Certs) {
		// If secretRef is defined, do not create any of these certificates or when
		// TLS reference is not enabled.
		if !helmette.Empty(data.SecretRef) || !ptr.Deref(data.Enabled, true) {
			continue
		}

		// If issuerRef is defined, use the specified issuer for the certs
		// If it's not defined, create and use our own issuer.
		if data.IssuerRef == nil {
			// The self-signed issuer is used to create the self-signed CA
			issuers = append(issuers,
				&certmanagerv1.Issuer{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "cert-manager.io/v1",
						Kind:       "Issuer",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf(`%s-%s-selfsigned-issuer`, Fullname(dot), name),
						Namespace: dot.Release.Namespace,
						Labels:    FullLabels(dot),
					},
					Spec: certmanagerv1.IssuerSpec{
						IssuerConfig: certmanagerv1.IssuerConfig{
							SelfSigned: &certmanagerv1.SelfSignedIssuer{},
						},
					},
				},
			)
		}

		// This is the self-signed CA used to issue certs
		issuers = append(issuers,
			&certmanagerv1.Issuer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cert-manager.io/v1",
					Kind:       "Issuer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(`%s-%s-root-issuer`, Fullname(dot), name),
					Namespace: dot.Release.Namespace,
					Labels:    FullLabels(dot),
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						CA: &certmanagerv1.CAIssuer{
							SecretName: fmt.Sprintf(`%s-%s-root-certificate`, Fullname(dot), name),
						},
					},
				},
			},
		)

		// This is the root CA certificate
		certs = append(certs,
			&certmanagerv1.Certificate{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cert-manager.io/v1",
					Kind:       "Certificate",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(`%s-%s-root-certificate`, Fullname(dot), name),
					Namespace: dot.Release.Namespace,
					Labels:    FullLabels(dot),
				},
				Spec: certmanagerv1.CertificateSpec{
					Duration:   helmette.MustDuration(helmette.Default("43800h", data.Duration)),
					IsCA:       true,
					CommonName: fmt.Sprintf(`%s-%s-root-certificate`, Fullname(dot), name),
					SecretName: fmt.Sprintf(`%s-%s-root-certificate`, Fullname(dot), name),
					PrivateKey: &certmanagerv1.CertificatePrivateKey{
						Algorithm: "ECDSA",
						Size:      256,
					},
					IssuerRef: cmmetav1.ObjectReference{
						Name:  fmt.Sprintf(`%s-%s-selfsigned-issuer`, Fullname(dot), name),
						Kind:  "Issuer",
						Group: "cert-manager.io",
					},
				},
			},
		)
	}

	return issuers, certs
}
