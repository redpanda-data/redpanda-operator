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
	"sort"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// bootstrappedCert holds the resolved cert name and duration for certs that
// need issuer/CA bootstrapping (i.e. not disabled, not user-provided, and
// without an external issuer).
type bootstrappedCert struct {
	name     string
	duration time.Duration
}

// bootstrappedCerts returns the set of in-use cert names that require
// self-signed issuer and root CA bootstrapping. A cert is excluded if:
//   - it's explicitly disabled
//   - it has a user-provided SecretRef (externally managed cert)
//   - it has an external IssuerRef (user manages their own cert-manager issuer)
func bootstrappedCerts(spec *redpandav1alpha2.StretchClusterSpec) []bootstrappedCert {
	tlsCfg := spec.TLS
	if tlsCfg == nil {
		return nil
	}

	inUseCerts := map[string]bool{}
	for _, name := range spec.InUseServerCerts() {
		inUseCerts[name] = true
	}
	for _, name := range spec.InUseClientCerts() {
		inUseCerts[name] = true
	}

	sortedNames := make([]string, 0, len(inUseCerts))
	for name := range inUseCerts {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	var result []bootstrappedCert
	for _, name := range sortedNames {
		cert := tlsCfg.Certs[name]

		if cert != nil {
			if !cert.IsEnabled() || cert.SecretRef != nil || cert.IssuerRef != nil {
				continue
			}
		}

		duration := defaultCertDuration
		if cert != nil && cert.Duration != nil {
			duration = cert.Duration.Duration
		}

		result = append(result, bootstrappedCert{name: name, duration: duration})
	}
	return result
}

// certIssuers returns all cert-manager Issuers for the given RenderState.
// Each bootstrapped cert gets two issuers:
//  1. A self-signed issuer — used only to sign the root CA certificate.
//  2. A CA issuer backed by the root CA — used to sign the actual server/client certs.
//
// This two-level chain means leaf certs are signed by a proper CA (not self-signed),
// which allows clients to verify the full chain using just the root CA's public key.
func certIssuers(state *RenderState) []*certmanagerv1.Issuer {
	fullname := state.fullname()
	var issuers []*certmanagerv1.Issuer

	for _, bc := range bootstrappedCerts(state.Spec()) {
		// Self-signed issuer.
		issuers = append(issuers, &certmanagerv1.Issuer{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Issuer",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-selfsigned-issuer", fullname, bc.name),
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					SelfSigned: &certmanagerv1.SelfSignedIssuer{},
				},
			},
		})

		// CA issuer backed by the root certificate.
		issuers = append(issuers, &certmanagerv1.Issuer{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Issuer",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-root-issuer", fullname, bc.name),
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: fmt.Sprintf("%s-%s-root-certificate", fullname, bc.name),
					},
				},
			},
		})
	}

	return issuers
}

// rootCAs returns all root CA Certificates for the given RenderState.
func rootCAs(state *RenderState) []*certmanagerv1.Certificate {
	fullname := state.fullname()
	var certs []*certmanagerv1.Certificate

	for _, bc := range bootstrappedCerts(state.Spec()) {
		rootName := fmt.Sprintf("%s-%s-root-certificate", fullname, bc.name)
		certs = append(certs, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rootName,
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Spec: certmanagerv1.CertificateSpec{
				Duration:   &metav1.Duration{Duration: bc.duration},
				IsCA:       true,
				CommonName: rootName,
				SecretName: rootName,
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
				IssuerRef: cmmetav1.ObjectReference{
					Name:  fmt.Sprintf("%s-%s-selfsigned-issuer", fullname, bc.name),
					Kind:  "Issuer",
					Group: "cert-manager.io",
				},
			},
		})
	}

	return certs
}
