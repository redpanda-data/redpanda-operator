// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_certs.go.tpl
package redpanda

import (
	"fmt"
	"strings"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func ClientCerts(state *RenderState) []*certmanagerv1.Certificate {
	if !TLSEnabled(state) {
		return []*certmanagerv1.Certificate{}
	}

	fullname := Fullname(state)
	service := ServiceName(state)
	ns := state.Release.Namespace
	// Trailing .'s don't play nice with TLS/SNI: https://datatracker.ietf.org/doc/html/rfc6066#section-3
	// So we trim it when generating certificates.
	domain := strings.TrimSuffix(state.Values.ClusterDomain, ".")

	var certs []*certmanagerv1.Certificate
	for name, data := range helmette.SortedMap(state.Values.TLS.Certs) {
		if !helmette.Empty(data.SecretRef) || !ptr.Deref(data.Enabled, true) {
			continue
		}

		var names []string
		if data.IssuerRef == nil || ptr.Deref(data.ApplyInternalDNSNames, false) {
			names = append(names, fmt.Sprintf("%s-cluster.%s.%s.svc.%s", fullname, service, ns, domain))
			names = append(names, fmt.Sprintf("%s-cluster.%s.%s.svc", fullname, service, ns))
			names = append(names, fmt.Sprintf("%s-cluster.%s.%s", fullname, service, ns))
			names = append(names, fmt.Sprintf("*.%s-cluster.%s.%s.svc.%s", fullname, service, ns, domain))
			names = append(names, fmt.Sprintf("*.%s-cluster.%s.%s.svc", fullname, service, ns))
			names = append(names, fmt.Sprintf("*.%s-cluster.%s.%s", fullname, service, ns))
			names = append(names, fmt.Sprintf("%s.%s.svc.%s", service, ns, domain))
			names = append(names, fmt.Sprintf("%s.%s.svc", service, ns))
			names = append(names, fmt.Sprintf("%s.%s", service, ns))
			names = append(names, fmt.Sprintf("*.%s.%s.svc.%s", service, ns, domain))
			names = append(names, fmt.Sprintf("*.%s.%s.svc", service, ns))
			names = append(names, fmt.Sprintf("*.%s.%s", service, ns))
		}

		if state.Values.External.Domain != nil {
			names = append(names, helmette.Tpl(state.dot, *state.Values.External.Domain, state.dot))
			names = append(names, fmt.Sprintf("*.%s", helmette.Tpl(state.dot, *state.Values.External.Domain, state.dot)))
		}

		duration := helmette.Default("43800h", data.Duration)
		issuerRef := ptr.Deref(data.IssuerRef, cmmetav1.ObjectReference{
			Kind:  "Issuer",
			Group: "cert-manager.io",
			Name:  fmt.Sprintf("%s-%s-root-issuer", fullname, name),
		})

		certs = append(certs, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-cert", fullname, name),
				Labels:    FullLabels(state),
				Namespace: state.Release.Namespace,
			},
			Spec: certmanagerv1.CertificateSpec{
				DNSNames:   names,
				Duration:   helmette.MustDuration(duration),
				IsCA:       false,
				IssuerRef:  issuerRef,
				SecretName: fmt.Sprintf("%s-%s-cert", fullname, name),
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
			},
		})
	}

	name := state.Values.Listeners.Kafka.TLS.Cert

	data, ok := state.Values.TLS.Certs[name]
	if !ok {
		panic(fmt.Sprintf("Certificate %q referenced but not defined", name))
	}

	if !helmette.Empty(data.SecretRef) || !ClientAuthRequired(state) {
		return certs
	}

	issuerRef := cmmetav1.ObjectReference{
		Group: "cert-manager.io",
		Kind:  "Issuer",
		Name:  fmt.Sprintf("%s-%s-root-issuer", fullname, name),
	}

	if data.IssuerRef != nil {
		issuerRef = *data.IssuerRef
		issuerRef.Group = "cert-manager.io"
	}

	duration := helmette.Default("43800h", data.Duration)

	return append(certs, &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-client", fullname),
			Labels: FullLabels(state),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-client", fullname),
			Duration:   helmette.MustDuration(duration),
			IsCA:       false,
			SecretName: fmt.Sprintf("%s-client", fullname),
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: "ECDSA",
				Size:      256,
			},
			IssuerRef: issuerRef,
		},
	})
}
