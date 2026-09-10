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
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestSANHelpers pins each block's contents and shape. Callers concatenate
// them, so a change here silently rotates every broker certificate.
func TestSANHelpers(t *testing.T) {
	require.Equal(t, []string{
		"rp-cluster.rp.ns.svc.cluster.local",
		"rp-cluster.rp.ns.svc",
		"rp-cluster.rp.ns",
		"*.rp-cluster.rp.ns.svc.cluster.local",
		"*.rp-cluster.rp.ns.svc",
		"*.rp-cluster.rp.ns",
		"rp.ns.svc.cluster.local",
		"rp.ns.svc",
		"rp.ns",
		"*.rp.ns.svc.cluster.local",
		"*.rp.ns.svc",
		"*.rp.ns",
	}, ServiceSANs("rp", "rp", "ns", "cluster.local"))

	// Wildcards first, then one two-label name per broker. No "*.ns": a
	// wildcard on a single-label parent, which OpenSSL >= 3.0 rejects.
	nsSANs := NamespaceSANs("ns", "cluster.local", []string{"rp-0", "rp-1"})
	require.Equal(t, []string{
		"*.ns.svc.cluster.local",
		"*.ns.svc",
		"rp-0.ns",
		"rp-1.ns",
	}, nsSANs)
	require.NotContains(t, nsSANs, "*.ns")

	require.Equal(t, []string{
		"rp-cluster.rp.ns.svc.clusterset.local",
		"*.rp-cluster.rp.ns.svc.clusterset.local",
		"rp.ns.svc.clusterset.local",
		"*.rp.ns.svc.clusterset.local",
		"rp-0.ns.svc.clusterset.local",
	}, ClusterSetSANs("rp", "rp", "ns", []string{"rp-0"}))

	require.Equal(t, []string{"rp.example.com", "*.rp.example.com"}, DomainSANs("rp.example.com"))
	require.Nil(t, DomainSANs(""))
}

// TestPKIRender asserts a Certificate object is emitted only where a keypair
// carries a request, every serving one before any client one, each targeting
// the right Secret.
func TestPKIRender(t *testing.T) {
	issuer := cmmetav1.ObjectReference{Kind: "Issuer", Group: "cert-manager.io", Name: "rp-default-root-issuer"}
	duration := &metav1.Duration{Duration: 43800 * time.Hour}

	pki := PKI{
		Namespace: "ns",
		Labels:    map[string]string{"app": "redpanda"},
		Certificates: map[string]Certificate{
			"default": {
				Server: Keypair{
					Name:   "default",
					Secret: corev1.LocalObjectReference{Name: "rp-default-cert"},
					Request: &CertificateRequest{
						ObjectName: "rp-default-cert",
						DNSNames:   []string{"rp.ns"},
						Duration:   duration,
						IssuerRef:  issuer,
					},
				},
				Client: &Keypair{
					Name:   ClientKeypairName("default"),
					Secret: corev1.LocalObjectReference{Name: "rp-default-client-cert"},
					Request: &CertificateRequest{
						ObjectName: "rp-default-client",
						CommonName: "rp--default-client",
						Duration:   duration,
						IssuerRef:  issuer,
					},
				},
			},
			// User supplied: mounted, never issued.
			"byo": {
				Server: Keypair{Name: "byo", Secret: corev1.LocalObjectReference{Name: "my-own-secret"}},
			},
		},
	}

	certs := pki.Render()
	require.Len(t, certs, 2)

	server := certs[0]
	require.Equal(t, "rp-default-cert", server.Name)
	require.Equal(t, "ns", server.Namespace)
	require.Equal(t, map[string]string{"app": "redpanda"}, server.Labels)
	require.Equal(t, "cert-manager.io/v1", server.APIVersion)
	require.Equal(t, "rp-default-cert", server.Spec.SecretName)
	require.Equal(t, []string{"rp.ns"}, server.Spec.DNSNames)
	require.Empty(t, server.Spec.CommonName)
	require.False(t, server.Spec.IsCA)
	require.Equal(t, issuer, server.Spec.IssuerRef)
	require.Equal(t, certmanagerv1.ECDSAKeyAlgorithm, server.Spec.PrivateKey.Algorithm)
	require.Equal(t, 256, server.Spec.PrivateKey.Size)

	client := certs[1]
	require.Equal(t, "rp-default-client", client.Name)
	require.Equal(t, "rp-default-client-cert", client.Spec.SecretName)
	require.Equal(t, "rp--default-client", client.Spec.CommonName)
	require.Empty(t, client.Spec.DNSNames)
}

// TestKeypairPaths pins the derived names and paths. Both callers had their
// own copies of these format strings; a divergence silently unmounts
// certificates rather than failing to render.
func TestKeypairPaths(t *testing.T) {
	server := Keypair{Name: "default"}
	require.Equal(t, "/etc/tls/certs/default", server.MountPath())
	require.Equal(t, "redpanda-default-cert", server.VolumeName())
	require.Equal(t, "/etc/tls/certs/default/tls.crt", server.CertFile())
	require.Equal(t, "/etc/tls/certs/default/tls.key", server.KeyFile())

	// The client keypair's name carries the suffix, so one set of formatters
	// covers both halves.
	client := Keypair{Name: ClientKeypairName("default")}
	require.Equal(t, "default-client", client.Name)
	require.Equal(t, "/etc/tls/certs/default-client", client.MountPath())
	require.Equal(t, "redpanda-default-client-cert", client.VolumeName())
	require.Equal(t, "/etc/tls/certs/default-client/tls.crt", client.CertFile())
	require.Equal(t, "/etc/tls/certs/default-client/tls.key", client.KeyFile())
}

// TestKeypairCAFile asserts CAFile keys on the CA key, not on cert-manager's
// convention of always writing a ca.crt.
func TestKeypairCAFile(t *testing.T) {
	var noCA Keypair
	noCA.Name = "default"
	require.Empty(t, noCA.CAFile())

	withCA := Keypair{Name: "default", CA: ptr.To("ca.crt")}
	require.Equal(t, "/etc/tls/certs/default/ca.crt", withCA.CAFile())

	// The key is taken from CA, not assumed.
	otherKey := Keypair{Name: "default", CA: ptr.To("bundle.pem")}
	require.Equal(t, "/etc/tls/certs/default/bundle.pem", otherKey.CAFile())
}

// TestKeypairCAOrCertFile pins the fallback absent an explicit truststore: we
// can't assume a ca.crt was written.
func TestKeypairCAOrCertFile(t *testing.T) {
	pki := PKI{Certificates: map[string]Certificate{
		"withCA": {Server: Keypair{Name: "withCA", CA: ptr.To("ca.crt")}},
		"noCA":   {Server: Keypair{Name: "noCA"}},
	}}

	withCA := pki.ServerKeypair("withCA")
	require.Equal(t, "/etc/tls/certs/withCA/ca.crt", withCA.CAOrCertFile())

	noCA := pki.ServerKeypair("noCA")
	require.Equal(t, "/etc/tls/certs/noCA/tls.crt", noCA.CAOrCertFile())
}

// TestPKIVolumesAndMounts asserts every serving keypair precedes any client
// one, and that a client pair appears only for a Certificate carrying one.
// Pod templates merge by volume name, so the order is observable in rendered
// output.
func TestPKIVolumesAndMounts(t *testing.T) {
	pki := PKI{Certificates: map[string]Certificate{
		"default": {
			Server: Keypair{Name: "default", Secret: corev1.LocalObjectReference{Name: "release-default-cert"}},
			Client: &Keypair{
				Name:   ClientKeypairName("default"),
				Secret: corev1.LocalObjectReference{Name: "release-default-client-cert"},
			},
		},
		"external": {
			Server: Keypair{Name: "external", Secret: corev1.LocalObjectReference{Name: "release-external-cert"}},
		},
	}}

	mounts := pki.Mounts()
	require.Len(t, mounts, 3)
	require.Equal(t, "redpanda-default-cert", mounts[0].Name)
	require.Equal(t, "redpanda-external-cert", mounts[1].Name)
	require.Equal(t, "redpanda-default-client-cert", mounts[2].Name)

	volumes := pki.Volumes()
	require.Len(t, volumes, 3)
	require.Equal(t, "redpanda-default-cert", volumes[0].Name)
	require.Equal(t, "release-default-cert", volumes[0].Secret.SecretName)
	require.Equal(t, "redpanda-external-cert", volumes[1].Name)
	require.Equal(t, "redpanda-default-client-cert", volumes[2].Name)
	require.Equal(t, "release-default-client-cert", volumes[2].Secret.SecretName)

	var empty PKI
	require.Nil(t, empty.Volumes())
	require.Nil(t, empty.Mounts())
}
