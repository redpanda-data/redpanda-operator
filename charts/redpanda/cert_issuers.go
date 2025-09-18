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

func CertIssuers(state *RenderState) []*certmanagerv1.Issuer {
	issuers, _ := certIssuersAndCAs(state)
	return issuers
}

func RootCAs(state *RenderState) []*certmanagerv1.Certificate {
	_, cas := certIssuersAndCAs(state)
	return cas
}

func certIssuersAndCAs(state *RenderState) ([]*certmanagerv1.Issuer, []*certmanagerv1.Certificate) {
	var issuers []*certmanagerv1.Issuer
	var certs []*certmanagerv1.Certificate

	inUseCerts := map[string]bool{}
	for _, name := range state.Values.Listeners.InUseServerCerts(&state.Values.TLS) {
		inUseCerts[name] = true
	}
	for _, name := range state.Values.Listeners.InUseClientCerts(&state.Values.TLS) {
		inUseCerts[name] = true
	}

	for name := range helmette.SortedMap(inUseCerts) {
		data := state.Values.TLS.Certs.MustGet(name)

		// If this certificate is disabled (.Enabled), provided directly by the
		// end user (.SecretRef), or has an issuer provided (.IssuerRef), we
		// don't need to bootstrap an issuer.
		if !ptr.Deref(data.Enabled, true) || data.SecretRef != nil || data.IssuerRef != nil {
			continue
		}

		// Otherwise we need to bootstrap a CA Issuer as explained here:
		// https://cert-manager.io/docs/configuration/selfsigned/#bootstrapping-ca-issuers

		// First we create a self-signed (self-signing) Issuer. Unlike what the name would
		// indicate this Issuer does NOT have a key pair associated with it. It
		// simply signs Certificates with their own private key.
		// tl;dr: This Issuer is not self-signed but produces Certificates that
		// are themselves self-signed.
		// NB: Technically, we only need a single self signer. For backwards
		// compatibility, we generate one per cert.
		issuers = append(issuers,
			&certmanagerv1.Issuer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cert-manager.io/v1",
					Kind:       "Issuer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(`%s-%s-selfsigned-issuer`, Fullname(state), name),
					Namespace: state.Release.Namespace,
					Labels:    FullLabels(state),
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						// SelfSigningIssuer would be a MUCH better name.
						SelfSigned: &certmanagerv1.SelfSignedIssuer{},
					},
				},
			},
		)

		// This is the CA that will be signed with it's own private key by the
		// above issuer. It will then be used as the Root CA for the Issuer
		// that the chart actually uses.
		certs = append(certs,
			&certmanagerv1.Certificate{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cert-manager.io/v1",
					Kind:       "Certificate",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(`%s-%s-root-certificate`, Fullname(state), name),
					Namespace: state.Release.Namespace,
					Labels:    FullLabels(state),
				},
				Spec: certmanagerv1.CertificateSpec{
					Duration:   helmette.MustDuration(helmette.Default("43800h", data.Duration)),
					IsCA:       true,
					CommonName: fmt.Sprintf(`%s-%s-root-certificate`, Fullname(state), name),
					SecretName: fmt.Sprintf(`%s-%s-root-certificate`, Fullname(state), name),
					PrivateKey: &certmanagerv1.CertificatePrivateKey{
						Algorithm: "ECDSA",
						Size:      256,
					},
					IssuerRef: cmmetav1.ObjectReference{
						Name:  fmt.Sprintf(`%s-%s-selfsigned-issuer`, Fullname(state), name),
						Kind:  "Issuer",
						Group: "cert-manager.io",
					},
				},
			},
		)

		// This Issuer works like a normal CA Issuer using the above
		// Certificate as it's root. NB: The root CA is self signed and
		// therefore all Certificates from this Issuer will be self signed as
		// well. That is distinct from the Issuer being a
		// [certmanagerv1.SelfSignedIssuer].
		issuers = append(issuers,
			&certmanagerv1.Issuer{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "cert-manager.io/v1",
					Kind:       "Issuer",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf(`%s-%s-root-issuer`, Fullname(state), name),
					Namespace: state.Release.Namespace,
					Labels:    FullLabels(state),
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						CA: &certmanagerv1.CAIssuer{
							SecretName: data.RootSecretName(state, name),
						},
					},
				},
			},
		)

	}

	return issuers, certs
}
