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
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// certificates returns all cert-manager Certificates (server + client) for the given RenderState.
func certificates(state *RenderState) ([]*certmanagerv1.Certificate, error) {
	fullname := state.fullname()
	service := state.Spec().GetServiceName(state.fullname())
	ns := state.namespace
	// Trailing dots don't play nice with TLS/SNI.
	domain := strings.TrimSuffix(state.Spec().GetClusterDomain(), ".")

	tlsCfg := state.Spec().TLS
	if tlsCfg == nil {
		return nil, nil
	}

	var certs []*certmanagerv1.Certificate

	// Server certificates.
	for _, name := range state.Spec().InUseServerCerts() {
		cert := tlsCfg.Certs[name]

		// Don't generate server certs if a secret is provided.
		if cert != nil && cert.SecretRef != nil {
			continue
		}

		var names []string
		if cert == nil || cert.IssuerRef == nil || cert.ShouldApplyInternalDNSNames() {
			names = append(names,
				fmt.Sprintf("%s-cluster.%s.%s.svc.%s", fullname, service, ns, domain),
				fmt.Sprintf("%s-cluster.%s.%s.svc", fullname, service, ns),
				fmt.Sprintf("%s-cluster.%s.%s", fullname, service, ns),
				fmt.Sprintf("*.%s-cluster.%s.%s.svc.%s", fullname, service, ns, domain),
				fmt.Sprintf("*.%s-cluster.%s.%s.svc", fullname, service, ns),
				fmt.Sprintf("*.%s-cluster.%s.%s", fullname, service, ns),
				fmt.Sprintf("%s.%s.svc.%s", service, ns, domain),
				fmt.Sprintf("%s.%s.svc", service, ns),
				fmt.Sprintf("%s.%s", service, ns),
				fmt.Sprintf("*.%s.%s.svc.%s", service, ns, domain),
				fmt.Sprintf("*.%s.%s.svc", service, ns),
				fmt.Sprintf("*.%s.%s", service, ns),
				// Per-pod service names are standalone services in the namespace,
				// not subdomains of the headless service. Namespace-wide
				// wildcards cover the FQDN and 3-label forms of those services.
				// The 2-label form `<pod>.<ns>` is covered by the explicit
				// per-broker SANs below — a `*.<ns>` wildcard would be on a
				// single-label parent (RFC 6125 §6.4.3) which OpenSSL ≥3.0
				// rejects with "hostname mismatch", so emitting one would
				// add noise without buying anything.
				fmt.Sprintf("*.%s.svc.%s", ns, domain),
				fmt.Sprintf("*.%s.svc", ns),
			)
			// In flat & MCS modes the operator writes 2-label hostnames
			// (`<pod>.<ns>`) into seed_servers / advertised_rpc_api, which
			// only a single-label-parent wildcard could match — and that's
			// the RFC violation noted above. Enumerate one well-formed SAN
			// per broker so the RPC handshake doesn't fail under strict
			// hostname verification and the cluster can actually reach
			// quorum (see #1499).
			for _, pool := range state.Pools() {
				for i := int32(0); i < pool.GetReplicas(); i++ {
					podName := PerPodServiceName(state.poolFullname(pool), i)
					names = append(names, fmt.Sprintf("%s.%s", podName, ns))
				}
			}
		}

		// In MCS mode, add clusterset.local SANs for cross-cluster DNS.
		if state.Spec().Networking.IsMCS() {
			names = append(names,
				fmt.Sprintf("%s-cluster.%s.%s.svc.clusterset.local", fullname, service, ns),
				fmt.Sprintf("*.%s-cluster.%s.%s.svc.clusterset.local", fullname, service, ns),
				fmt.Sprintf("%s.%s.svc.clusterset.local", service, ns),
				fmt.Sprintf("*.%s.%s.svc.clusterset.local", service, ns),
			)
			// Per-broker explicit SANs for the clusterset.local advertised
			// hostnames (see #1499). The wildcards above match the headless
			// service hierarchy, not the per-pod hostnames the operator
			// publishes via seed_servers / advertised_rpc_api in MCS mode
			// (`<pod>.<ns>.svc.clusterset.local`).
			for _, pool := range state.Pools() {
				for i := int32(0); i < pool.GetReplicas(); i++ {
					podName := PerPodServiceName(state.poolFullname(pool), i)
					names = append(names, fmt.Sprintf("%s.%s.svc.clusterset.local", podName, ns))
				}
			}
		}

		if ext := state.Spec().External; ext != nil && ext.Domain != nil {
			expandedDomain, err := tplutil.Tpl(*ext.Domain, state.tplData())
			if err != nil {
				return nil, fmt.Errorf("expanding external domain template: %w", err)
			}
			names = append(names, expandedDomain)
			names = append(names, fmt.Sprintf("*.%s", expandedDomain))
		}

		certs = append(certs, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-cert", fullname, name),
				Labels:    state.commonLabels(),
				Namespace: state.namespace,
			},
			Spec: certmanagerv1.CertificateSpec{
				DNSNames:   names,
				Duration:   &metav1.Duration{Duration: certDuration(cert)},
				IsCA:       false,
				IssuerRef:  certIssuerRef(fullname, name, cert),
				SecretName: state.Spec().TLS.CertServerSecretName(state.fullname(), name),
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
			},
		})
	}

	// Client certificates.
	for _, name := range state.Spec().InUseClientCerts() {
		cert := tlsCfg.Certs[name]

		if cert != nil {
			if cert.SecretRef != nil && cert.ClientSecretRef == nil {
				return nil, fmt.Errorf(".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q", name)
			}
			if cert.ClientSecretRef != nil {
				continue
			}
		}

		certs = append(certs, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-client", fullname, name),
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Spec: certmanagerv1.CertificateSpec{
				CommonName: fmt.Sprintf("%s--%s-client", fullname, name),
				Duration:   &metav1.Duration{Duration: certDuration(cert)},
				IsCA:       false,
				SecretName: state.Spec().TLS.CertClientSecretName(state.fullname(), name),
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
				IssuerRef: certIssuerRef(fullname, name, cert),
			},
		})
	}

	return certs, nil
}

// certDuration returns the certificate duration, falling back to defaultCertDuration.
func certDuration(cert *redpandav1alpha2.Certificate) time.Duration {
	if cert != nil && cert.Duration != nil {
		return cert.Duration.Duration
	}
	return defaultCertDuration
}

// certIssuerRef returns the issuer reference for a certificate. If the cert has
// an explicit IssuerRef, it is used; otherwise a default root-issuer is generated.
func certIssuerRef(fullname, certName string, cert *redpandav1alpha2.Certificate) cmmetav1.ObjectReference {
	if cert != nil && cert.IssuerRef != nil {
		return cmmetav1.ObjectReference{
			Name:  cert.IssuerRef.GetName(),
			Kind:  cert.IssuerRef.GetKind(),
			Group: cert.IssuerRef.GetGroup(),
		}
	}
	return cmmetav1.ObjectReference{
		Kind:  "Issuer",
		Group: "cert-manager.io",
		Name:  fmt.Sprintf("%s-%s-root-issuer", fullname, certName),
	}
}
