// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"
	"maps"
	"slices"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// certMountRoot is the root of every certificate mount. Go through
	// [Keypair]'s methods rather than formatting paths by hand.
	certMountRoot = "/etc/tls/certs"

	certVolumeDefaultMode = 0o440

	certPrivateKeyAlgorithm = certmanagerv1.ECDSAKeyAlgorithm
	certPrivateKeySize      = 256
)

// PKI is a cluster's resolved TLS material: what a broker mounts, and which of
// it cert-manager has to issue.
//
// NB: CertificateIssuers and CA bootstrapping are explicit excluded from PKI
// as they do not impact the cluster's shape.
type PKI struct {
	Namespace string
	Labels    map[string]string

	// Certificates is every certificate in use, keyed by certificate name.
	Certificates map[string]Certificate
}

// ServerKeypair is the named certificate's serving keypair.
func (p *PKI) ServerKeypair(name string) Keypair {
	return p.Certificates[name].Server
}

// ClientKeypair is the named certificate's client keypair.
func (p *PKI) ClientKeypair(name string) Keypair {
	// NB: a certificate no listener requires mTLS on carries no client
	// keypair, and callers past that guard only want paths, which need the
	// name alone.
	if cert, ok := p.Certificates[name]; ok && cert.Client != nil {
		return *cert.Client
	}
	return Keypair{Name: ClientKeypairName(name)}
}

// names returns the certificate names in a stable order.
func (p *PKI) names() []string {
	return slices.Sorted(maps.Keys(p.Certificates))
}

// Render returns a cert-manager Certificate per keypair carrying a
// [CertificateRequest], every serving keypair before any client one.
func (p *PKI) Render() []*certmanagerv1.Certificate {
	// NB: Performed in two separate loops to preserve historical ordering.
	var certs []*certmanagerv1.Certificate

	for _, name := range p.names() {
		cert := p.Certificates[name]
		if cert.Server.Request != nil {
			certs = append(certs, p.serverCertificate(&cert.Server))
		}
	}

	for _, name := range p.names() {
		cert := p.Certificates[name]
		if cert.Client != nil && cert.Client.Request != nil {
			certs = append(certs, p.clientCertificate(cert.Client))
		}
	}

	return certs
}

// Mounts returns every serving and client keypairs' mount.
func (p *PKI) Mounts() []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	for _, name := range p.names() {
		cert := p.Certificates[name]
		mounts = append(mounts, cert.Server.Mount())
	}

	for _, name := range p.names() {
		cert := p.Certificates[name]
		if cert.Client == nil {
			continue
		}
		mounts = append(mounts, cert.Client.Mount())
	}

	return mounts
}

// Volumes returns the Volumes pairing with [PKI.Mounts], same order.
func (p *PKI) Volumes() []corev1.Volume {
	var volumes []corev1.Volume

	for _, name := range p.names() {
		cert := p.Certificates[name]
		volumes = append(volumes, cert.Server.Volume())
	}

	for _, name := range p.names() {
		cert := p.Certificates[name]
		if cert.Client == nil {
			continue
		}
		volumes = append(volumes, cert.Client.Volume())
	}

	return volumes
}

// serverCertificate and clientCertificate are separate because a serving
// keypair is identified by its SANs and a client keypair by its common name.
// Setting both would emit the unused one as an explicit null: gotohelm writes
// a key for every field in a struct literal, where Go's omitempty drops it.
func (p *PKI) serverCertificate(k *Keypair) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.Request.ObjectName,
			Namespace: p.Namespace,
			Labels:    p.Labels,
		},
		Spec: certmanagerv1.CertificateSpec{
			DNSNames:   k.Request.DNSNames,
			Duration:   k.Request.Duration,
			IsCA:       false,
			IssuerRef:  k.Request.IssuerRef,
			SecretName: k.Secret.Name,
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: certPrivateKeyAlgorithm,
				Size:      certPrivateKeySize,
			},
		},
	}
}

func (p *PKI) clientCertificate(k *Keypair) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cert-manager.io/v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.Request.ObjectName,
			Namespace: p.Namespace,
			Labels:    p.Labels,
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: k.Request.CommonName,
			Duration:   k.Request.Duration,
			IsCA:       false,
			SecretName: k.Secret.Name,
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: certPrivateKeyAlgorithm,
				Size:      certPrivateKeySize,
			},

			IssuerRef: k.Request.IssuerRef,
		},
	}
}

// Certificate pairs the keypairs a broker mounts under one certificate name.
// It is [PKI]'s unit -- the pairing is what lets volumes, mounts, and issuance
// emit every serving keypair before any client one. Callers that only need
// paths build a [Keypair] directly.
type Certificate struct {
	// Client is nil when nothing using this certificate requires mTLS; its
	// presence decides whether a client volume and mount are emitted.
	Server Keypair
	Client *Keypair
}

// Keypair is one mounted TLS keypair.
//
// Paths derive from Name alone, so a caller that only needs cert_file,
// key_file, or truststore_file can build one with nothing else set. Secret and
// Request are read only by [PKI].
type Keypair struct {
	// Name is the keypair's mount name: the certificate's name for a serving
	// keypair, suffixed "-client" for a client one.
	Name string

	// Secret holds the keypair under tls.crt and tls.key. Resolved: the chart
	// prefixes generated names with the release fullname, the operator with
	// the per-pool fullname, and a user secretRef supersedes both.
	Secret corev1.LocalObjectReference

	// Request asks cert-manager to issue this keypair. Nil means it is user
	// supplied, via secretRef, and no Certificate object is emitted.
	Request *CertificateRequest

	// CA is the key within Secret holding the issuing CA bundle, or nil when
	// it ships none.
	//
	// A key rather than a bool because the callers disagree on both the
	// default and the key: chart caEnabled defaults false and falls through to
	// tls.crt, operator IsCAEnabled defaults true and uses ca.crt. A key
	// rather than a full reference because the CA is always a key of Secret --
	// the only Secret mounted at [Keypair.MountPath].
	CA *string
}

// MountPath is where the keypair mounts.
func (k *Keypair) MountPath() string {
	return fmt.Sprintf("%s/%s", certMountRoot, k.Name)
}

// VolumeName is the Volume projecting the keypair. Unprefixed by release or
// cluster so overrides can target it by a stable name.
func (k *Keypair) VolumeName() string {
	return fmt.Sprintf("redpanda-%s-cert", k.Name)
}

// CertFile and KeyFile are the keypair's paths.
func (k *Keypair) CertFile() string {
	return fmt.Sprintf("%s/%s", k.MountPath(), corev1.TLSCertKey)
}

func (k *Keypair) KeyFile() string {
	return fmt.Sprintf("%s/%s", k.MountPath(), corev1.TLSPrivateKeyKey)
}

// CAFile is the issuing CA's path, or "" when [Keypair.CA] is nil.
//
// Keyed on CA, not on cert-manager's convention of always writing a ca.crt:
// whether the file exists and whether Redpanda should trust it are different
// questions, and the callers answer the second differently.
func (k *Keypair) CAFile() string {
	if k.CA == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", k.MountPath(), *k.CA)
}

// CAOrCertFile is the path a peer verifies this server against absent an
// explicit truststore: the issuing CA when the keypair ships one, else the
// serving certificate itself.
func (k *Keypair) CAOrCertFile() string {
	// NB: absent a CA we can't assume a ca.crt was written, so the serving
	// certificate is the only thing guaranteed to be on disk.
	if k.CA != nil {
		return k.CAFile()
	}
	return k.CertFile()
}

// Volume and Mount project the keypair's Secret at [Keypair.MountPath].
func (k *Keypair) Volume() corev1.Volume {
	return corev1.Volume{
		Name: k.VolumeName(),
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  k.Secret.Name,
				DefaultMode: ptr.To[int32](certVolumeDefaultMode),
			},
		},
	}
}

func (k *Keypair) Mount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      k.VolumeName(),
		MountPath: k.MountPath(),
	}
}

// ClientKeypairName is the mount name of certName's client keypair. Exported
// so callers can build a paths-only [Keypair] without a [Certificate].
func ClientKeypairName(certName string) string {
	return fmt.Sprintf("%s-client", certName)
}

// CertificateRequest asks cert-manager to issue one [Keypair]. Its presence is
// what decides whether a Certificate object is emitted at all.
type CertificateRequest struct {
	// ObjectName is the Certificate's metadata.name. Resolved, not derived:
	// the chart keys it on the release fullname, the operator on the per-pool
	// fullname so two pools sharing a certificate name don't collide.
	ObjectName string `json:"objectName"`

	// DNSNames identifies a serving keypair. Build it with the SANs helpers
	// below.
	DNSNames []string `json:"dnsNames,omitempty"`

	// CommonName identifies a client keypair.
	CommonName string `json:"commonName,omitempty"`

	// Duration is the certificate's lifetime.
	//
	// NB: the json tag is load-bearing. gotohelm's zeroOf panics on any
	// json.Marshaler it hasn't special cased and metav1.Duration isn't one;
	// omitempty makes it skip the field when zeroing the struct. Dropping the
	// tag fails the transpile, not the render.
	Duration *metav1.Duration `json:"duration,omitempty"`

	// IssuerRef is fully resolved, Kind and Group included, and covers the
	// fallback to the bootstrapped per-certificate root issuer.
	IssuerRef cmmetav1.ObjectReference `json:"issuerRef"`
}

// ServiceSANs returns the names a broker is reachable under as a subdomain of
// its headless Service: "<fullname>-cluster.<service>.<ns>" and
// "<service>.<ns>" in FQDN, .svc, and bare forms, each plus a wildcard.
//
// clusterDomain must arrive with any trailing dot trimmed; trailing dots break
// TLS/SNI (RFC 6066 section 3).
func ServiceSANs(fullname, service, namespace, clusterDomain string) []string {
	bootstrap := fmt.Sprintf("%s-cluster.%s.%s", fullname, service, namespace)
	svc := fmt.Sprintf("%s.%s", service, namespace)

	return []string{
		fmt.Sprintf("%s.svc.%s", bootstrap, clusterDomain),
		fmt.Sprintf("%s.svc", bootstrap),
		bootstrap,
		fmt.Sprintf("*.%s.svc.%s", bootstrap, clusterDomain),
		fmt.Sprintf("*.%s.svc", bootstrap),
		fmt.Sprintf("*.%s", bootstrap),
		fmt.Sprintf("%s.svc.%s", svc, clusterDomain),
		fmt.Sprintf("%s.svc", svc),
		svc,
		fmt.Sprintf("*.%s.svc.%s", svc, clusterDomain),
		fmt.Sprintf("*.%s.svc", svc),
		fmt.Sprintf("*.%s", svc),
	}
}

// NamespaceSANs returns the namespace-wide wildcards followed by one
// "<broker>.<ns>" name per broker.
//
// Only callers whose brokers get standalone per-pod Services -- siblings of
// the headless Service rather than subdomains of it -- need these. Note the
// absence of "*.<ns>": a wildcard on a single-label parent (RFC 6125 section
// 6.4.3), which OpenSSL >= 3.0 rejects. The per-broker names cover the
// two-label form instead, matching the hostnames written into seed_servers and
// advertised_rpc_api; without them the RPC handshake fails strict hostname
// verification and the cluster can't reach quorum.
func NamespaceSANs(namespace, clusterDomain string, brokers []string) []string {
	names := []string{
		fmt.Sprintf("*.%s.svc.%s", namespace, clusterDomain),
		fmt.Sprintf("*.%s.svc", namespace),
	}

	for _, broker := range brokers {
		names = append(names, fmt.Sprintf("%s.%s", broker, namespace))
	}

	return names
}

// ClusterSetSANs returns the multi-cluster services clusterset.local names:
// cluster and service level, plus one per broker.
func ClusterSetSANs(fullname, service, namespace string, brokers []string) []string {
	names := []string{
		fmt.Sprintf("%s-cluster.%s.%s.svc.clusterset.local", fullname, service, namespace),
		fmt.Sprintf("*.%s-cluster.%s.%s.svc.clusterset.local", fullname, service, namespace),
		fmt.Sprintf("%s.%s.svc.clusterset.local", service, namespace),
		fmt.Sprintf("*.%s.%s.svc.clusterset.local", service, namespace),
	}

	for _, broker := range brokers {
		names = append(names, fmt.Sprintf("%s.%s.svc.clusterset.local", broker, namespace))
	}

	return names
}

// DomainSANs returns "<domain>" and "*.<domain>", or nothing for an empty
// domain. The domain must arrive already template-expanded.
func DomainSANs(domain string) []string {
	if domain == "" {
		return nil
	}
	return []string{domain, fmt.Sprintf("*.%s", domain)}
}
