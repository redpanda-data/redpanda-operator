// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_certs.go.tpl
package chart

import (
	"fmt"
	"strings"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// defaultCertDuration is a managed certificate's lifetime absent a values
// override: five years.
const defaultCertDuration = "43800h"

// PKI resolves this chart's values into an authoritative certificate set. When
// possible, use the PKI rather than going through Values.
func resolvePKI(state *RenderState) redpanda.PKI {
	fullname := Fullname(state)
	service := ServiceName(state)
	ns := state.Release.Namespace
	// Trailing .'s don't play nice with TLS/SNI: https://datatracker.ietf.org/doc/html/rfc6066#section-3
	// So we trim it when generating certificates.
	domain := strings.TrimSuffix(state.Values.ClusterDomain, ".")

	// external.domain is templated, so expand it once rather than per cert.
	externalDomain := ""
	if state.Values.External.Domain != nil {
		externalDomain = helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot)
	}

	certs := map[string]redpanda.Certificate{}

	for _, name := range state.Values.Listeners.InUseServerCerts(&state.Values.TLS) {
		data := state.Values.TLS.Certs.MustGet(name)

		// NB: caEnabled declares that the serving Secret carries a trustworthy
		// ca.crt. Absent it the chart falls back to the serving certificate
		// itself, so there's no key.
		var ca *string
		if data.CAEnabled {
			ca = ptr.To("ca.crt")
		}

		// A provided secretRef means the keypair already exists; only the
		// generated ones are issued.
		var request *redpanda.CertificateRequest
		if data.SecretRef == nil {
			request = &redpanda.CertificateRequest{
				ObjectName: fmt.Sprintf("%s-%s-cert", fullname, name),
				DNSNames:   serverSANs(state, name, fullname, service, ns, domain, externalDomain),
				Duration:   helmette.MustDuration(helmette.Default(defaultCertDuration, data.Duration)),
				IssuerRef:  certIssuerRef(fullname, name, data),
			}
		}

		certs[name] = redpanda.Certificate{
			Server: redpanda.Keypair{
				Name:    name,
				CA:      ca,
				Secret:  corev1.LocalObjectReference{Name: data.ServerSecretName(state, name)},
				Request: request,
			},
		}
	}

	for _, name := range state.Values.Listeners.InUseClientCerts(&state.Values.TLS) {
		data := state.Values.TLS.Certs.MustGet(name)

		if data.SecretRef != nil && data.ClientSecretRef == nil {
			panic(fmt.Sprintf(".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q", name))
		}

		var request *redpanda.CertificateRequest
		if data.ClientSecretRef == nil {
			request = &redpanda.CertificateRequest{
				ObjectName: fmt.Sprintf("%s-%s-client", fullname, name),
				CommonName: fmt.Sprintf("%s--%s-client", fullname, name),
				Duration:   helmette.MustDuration(helmette.Default(defaultCertDuration, data.Duration)),
				IssuerRef:  certIssuerRef(fullname, name, data),
			}
		}

		// NB: the client certs are a subset of the server certs, so the entry
		// always exists. Map values aren't addressable, hence the write back.
		cert := certs[name]
		cert.Client = &redpanda.Keypair{
			Name:    fmt.Sprintf("%s-client", name),
			Secret:  corev1.LocalObjectReference{Name: data.ClientSecretName(state, name)},
			Request: request,
		}
		certs[name] = cert
	}

	return redpanda.PKI{
		Namespace:    ns,
		Labels:       FullLabels(state),
		Annotations:  FullAnnotations(state),
		Certificates: certs,
	}
}

// serverSANs is the chart's SAN set. Brokers here are subdomains of the
// headless Service, so it needs neither the namespace wildcards nor the
// per-broker names the operator adds.
func serverSANs(state *RenderState, name, fullname, service, ns, domain, externalDomain string) []string {
	var names []string

	data := state.Values.TLS.Certs.MustGet(name)
	if data.IssuerRef == nil || ptr.Deref(data.ApplyInternalDNSNames, false) {
		names = append(names, redpanda.ServiceSANs(fullname, service, ns, domain)...)
	}

	names = append(names, redpanda.DomainSANs(externalDomain)...)

	// A TLS-passthrough Gateway listener presents this cert directly under
	// its host/hostTemplate SNI names, which neither the internal service DNS
	// nor the external.domain wildcard covers.
	names = append(names, gatewayServerCertDNSNames(state, name)...)

	return names
}

// certIssuerRef falls back to the per-certificate root issuer this chart
// bootstraps in cert_issuers.go.
func certIssuerRef(fullname, name string, data *TLSCert) cmmetav1.ObjectReference {
	return ptr.Deref(data.IssuerRef, cmmetav1.ObjectReference{
		Kind:  "Issuer",
		Group: "cert-manager.io",
		Name:  fmt.Sprintf("%s-%s-root-issuer", fullname, name),
	})
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

	var names []string

	// NB: reads values, not [resolveListeners]. This feeds a certificate
	// request and a resolved listener carries its keypair, so going through the
	// IR would need the PKI this is helping to build. The predicates below
	// mirror resolveAPIListeners and have to stay in step with it.
	for _, entry := range gatewayListenerConfigs(state) {
		listener := entry.Listeners

		for _, external := range helmette.SortedMap(listener.External) {
			if !external.IsEnabled() || !ptr.Deref(external.Enabled, state.Values.External.Enabled) {
				continue
			}

			if !external.TLS.IsEnabled(&listener.TLS, &state.Values.TLS) || external.TLS.GetCertName(&listener.TLS) != certName {
				continue
			}

			gateway := resolveGateway(state, external)
			if gateway == nil {
				continue
			}

			if gateway.Host != "" {
				names = append(names, gateway.Host)
			}
			names = append(names, gateway.BrokerHosts...)
		}
	}

	return names
}

// gatewayAPI pairs an API's kind with its values, for the paths that read
// values rather than [resolveListeners].
type gatewayAPI struct {
	Kind      redpanda.APIKind
	Listeners ListenerConfig[string]
}

// gatewayListenerConfigs is the values' four client facing APIs in
// [redpanda.Listeners.Gateways] order, which a certificate's dnsNames pin.
func gatewayListenerConfigs(state *RenderState) []gatewayAPI {
	return []gatewayAPI{
		{Kind: redpanda.KafkaAPI, Listeners: state.Values.Listeners.Kafka.AsString()},
		{Kind: redpanda.HTTPAPI, Listeners: state.Values.Listeners.HTTP.AsString()},
		{Kind: redpanda.AdminAPI, Listeners: state.Values.Listeners.Admin.AsString()},
		{Kind: redpanda.SchemaRegistryAPI, Listeners: state.Values.Listeners.SchemaRegistry.AsString()},
	}
}
