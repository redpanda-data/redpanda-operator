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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// certificates returns all cert-manager Certificates across every local pool.
// Wrapper used by the umbrella RenderResources.
func certificates(state *RenderState) ([]*certmanagerv1.Certificate, error) {
	var out []*certmanagerv1.Certificate
	for _, pool := range state.inClusterPools {
		c, err := certificatesForPool(state, pool)
		if err != nil {
			return nil, err
		}
		out = append(out, c...)
	}
	return out, nil
}

// certificatesForPool returns the cert-manager Certificates (server + client)
// for a single local pool. Each pool gets its own Certificates with content
// derived from pool.Spec.{TLS,ClusterDomain,External} and per-broker SANs
// drawn from the cross-region pool set so cross-cluster brokers can verify
// each other under the same CA. Issuer references stay cluster-wide
// (named with the cluster fullname); per-pool secrets are keyed by the
// pool fullname so two pools using cert name "default" don't collide.
func certificatesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ([]*certmanagerv1.Certificate, error) {
	poolSpec := &pool.Spec
	tlsCfg := poolSpec.TLS
	if tlsCfg == nil {
		return nil, nil
	}

	pki := poolPKI(state, pool)

	fullname := state.fullname()
	poolFullname := state.poolFullname(pool)
	// Headless Service is cluster-wide and always named after the cluster.
	service := fullname
	ns := state.namespace
	// Trailing dots don't play nice with TLS/SNI.
	domain := strings.TrimSuffix(poolSpec.GetClusterDomain(), ".")

	// external.domain is templated, so expand it once rather than per cert.
	externalDomain := ""
	if ext := poolSpec.External; ext != nil && ext.Domain != nil {
		expanded, err := tplutil.Tpl(*ext.Domain, state.tplData())
		if err != nil {
			return nil, fmt.Errorf("expanding external domain template: %w", err)
		}
		externalDomain = expanded
	}

	brokers := perPodHosts(state)
	isMCS := state.Spec().Networking.IsMCS()

	// NB: map values aren't addressable, so each certificate is copied out,
	// given its requests, and written back.
	issuing := map[string]redpanda.Certificate{}
	for name, cert := range pki.Certificates {
		data := tlsCfg.Certs[name]

		if cert.Client != nil && data != nil && data.SecretRef != nil && data.ClientSecretRef == nil {
			return nil, fmt.Errorf(".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q", name)
		}

		// A provided secretRef means the keypair already exists; only the
		// generated ones are issued.
		if data == nil || data.SecretRef == nil {
			cert.Server.Request = &redpanda.CertificateRequest{
				ObjectName: fmt.Sprintf("%s-%s-cert", poolFullname, name),
				DNSNames:   poolSANs(data, fullname, service, ns, domain, externalDomain, brokers, isMCS),
				Duration:   &metav1.Duration{Duration: certDuration(data)},
				IssuerRef:  certIssuerRef(fullname, name, data),
			}
		}

		if cert.Client != nil && (data == nil || data.ClientSecretRef == nil) {
			client := *cert.Client
			client.Request = &redpanda.CertificateRequest{
				ObjectName: fmt.Sprintf("%s-%s-client", poolFullname, name),
				CommonName: fmt.Sprintf("%s--%s-client", poolFullname, name),
				Duration:   &metav1.Duration{Duration: certDuration(data)},
				IssuerRef:  certIssuerRef(fullname, name, data),
			}
			cert.Client = &client
		}

		issuing[name] = cert
	}

	pki.Certificates = issuing

	return pki.Render(), nil
}

// poolSANs is the operator's SAN set. Per-pod Services are standalone in the
// namespace rather than subdomains of the headless Service, hence the
// namespace wildcards and per-broker names; see [redpanda.NamespaceSANs].
func poolSANs(
	data *redpandav1alpha2.Certificate,
	fullname, service, ns, domain, externalDomain string,
	brokers []string,
	isMCS bool,
) []string {
	var names []string

	if data == nil || data.IssuerRef == nil || data.ShouldApplyInternalDNSNames() {
		names = append(names, redpanda.ServiceSANs(fullname, service, ns, domain)...)
		names = append(names, redpanda.NamespaceSANs(ns, domain, brokers)...)
	}

	if isMCS {
		names = append(names, redpanda.ClusterSetSANs(fullname, service, ns, brokers)...)
	}

	names = append(names, redpanda.DomainSANs(externalDomain)...)

	return names
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

// perPodHosts returns every broker's per-pod Service name across all pools.
func perPodHosts(state *RenderState) []string {
	var hosts []string
	for _, p := range state.Pools() {
		for i := int32(0); i < p.GetReplicas(); i++ {
			hosts = append(hosts, PerPodServiceName(state.poolFullname(p), i))
		}
	}
	return hosts
}
