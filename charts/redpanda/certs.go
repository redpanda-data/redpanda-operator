// Copyright 2026 Redpanda Data, Inc.
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
	fullname := Fullname(state)
	service := ServiceName(state)
	ns := state.Release.Namespace
	// Trailing .'s don't play nice with TLS/SNI: https://datatracker.ietf.org/doc/html/rfc6066#section-3
	// So we trim it when generating certificates.
	domain := strings.TrimSuffix(state.Values.ClusterDomain, ".")

	var certs []*certmanagerv1.Certificate
	for _, name := range state.Values.Listeners.InUseServerCerts(&state.Values.TLS) {
		data := state.Values.TLS.Certs.MustGet(name)

		// Don't generate server Certificates if a secret is provided.
		if !helmette.Empty(data.SecretRef) {
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
			names = append(names, helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot))
			names = append(names, fmt.Sprintf("*.%s", helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot)))
		}

		// Gateway API: a TLS-passthrough listener presents this managed cert
		// directly to clients, who connect using the listener's host/hostTemplate
		// SNI names. Those names are not covered by the internal service DNS or the
		// external.domain wildcard above, so add them here to avoid post-bootstrap
		// hostname-verification failures.
		names = append(names, gatewayServerCertDNSNames(state, name)...)

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
				SecretName: data.ServerSecretName(state, name),
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
			},
		})
	}

	for _, name := range state.Values.Listeners.InUseClientCerts(&state.Values.TLS) {
		data := state.Values.TLS.Certs.MustGet(name)

		if data.SecretRef != nil && data.ClientSecretRef == nil {
			panic(fmt.Sprintf(".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q", name))
		}

		// Don't generate a client Certificate if a client secret is provided.
		if data.ClientSecretRef != nil {
			continue
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

		certs = append(certs, &certmanagerv1.Certificate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Certificate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-client", fullname, name),
				Namespace: state.Release.Namespace,
				Labels:    FullLabels(state),
			},
			Spec: certmanagerv1.CertificateSpec{
				CommonName: fmt.Sprintf("%s--%s-client", fullname, name),
				Duration:   helmette.MustDuration(duration),
				IsCA:       false,
				SecretName: data.ClientSecretName(state, name),
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: "ECDSA",
					Size:      256,
				},
				IssuerRef: issuerRef,
			},
		})
	}

	return certs
}

// gatewayServerCertDNSNames returns the Gateway API SNI hostnames that the
// managed server certificate named certName must additionally cover. Every
// external listener that opts into Gateway TLSRoute mode with TLS enabled and
// resolves to certName contributes its bootstrap host plus one rendered
// hostTemplate name per broker. Under TLS passthrough the broker presents this
// cert directly, so omitting these SANs makes Kafka clients fail hostname
// verification once they reconnect to a per-broker SNI host.
func gatewayServerCertDNSNames(state *RenderState, certName string) []string {
	if !state.Values.External.IsGatewayEnabled() {
		return nil
	}

	pods := gatewayPodNames(state)
	var names []string

	for _, listener := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		names = appendGatewayCertHosts(state, names, certName, ptr.Deref(listener.Enabled, state.Values.External.Enabled), listener.IsGatewayListener(), listener.TLS, &state.Values.Listeners.Kafka.TLS, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), pods)
	}
	for _, listener := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		names = appendGatewayCertHosts(state, names, certName, ptr.Deref(listener.Enabled, state.Values.External.Enabled), listener.IsGatewayListener(), listener.TLS, &state.Values.Listeners.HTTP.TLS, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), pods)
	}
	for _, listener := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		names = appendGatewayCertHosts(state, names, certName, ptr.Deref(listener.Enabled, state.Values.External.Enabled), listener.IsGatewayListener(), listener.TLS, &state.Values.Listeners.Admin.TLS, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), pods)
	}
	for _, listener := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		names = appendGatewayCertHosts(state, names, certName, ptr.Deref(listener.Enabled, state.Values.External.Enabled), listener.IsGatewayListener(), listener.TLS, &state.Values.Listeners.SchemaRegistry.TLS, ptr.Deref(listener.Host, ""), ptr.Deref(listener.HostTemplate, ""), pods)
	}

	return names
}

// appendGatewayCertHosts appends a gateway listener's SNI hostnames to names
// when the listener is enabled, in gateway mode, TLS-enabled, and resolves to
// certName. extTLS.IsEnabled is nil-safe, and GetCertName is only reached when
// it returns true (so extTLS is non-nil there).
func appendGatewayCertHosts(state *RenderState, names []string, certName string, enabled bool, isGateway bool, extTLS *ExternalTLS, listenerTLS *InternalTLS, host string, hostTemplate string, pods []string) []string {
	if !enabled || !isGateway {
		return names
	}
	if !extTLS.IsEnabled(listenerTLS, &state.Values.TLS) {
		return names
	}
	if extTLS.GetCertName(listenerTLS) != certName {
		return names
	}

	if host != "" {
		names = append(names, host)
	}
	if hostTemplate != "" {
		for i, podname := range pods {
			names = append(names, renderBrokerHost(hostTemplate, i, podname))
		}
	}
	return names
}
