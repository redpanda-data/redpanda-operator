// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"testing"
	"time"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestCertificates(t *testing.T) {
	cases := map[string]struct {
		Cert                   *TLSCert
		CertificateName        string
		ExpectedRootCertName   string
		ExpectedRootCertKey    string
		ExpectedClientCertName string
	}{
		"default": {
			CertificateName:        "default",
			ExpectedRootCertName:   "redpanda-default-root-certificate",
			ExpectedRootCertKey:    "tls.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"default with non-enabled global cert": {
			Cert: &TLSCert{
				Enabled: ptr.To(false),
				SecretRef: &corev1.LocalObjectReference{
					Name: "some-cert",
				},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "redpanda-default-root-certificate",
			ExpectedRootCertKey:    "tls.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"certificate with secret ref": {
			Cert: &TLSCert{
				SecretRef: &corev1.LocalObjectReference{
					Name: "some-cert",
				},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "some-cert",
			ExpectedRootCertKey:    "tls.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"certificate with CA": {
			Cert: &TLSCert{
				CAEnabled: true,
				SecretRef: &corev1.LocalObjectReference{
					Name: "some-cert",
				},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "some-cert",
			ExpectedRootCertKey:    "ca.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"certificate with client certificate": {
			Cert: &TLSCert{
				CAEnabled: true,
				SecretRef: &corev1.LocalObjectReference{
					Name: "some-cert",
				},
				ClientSecretRef: &corev1.LocalObjectReference{
					Name: "client-cert",
				},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "some-cert",
			ExpectedRootCertKey:    "ca.crt",
			ExpectedClientCertName: "client-cert",
		},
		"certificate with issuer ref": {
			Cert: &TLSCert{
				CAEnabled: true,
				IssuerRef: &cmmetav1.ObjectReference{Name: "private-ca", Kind: "ClusterIssuer"},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "redpanda-default-cert",
			ExpectedRootCertKey:    "ca.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"certificate with issuer ref without CA": {
			Cert: &TLSCert{
				IssuerRef: &cmmetav1.ObjectReference{Name: "private-ca", Kind: "ClusterIssuer"},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "redpanda-default-cert",
			ExpectedRootCertKey:    "tls.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
		"certificate with issuer ref and secret ref": {
			Cert: &TLSCert{
				CAEnabled: true,
				IssuerRef: &cmmetav1.ObjectReference{Name: "private-ca", Kind: "ClusterIssuer"},
				SecretRef: &corev1.LocalObjectReference{Name: "some-cert"},
			},
			CertificateName:        "default",
			ExpectedRootCertName:   "some-cert",
			ExpectedRootCertKey:    "ca.crt",
			ExpectedClientCertName: "redpanda-default-client-cert",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			certMap := TLSCertMap{}

			if c.Cert != nil {
				certMap[c.CertificateName] = *c.Cert
			}

			dot, err := Chart.Dot(nil, helmette.Release{
				Name:      "redpanda",
				Namespace: "redpanda",
				Service:   "Helm",
			}, Values{
				TLS: TLS{
					Certs: certMap,
				},
			})
			require.NoError(t, err)
			state, err := RenderStateFromDot(dot)
			require.NoError(t, err)

			actualRootCertName, actualRootCertKey, actualClientCertName := certificatesFor(state, c.CertificateName)
			require.Equal(t, c.ExpectedRootCertName, actualRootCertName)
			require.Equal(t, c.ExpectedRootCertKey, actualRootCertKey)
			require.Equal(t, c.ExpectedClientCertName, actualClientCertName)
		})
	}
}

// TestTLSConfig asserts that the operator's clients trust the certificate
// brokers serve, in each way the serving certificate can be issued.
func TestTLSConfig(t *testing.T) {
	t.Parallel()

	ctl := kubetest.NewEnv(t)
	ctx := t.Context()

	const namespace = "tls-config"
	serverName := "redpanda." + namespace + ".svc.cluster.local"

	_, err := kube.Create(ctx, ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})
	require.NoError(t, err)

	root := newTestCA(t, "root")
	rotated := newTestCA(t, "rotated")
	rootLeaf := root.issue(t, serverName)
	rotatedLeaf := rotated.issue(t, serverName)

	issuerRef := &cmmetav1.ObjectReference{Name: "private-ca", Kind: "ClusterIssuer"}

	cases := map[string]struct {
		cert    TLSCert
		secrets map[string]map[string][]byte
		served  *x509.Certificate
		wantErr string
	}{
		"self-signed reads the root certificate": {
			cert: TLSCert{CAEnabled: true},
			secrets: map[string]map[string][]byte{
				"redpanda-default-root-certificate": {"tls.crt": root.pem},
			},
			served: rootLeaf,
		},
		// NB: tls.crt is left out wherever ca.crt should be read; a served
		// leaf in the pool would verify itself.
		"issuer ref reads the serving ca.crt": {
			cert: TLSCert{CAEnabled: true, IssuerRef: issuerRef},
			secrets: map[string]map[string][]byte{
				"redpanda-default-cert": {"ca.crt": root.pem},
			},
			served: rootLeaf,
		},
		"issuer ref without CA reads the serving tls.crt": {
			cert: TLSCert{IssuerRef: issuerRef},
			secrets: map[string]map[string][]byte{
				"redpanda-default-cert": {"tls.crt": encodeCert(rootLeaf)},
			},
			served: rootLeaf,
		},
		"secret ref reads its ca.crt": {
			cert: TLSCert{CAEnabled: true, SecretRef: &corev1.LocalObjectReference{Name: "byo-cert"}},
			secrets: map[string]map[string][]byte{
				"byo-cert": {"ca.crt": root.pem},
			},
			served: rootLeaf,
		},
		"ca.crt bundle trusts every CA": {
			cert: TLSCert{CAEnabled: true, IssuerRef: issuerRef},
			secrets: map[string]map[string][]byte{
				"redpanda-default-cert": {"ca.crt": slices.Concat(root.pem, rotated.pem)},
			},
			served: rotatedLeaf,
		},
		"empty ca.crt": {
			cert: TLSCert{CAEnabled: true, IssuerRef: issuerRef},
			secrets: map[string]map[string][]byte{
				"redpanda-default-cert": {"tls.crt": encodeCert(rootLeaf), "ca.crt": {}},
			},
			wantErr: `no PEM certificates in key "ca.crt"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			release := helmette.Release{Name: "redpanda", Namespace: namespace, Service: "Helm"}
			dot, err := Chart.Dot(ctl.RestConfig(), release, Values{
				ClusterDomain: "cluster.local.",
				TLS:           TLS{Certs: TLSCertMap{"default": tc.cert}},
			})
			require.NoError(t, err)
			state, err := RenderStateFromDot(dot)
			require.NoError(t, err)

			for secretName, data := range tc.secrets {
				secret := corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data:       data,
				}
				_, err := kube.Create(ctx, ctl, secret)
				require.NoError(t, err)
				t.Cleanup(func() { _ = ctl.Delete(ctx, &secret) })
			}

			cfg, err := state.TLSConfig(InternalTLS{Cert: "default"})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			_, err = tc.served.Verify(x509.VerifyOptions{Roots: cfg.RootCAs, DNSName: cfg.ServerName})
			require.NoError(t, err)
		})
	}
}

func TestFetchBootstrapUser(t *testing.T) {
	t.Parallel()

	ctl := kubetest.NewEnv(t)
	ctx := t.Context()

	const namespace = "fetch-bootstrap-user"

	_, err := kube.Create(ctx, ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})
	require.NoError(t, err)

	// The chart-managed secret uses the default name format `<release>-bootstrap-user`.
	_, err = kube.Create(ctx, ctl, corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "redpanda-bootstrap-user", Namespace: namespace},
		Data:       map[string][]byte{"password": []byte("chart-managed-password")},
	})
	require.NoError(t, err)

	// An externally-managed secret with a non-default key name.
	_, err = kube.Create(ctx, ctl, corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "user-secret", Namespace: namespace},
		Data:       map[string][]byte{"custom-key": []byte("user-provided-password")},
	})
	require.NoError(t, err)

	makeState := func(sasl *SASLAuth) *RenderState {
		return &RenderState{
			Release: &helmette.Release{Name: "redpanda", Namespace: namespace},
			Values:  Values{Auth: Auth{SASL: sasl}},
			Dot:     &helmette.Dot{KubeConfig: ctl.RestConfig()},
		}
	}

	cases := map[string]struct {
		sasl         *SASLAuth
		wantPassword string
		wantSecret   bool
	}{
		"sasl nil": {
			sasl: nil,
		},
		"sasl disabled": {
			sasl: &SASLAuth{Enabled: false},
		},
		"chart-managed secret present": {
			sasl:         &SASLAuth{Enabled: true},
			wantPassword: "chart-managed-password",
			wantSecret:   true,
		},
		"user-provided secret present": {
			sasl: &SASLAuth{
				Enabled: true,
				BootstrapUser: BootstrapUser{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "user-secret"},
						Key:                  "custom-key",
					},
				},
			},
			wantPassword: "user-provided-password",
			wantSecret:   true,
		},
		"user-provided secret missing": {
			sasl: &SASLAuth{
				Enabled: true,
				BootstrapUser: BootstrapUser{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
						Key:                  "password",
					},
				},
			},
		},
		"user-provided secret wrong key": {
			sasl: &SASLAuth{
				Enabled: true,
				BootstrapUser: BootstrapUser{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "user-secret"},
						Key:                  "missing-key",
					},
				},
			},
			wantSecret: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			state := makeState(tc.sasl)
			state.FetchBootstrapUser()

			require.Equal(t, tc.wantPassword, state.BootstrapUserPassword)
			if tc.wantSecret {
				require.NotNil(t, state.BootstrapUserSecret)
				// The secret is re-rendered into the chart's output, so
				// server populated metadata must not survive the lookup or
				// Helm 4's server-side apply will reject the manifest.
				require.Nil(t, state.BootstrapUserSecret.ManagedFields)
				require.Empty(t, state.BootstrapUserSecret.ResourceVersion)
				require.Empty(t, state.BootstrapUserSecret.UID)
			} else {
				require.Nil(t, state.BootstrapUserSecret)
			}
		})
	}
}

func TestFirstUser(t *testing.T) {
	cases := []struct {
		In  string
		Out [3]string
	}{
		{
			In:  "hello:world:SCRAM-SHA-256",
			Out: [3]string{"hello", "world", "SCRAM-SHA-256"},
		},
		{
			In:  "name:password\n#Intentionally Blank\n",
			Out: [3]string{"name", "password", "SCRAM-SHA-512"},
		},
		{
			In:  "name:password:SCRAM-MD5-999",
			Out: [3]string{"", "", ""},
		},
	}

	for _, c := range cases {
		user, password, mechanism := firstUser([]byte(c.In))
		assert.Equal(t, [3]string{user, password, mechanism}, c.Out)
	}
}

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, cn string) testCA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return testCA{cert: cert, key: key, pem: encodeCert(cert)}
}

func (ca testCA) issue(t *testing.T, dnsName string) *x509.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return cert
}

func encodeCert(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}
