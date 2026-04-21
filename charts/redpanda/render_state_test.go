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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
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

func TestFetchBootstrapUser(t *testing.T) {
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
