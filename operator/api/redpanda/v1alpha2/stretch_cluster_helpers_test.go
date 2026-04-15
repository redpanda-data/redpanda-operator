// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func rawJSON(v any) *runtime.RawExtension {
	b, _ := json.Marshal(v)
	return &runtime.RawExtension{Raw: b}
}

func TestRackAwareness(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.RackAwareness
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.RackAwareness{}, false},
			{"explicitly disabled", &redpandav1alpha2.RackAwareness{Enabled: ptr.To(false)}, false},
			{"enabled", &redpandav1alpha2.RackAwareness{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("GetNodeAnnotation", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.RackAwareness
			expected string
		}{
			{"nil receiver", nil, redpandav1alpha2.DefaultRackAwarenessNodeAnnotation},
			{"zero value", &redpandav1alpha2.RackAwareness{}, redpandav1alpha2.DefaultRackAwarenessNodeAnnotation},
			{"custom value", &redpandav1alpha2.RackAwareness{NodeAnnotation: ptr.To("custom/zone")}, "custom/zone"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetNodeAnnotation())
			})
		}
	})
}

func TestMonitoring(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.Monitoring
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.Monitoring{}, false},
			{"enabled", &redpandav1alpha2.Monitoring{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestRBAC(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.RBAC
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value defaults to true", &redpandav1alpha2.RBAC{}, true},
			{"explicitly disabled", &redpandav1alpha2.RBAC{Enabled: ptr.To(false)}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestSASL(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.SASL
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.SASL{}, false},
			{"enabled", &redpandav1alpha2.SASL{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("GetMechanism", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.SASL
			expected string
		}{
			{"nil receiver", nil, redpandav1alpha2.DefaultSASLMechanism},
			{"zero value", &redpandav1alpha2.SASL{}, redpandav1alpha2.DefaultSASLMechanism},
			{"custom mechanism", &redpandav1alpha2.SASL{Mechanism: ptr.To("PLAIN")}, "PLAIN"},
			{"empty string falls back to default", &redpandav1alpha2.SASL{Mechanism: ptr.To("")}, redpandav1alpha2.DefaultSASLMechanism},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetMechanism())
			})
		}
	})
}

func TestAuth(t *testing.T) {
	t.Run("IsSASLEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.Auth
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.Auth{}, false},
			{"SASL enabled", &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)}}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsSASLEnabled())
			})
		}
	})
}

func TestTLS(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.TLS
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.TLS{}, false},
			{"enabled", &redpandav1alpha2.TLS{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("CertServerSecretName", func(t *testing.T) {
		t.Run("nil TLS uses default", func(t *testing.T) {
			assert.Equal(t, "release-default-cert", (*redpandav1alpha2.TLS)(nil).CertServerSecretName("release", "default"))
		})

		t.Run("no secret ref uses default", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{Certs: map[string]*redpandav1alpha2.Certificate{"my-cert": {}}}
			assert.Equal(t, "release-my-cert-cert", tls.CertServerSecretName("release", "my-cert"))
		})

		t.Run("with secret ref", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{Certs: map[string]*redpandav1alpha2.Certificate{
				"my-cert": {SecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("custom-secret")}},
			}}
			assert.Equal(t, "custom-secret", tls.CertServerSecretName("release", "my-cert"))
		})
	})

	t.Run("CertClientSecretName", func(t *testing.T) {
		t.Run("nil TLS uses default", func(t *testing.T) {
			assert.Equal(t, "release-default-client-cert", (*redpandav1alpha2.TLS)(nil).CertClientSecretName("release", "default"))
		})

		t.Run("with client secret ref", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{Certs: map[string]*redpandav1alpha2.Certificate{
				"my-cert": {ClientSecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("client-secret")}},
			}}
			assert.Equal(t, "client-secret", tls.CertClientSecretName("release", "my-cert"))
		})
	})

	t.Run("CertServerCAPath", func(t *testing.T) {
		t.Run("nil TLS falls back to tls.crt", func(t *testing.T) {
			assert.Equal(t, "/etc/tls/certs/default/tls.crt", (*redpandav1alpha2.TLS)(nil).CertServerCAPath("default"))
		})

		t.Run("CA enabled uses ca.crt", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{Certs: map[string]*redpandav1alpha2.Certificate{"my-cert": {CAEnabled: ptr.To(true)}}}
			assert.Equal(t, "/etc/tls/certs/my-cert/ca.crt", tls.CertServerCAPath("my-cert"))
		})

		t.Run("CA disabled uses tls.crt", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{Certs: map[string]*redpandav1alpha2.Certificate{"my-cert": {CAEnabled: ptr.To(false)}}}
			assert.Equal(t, "/etc/tls/certs/my-cert/tls.crt", tls.CertServerCAPath("my-cert"))
		})
	})

	t.Run("CertificatesFor", func(t *testing.T) {
		t.Run("nil TLS returns defaults", func(t *testing.T) {
			certSecret, certKey, clientSecret := (*redpandav1alpha2.TLS)(nil).CertificatesFor("release", "default")
			assert.Equal(t, "release-default-root-certificate", certSecret)
			assert.Equal(t, corev1.TLSCertKey, certKey)
			assert.Equal(t, "release-default-client-cert", clientSecret)
		})

		t.Run("with custom refs", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{
				Certs: map[string]*redpandav1alpha2.Certificate{
					"my-cert": {
						Enabled:         ptr.To(true),
						SecretRef:       &redpandav1alpha2.SecretRef{Name: ptr.To("custom-server")},
						ClientSecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("custom-client")},
					},
				},
			}
			certSecret, certKey, clientSecret := tls.CertificatesFor("release", "my-cert")
			assert.Equal(t, "custom-server", certSecret)
			assert.Equal(t, corev1.TLSCertKey, certKey)
			assert.Equal(t, "custom-client", clientSecret)
		})

		t.Run("unknown cert returns defaults with cert name", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{
				Certs: map[string]*redpandav1alpha2.Certificate{
					"my-cert": {Enabled: ptr.To(true)},
				},
			}
			certSecret, _, clientSecret := tls.CertificatesFor("release", "unknown")
			assert.Equal(t, "release-unknown-root-certificate", certSecret)
			assert.Equal(t, "release-default-client-cert", clientSecret)
		})

		t.Run("with issuerRef returns leaf cert secret and ca.crt key", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{
				Certs: map[string]*redpandav1alpha2.Certificate{
					"my-cert": {
						Enabled: ptr.To(true),
						IssuerRef: &redpandav1alpha2.IssuerRef{
							Name: ptr.To("my-issuer"),
							Kind: ptr.To("ClusterIssuer"),
						},
					},
				},
			}
			certSecret, certKey, clientSecret := tls.CertificatesFor("release", "my-cert")
			// When IssuerRef is set, the CA is in the leaf cert's secret under ca.crt.
			assert.Equal(t, "release-my-cert-cert", certSecret)
			assert.Equal(t, "ca.crt", certKey)
			assert.Equal(t, "release-my-cert-client-cert", clientSecret)
		})

		t.Run("with issuerRef and clientSecretRef", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{
				Certs: map[string]*redpandav1alpha2.Certificate{
					"my-cert": {
						Enabled: ptr.To(true),
						IssuerRef: &redpandav1alpha2.IssuerRef{
							Name: ptr.To("my-issuer"),
							Kind: ptr.To("Issuer"),
						},
						ClientSecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("custom-client")},
					},
				},
			}
			certSecret, certKey, clientSecret := tls.CertificatesFor("release", "my-cert")
			assert.Equal(t, "release-my-cert-cert", certSecret)
			assert.Equal(t, "ca.crt", certKey)
			assert.Equal(t, "custom-client", clientSecret)
		})

		t.Run("secretRef takes precedence over issuerRef", func(t *testing.T) {
			tls := &redpandav1alpha2.TLS{
				Certs: map[string]*redpandav1alpha2.Certificate{
					"my-cert": {
						Enabled:   ptr.To(true),
						SecretRef: &redpandav1alpha2.SecretRef{Name: ptr.To("custom-server")},
						IssuerRef: &redpandav1alpha2.IssuerRef{
							Name: ptr.To("my-issuer"),
						},
					},
				},
			}
			certSecret, certKey, clientSecret := tls.CertificatesFor("release", "my-cert")
			// SecretRef takes precedence — user provides everything.
			assert.Equal(t, "custom-server", certSecret)
			assert.Equal(t, corev1.TLSCertKey, certKey)
			assert.Equal(t, "release-my-cert-client-cert", clientSecret)
		})
	})
}

func TestStretchListenerTLS(t *testing.T) {
	t.Run("GetCert", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchListenerTLS
			expected string
		}{
			{"nil receiver", nil, ""},
			{"zero value", &redpandav1alpha2.StretchListenerTLS{}, ""},
			{"with cert", &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("my-cert")}, "my-cert"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetCert())
			})
		}
	})

	t.Run("RequiresClientAuth", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchListenerTLS
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.StretchListenerTLS{}, false},
			{"required", &redpandav1alpha2.StretchListenerTLS{RequireClientAuth: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.RequiresClientAuth())
			})
		}
	})

	t.Run("IsTLSEnabled", func(t *testing.T) {
		globalEnabled := &redpandav1alpha2.TLS{Enabled: ptr.To(true)}
		globalDisabled := &redpandav1alpha2.TLS{Enabled: ptr.To(false)}

		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchListenerTLS
			global   *redpandav1alpha2.TLS
			expected bool
		}{
			{"nil listener with global enabled", nil, globalEnabled, true},
			{"nil listener with global disabled", nil, globalDisabled, false},
			{"explicit enable overrides global disabled", &redpandav1alpha2.StretchListenerTLS{Enabled: ptr.To(true)}, globalDisabled, true},
			{"explicit disable overrides global enabled", &redpandav1alpha2.StretchListenerTLS{Enabled: ptr.To(false)}, globalEnabled, false},
			{"no override inherits global enabled", &redpandav1alpha2.StretchListenerTLS{}, globalEnabled, true},
			{"no override inherits global disabled", &redpandav1alpha2.StretchListenerTLS{}, globalDisabled, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsTLSEnabled(tt.global))
			})
		}
	})

	t.Run("ServerCAPath", func(t *testing.T) {
		tls := &redpandav1alpha2.TLS{
			Enabled: ptr.To(true),
			Certs:   map[string]*redpandav1alpha2.Certificate{"default": {CAEnabled: ptr.To(true)}},
		}

		t.Run("nil listener returns empty", func(t *testing.T) {
			assert.Equal(t, "", (*redpandav1alpha2.StretchListenerTLS)(nil).ServerCAPath(tls))
		})

		t.Run("no truststore falls back to cert-based path", func(t *testing.T) {
			lt := &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default")}
			assert.Equal(t, "/etc/tls/certs/default/ca.crt", lt.ServerCAPath(tls))
		})

		t.Run("truststore takes precedence", func(t *testing.T) {
			lt := &redpandav1alpha2.StretchListenerTLS{
				Cert: ptr.To("default"),
				TrustStore: &redpandav1alpha2.TrustStore{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "custom-ca"},
						Key:                  "ca.crt",
					},
				},
			}
			assert.Equal(t, "/etc/truststores/secrets/custom-ca-ca.crt", lt.ServerCAPath(tls))
		})

		t.Run("no cert name returns empty", func(t *testing.T) {
			lt := &redpandav1alpha2.StretchListenerTLS{}
			assert.Equal(t, "", lt.ServerCAPath(tls))
		})
	})
}

func TestCertificate(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.Certificate
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value defaults to true", &redpandav1alpha2.Certificate{}, true},
			{"explicitly disabled", &redpandav1alpha2.Certificate{Enabled: ptr.To(false)}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("IsCAEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.Certificate
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value defaults to true", &redpandav1alpha2.Certificate{}, true},
			{"explicitly disabled", &redpandav1alpha2.Certificate{CAEnabled: ptr.To(false)}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsCAEnabled())
			})
		}
	})

	t.Run("ShouldApplyInternalDNSNames", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.Certificate
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value", &redpandav1alpha2.Certificate{}, false},
			{"enabled", &redpandav1alpha2.Certificate{ApplyInternalDNSNames: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.ShouldApplyInternalDNSNames())
			})
		}
	})
}

func TestIssuerRef(t *testing.T) {
	tests := []struct {
		name         string
		input        *redpandav1alpha2.IssuerRef
		expectedName string
		expectedKind string
		expectedGrp  string
	}{
		{"nil receiver", nil, "", "Issuer", "cert-manager.io"},
		{"custom name", &redpandav1alpha2.IssuerRef{Name: ptr.To("my-issuer")}, "my-issuer", "Issuer", "cert-manager.io"},
		{"custom kind", &redpandav1alpha2.IssuerRef{Kind: ptr.To("ClusterIssuer")}, "", "ClusterIssuer", "cert-manager.io"},
		{"custom group", &redpandav1alpha2.IssuerRef{Group: ptr.To("custom.io")}, "", "Issuer", "custom.io"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedName, tt.input.GetName())
			assert.Equal(t, tt.expectedKind, tt.input.GetKind())
			assert.Equal(t, tt.expectedGrp, tt.input.GetGroup())
		})
	}
}

func TestStretchTuning(t *testing.T) {
	t.Run("IsTuneAioEventsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchTuning
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.StretchTuning{TuneAioEvents: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsTuneAioEventsEnabled())
			})
		}
	})
}

func TestExternal(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.External
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.External{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("GetDomain", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.External
			expected string
		}{
			{"nil receiver", nil, ""},
			{"with domain", &redpandav1alpha2.External{Domain: ptr.To("example.com")}, "example.com"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetDomain())
			})
		}
	})

	t.Run("GetType", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.External
			expected string
		}{
			{"nil receiver", nil, ""},
			{"NodePort", &redpandav1alpha2.External{Type: ptr.To("NodePort")}, "NodePort"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetType())
			})
		}
	})
}

func TestServiceAccount(t *testing.T) {
	t.Run("GetServiceAccountName", func(t *testing.T) {
		fullname := "release-name"
		tests := []struct {
			name     string
			input    *redpandav1alpha2.ServiceAccount
			expected string
		}{
			{"nil receiver", nil, fullname},
			{"create true no name", &redpandav1alpha2.ServiceAccount{Create: ptr.To(true)}, fullname},
			{"create true with name", &redpandav1alpha2.ServiceAccount{Create: ptr.To(true), Name: ptr.To("custom-sa")}, "custom-sa"},
			{"create false with name", &redpandav1alpha2.ServiceAccount{Create: ptr.To(false), Name: ptr.To("custom-sa")}, "custom-sa"},
			{"create false no name", &redpandav1alpha2.ServiceAccount{Create: ptr.To(false)}, fullname},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetServiceAccountName(fullname))
			})
		}
	})
}

func TestPersistentVolume(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.PersistentVolume
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.PersistentVolume{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestRedpandaImage(t *testing.T) {
	t.Run("AtLeast", func(t *testing.T) {
		tests := []struct {
			name     string
			tag      string
			version  string
			expected bool
		}{
			{"unparseable returns true", "latest", "22.3.0", true},
			{"equal", "22.3.0", "22.3.0", true},
			{"newer major", "23.1.0", "22.3.0", true},
			{"older minor", "22.2.0", "22.3.0", false},
			{"v-prefix", "v22.3.0", "22.3.0", true},
			{"newer patch", "22.3.1", "22.3.0", true},
			{"with pre-release", "22.3.0-rc1", "22.3.0", true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				img := &redpandav1alpha2.RedpandaImage{Tag: ptr.To(tt.tag)}
				assert.Equal(t, tt.expected, img.AtLeast(tt.version))
			})
		}
	})
}

func TestStretchListeners(t *testing.T) {
	t.Run("CertRequiresClientAuth", func(t *testing.T) {
		assert.False(t, (*redpandav1alpha2.StretchListeners)(nil).CertRequiresClientAuth("default"))

		l := &redpandav1alpha2.StretchListeners{
			Admin: &redpandav1alpha2.StretchAPIListener{
				StretchListener: redpandav1alpha2.StretchListener{
					TLS: &redpandav1alpha2.StretchListenerTLS{
						Cert:              ptr.To("admin-cert"),
						RequireClientAuth: ptr.To(true),
					},
				},
			},
			Kafka: &redpandav1alpha2.StretchAPIListener{
				StretchListener: redpandav1alpha2.StretchListener{
					TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("kafka-cert")},
				},
			},
		}
		assert.True(t, l.CertRequiresClientAuth("admin-cert"))
		assert.False(t, l.CertRequiresClientAuth("kafka-cert"))
		assert.False(t, l.CertRequiresClientAuth("unknown"))
	})

	t.Run("CollectCerts", func(t *testing.T) {
		assert.Nil(t, (*redpandav1alpha2.StretchListeners)(nil).CollectCerts(func(*redpandav1alpha2.StretchListenerTLS) bool { return true }))

		l := &redpandav1alpha2.StretchListeners{
			Admin: &redpandav1alpha2.StretchAPIListener{
				StretchListener: redpandav1alpha2.StretchListener{
					TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("shared-cert")},
				},
			},
			Kafka: &redpandav1alpha2.StretchAPIListener{
				StretchListener: redpandav1alpha2.StretchListener{
					TLS: &redpandav1alpha2.StretchListenerTLS{
						Cert:              ptr.To("shared-cert"),
						RequireClientAuth: ptr.To(true),
					},
				},
			},
		}

		all := l.CollectCerts(func(*redpandav1alpha2.StretchListenerTLS) bool { return true })
		assert.Equal(t, []string{"shared-cert"}, all)

		mtls := l.CollectCerts(func(tls *redpandav1alpha2.StretchListenerTLS) bool { return tls.RequiresClientAuth() })
		assert.Equal(t, []string{"shared-cert"}, mtls)
	})

	t.Run("TrustStores", func(t *testing.T) {
		tls := &redpandav1alpha2.TLS{Enabled: ptr.To(true)}

		t.Run("nil listeners", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchListeners)(nil).TrustStores(tls))
		})

		t.Run("no truststores configured", func(t *testing.T) {
			l := &redpandav1alpha2.StretchListeners{
				Admin: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default")}},
				},
			}
			assert.Nil(t, l.TrustStores(tls))
		})

		t.Run("with truststores on internal and external listeners", func(t *testing.T) {
			cmTS := &redpandav1alpha2.TrustStore{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "cm1"},
					Key:                  "ca.crt",
				},
			}
			secretTS := &redpandav1alpha2.TrustStore{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
					Key:                  "ca.crt",
				},
			}
			l := &redpandav1alpha2.StretchListeners{
				Admin: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default"), TrustStore: cmTS}},
				},
				Kafka: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default")}},
					External: map[string]*redpandav1alpha2.StretchExternalListener{
						"ext": {
							StretchListener: redpandav1alpha2.StretchListener{
								TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("external"), TrustStore: secretTS},
							},
						},
					},
				},
			}
			stores := l.TrustStores(tls)
			assert.Len(t, stores, 2)
			assert.Equal(t, cmTS, stores[0])
			assert.Equal(t, secretTS, stores[1])
		})
	})

	t.Run("TrustStoreVolume", func(t *testing.T) {
		tls := &redpandav1alpha2.TLS{Enabled: ptr.To(true)}

		t.Run("no truststores returns nil", func(t *testing.T) {
			l := &redpandav1alpha2.StretchListeners{
				Admin: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("default")}},
				},
			}
			assert.Nil(t, l.TrustStoreVolume(tls))
		})

		t.Run("single secret truststore", func(t *testing.T) {
			l := &redpandav1alpha2.StretchListeners{
				Admin: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{
						Cert: ptr.To("default"),
						TrustStore: &redpandav1alpha2.TrustStore{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
								Key:                  "ca.crt",
							},
						},
					}},
				},
			}
			vol := l.TrustStoreVolume(tls)
			require.NotNil(t, vol)
			assert.Equal(t, "truststores", vol.Name)
			require.NotNil(t, vol.Projected)
			require.Len(t, vol.Projected.Sources, 1)
			require.NotNil(t, vol.Projected.Sources[0].Secret)
			assert.Equal(t, "my-secret", vol.Projected.Sources[0].Secret.Name)
		})

		t.Run("mixed ConfigMap and Secret truststores", func(t *testing.T) {
			l := &redpandav1alpha2.StretchListeners{
				Admin: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{
						Cert: ptr.To("default"),
						TrustStore: &redpandav1alpha2.TrustStore{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "cm1"},
								Key:                  "ca.crt",
							},
						},
					}},
				},
				Kafka: &redpandav1alpha2.StretchAPIListener{
					StretchListener: redpandav1alpha2.StretchListener{TLS: &redpandav1alpha2.StretchListenerTLS{
						Cert: ptr.To("default"),
						TrustStore: &redpandav1alpha2.TrustStore{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
								Key:                  "ca.crt",
							},
						},
					}},
				},
			}
			vol := l.TrustStoreVolume(tls)
			require.NotNil(t, vol)
			require.Len(t, vol.Projected.Sources, 2)
			require.NotNil(t, vol.Projected.Sources[0].ConfigMap)
			assert.Equal(t, "cm1", vol.Projected.Sources[0].ConfigMap.Name)
			require.NotNil(t, vol.Projected.Sources[1].Secret)
			assert.Equal(t, "secret1", vol.Projected.Sources[1].Secret.Name)
		})
	})
}

func TestTrustStore(t *testing.T) {
	t.Run("TrustStoreFilePath", func(t *testing.T) {
		t.Run("ConfigMap ref", func(t *testing.T) {
			ts := &redpandav1alpha2.TrustStore{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
					Key:                  "ca.crt",
				},
			}
			assert.Equal(t, "/etc/truststores/configmaps/my-cm-ca.crt", ts.TrustStoreFilePath())
		})

		t.Run("Secret ref", func(t *testing.T) {
			ts := &redpandav1alpha2.TrustStore{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
					Key:                  "tls.crt",
				},
			}
			assert.Equal(t, "/etc/truststores/secrets/my-secret-tls.crt", ts.TrustStoreFilePath())
		})
	})

	t.Run("VolumeProjection", func(t *testing.T) {
		t.Run("ConfigMap ref", func(t *testing.T) {
			ts := &redpandav1alpha2.TrustStore{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
					Key:                  "ca.crt",
				},
			}
			proj := ts.VolumeProjection()
			require.NotNil(t, proj.ConfigMap)
			assert.Nil(t, proj.Secret)
			assert.Equal(t, "my-cm", proj.ConfigMap.Name)
			require.Len(t, proj.ConfigMap.Items, 1)
			assert.Equal(t, "ca.crt", proj.ConfigMap.Items[0].Key)
			assert.Equal(t, "configmaps/my-cm-ca.crt", proj.ConfigMap.Items[0].Path)
		})

		t.Run("Secret ref", func(t *testing.T) {
			ts := &redpandav1alpha2.TrustStore{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
					Key:                  "tls.crt",
				},
			}
			proj := ts.VolumeProjection()
			assert.Nil(t, proj.ConfigMap)
			require.NotNil(t, proj.Secret)
			assert.Equal(t, "my-secret", proj.Secret.Name)
			require.Len(t, proj.Secret.Items, 1)
			assert.Equal(t, "tls.crt", proj.Secret.Items[0].Key)
			assert.Equal(t, "secrets/my-secret-tls.crt", proj.Secret.Items[0].Path)
		})
	})
}

func TestStretchClusterSpec(t *testing.T) {
	t.Run("TLSEnabled", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsAdminTLSEnabled())
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsKafkaTLSEnabled())
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsHTTPTLSEnabled())
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsSchemaRegistryTLSEnabled())
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsRPCTLSEnabled())
		})

		t.Run("global TLS enabled no listener overrides", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(true)}}
			assert.True(t, spec.IsAdminTLSEnabled())
			assert.True(t, spec.IsKafkaTLSEnabled())
		})

		t.Run("listener explicitly disables", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(true)},
				Listeners: &redpandav1alpha2.StretchListeners{
					Admin: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{
							TLS: &redpandav1alpha2.StretchListenerTLS{Enabled: ptr.To(false)},
						},
					},
				},
			}
			assert.False(t, spec.IsAdminTLSEnabled())
			assert.True(t, spec.IsKafkaTLSEnabled(), "kafka inherits global")
		})
	})

	t.Run("Ports", func(t *testing.T) {
		t.Run("defaults", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{}
			assert.Equal(t, redpandav1alpha2.DefaultAdminPort, spec.AdminPort())
			assert.Equal(t, redpandav1alpha2.DefaultKafkaPort, spec.KafkaPort())
			assert.Equal(t, redpandav1alpha2.DefaultHTTPPort, spec.HTTPPort())
			assert.Equal(t, redpandav1alpha2.DefaultRPCPort, spec.RPCPort())
			assert.Equal(t, redpandav1alpha2.DefaultSchemaRegistryPort, spec.SchemaRegistryPort())
		})

		t.Run("custom ports", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Listeners: &redpandav1alpha2.StretchListeners{
					Admin: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(1234))},
					},
					Kafka: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(5678))},
					},
					RPC: &redpandav1alpha2.StretchRPC{Port: ptr.To(9999)},
				},
			}
			assert.Equal(t, int32(1234), spec.AdminPort())
			assert.Equal(t, int32(5678), spec.KafkaPort())
			assert.Equal(t, int32(9999), spec.RPCPort())
		})
	})

	t.Run("ClusterDomain", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchClusterSpec
			expected string
		}{
			{"nil receiver", nil, redpandav1alpha2.DefaultClusterDomain},
			{"zero value", &redpandav1alpha2.StretchClusterSpec{}, redpandav1alpha2.DefaultClusterDomain},
			{"custom domain", &redpandav1alpha2.StretchClusterSpec{ClusterDomain: ptr.To("custom.local")}, "custom.local"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetClusterDomain())
			})
		}
	})

	t.Run("GetServiceName", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchClusterSpec
			expected string
		}{
			{"default", &redpandav1alpha2.StretchClusterSpec{}, "my-release"},
			{"custom", &redpandav1alpha2.StretchClusterSpec{Service: &redpandav1alpha2.Service{Name: ptr.To("custom-svc")}}, "custom-svc"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetServiceName("my-release"))
			})
		}
	})

	t.Run("InternalDomain", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchClusterSpec
			expected string
		}{
			{"default domain", &redpandav1alpha2.StretchClusterSpec{}, "release.ns.svc.cluster.local."},
			{"custom domain without trailing dot", &redpandav1alpha2.StretchClusterSpec{ClusterDomain: ptr.To("custom.local")}, "release.ns.svc.custom.local"},
			{"custom domain with trailing dot", &redpandav1alpha2.StretchClusterSpec{ClusterDomain: ptr.To("custom.local.")}, "release.ns.svc.custom.local."},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.InternalDomain("release", "ns"))
			})
		}
	})

	t.Run("InUseCerts", func(t *testing.T) {
		t.Run("no TLS", func(t *testing.T) {
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).InUseServerCerts())
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).InUseClientCerts())
		})

		t.Run("with listeners", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(true)},
				Listeners: &redpandav1alpha2.StretchListeners{
					Admin: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{
							TLS: &redpandav1alpha2.StretchListenerTLS{
								Cert:              ptr.To("admin-cert"),
								RequireClientAuth: ptr.To(true),
							},
						},
					},
					Kafka: &redpandav1alpha2.StretchAPIListener{
						StretchListener: redpandav1alpha2.StretchListener{
							TLS: &redpandav1alpha2.StretchListenerTLS{Cert: ptr.To("kafka-cert")},
						},
					},
				},
			}
			assert.Equal(t, []string{"admin-cert", "kafka-cert"}, spec.InUseServerCerts())
			assert.Equal(t, []string{"admin-cert"}, spec.InUseClientCerts())
		})
	})

	t.Run("AdminInternalHTTPProtocol", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchClusterSpec
			expected string
		}{
			{"no TLS", &redpandav1alpha2.StretchClusterSpec{}, "http"},
			{"with TLS", &redpandav1alpha2.StretchClusterSpec{TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(true)}}, "https"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.AdminInternalHTTPProtocol())
			})
		}
	})

	t.Run("AdminAPIURLs", func(t *testing.T) {
		spec := &redpandav1alpha2.StretchClusterSpec{}
		assert.Equal(t, "${SERVICE_NAME}.release.ns.svc.cluster.local.:9644", spec.AdminAPIURLs("release", "ns"))
	})

	t.Run("AdminInternalURL", func(t *testing.T) {
		t.Run("without TLS", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{}
			assert.Equal(t, "http://${SERVICE_NAME}.release.ns.svc.cluster.local:9644", spec.AdminInternalURL("release", "ns"))
		})

		t.Run("with TLS", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{TLS: &redpandav1alpha2.TLS{Enabled: ptr.To(true)}}
			assert.Equal(t, "https://${SERVICE_NAME}.release.ns.svc.cluster.local:9644", spec.AdminInternalURL("release", "ns"))
		})
	})

	t.Run("Config", func(t *testing.T) {
		t.Run("GetNodeConfig", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetNodeConfig())
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).GetNodeConfig())

			spec := &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{
					Node: rawJSON(map[string]any{"developer_mode": true}),
				},
			}
			cfg := spec.GetNodeConfig()
			require.NotNil(t, cfg)
			assert.Equal(t, true, cfg["developer_mode"])
		})

		t.Run("GetClusterConfig", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetClusterConfig())
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).GetClusterConfig())

			spec := &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{
					Cluster: rawJSON(map[string]any{"enable_metrics_reporter": false}),
				},
			}
			cfg := spec.GetClusterConfig()
			require.NotNil(t, cfg)
			assert.Equal(t, false, cfg["enable_metrics_reporter"])
		})

		t.Run("GetTunableConfig", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetTunableConfig())
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).GetTunableConfig())

			spec := &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{
					Tunable: rawJSON(map[string]any{"log_segment_size": 1048576}),
				},
			}
			cfg := spec.GetTunableConfig()
			require.NotNil(t, cfg)
			assert.Equal(t, float64(1048576), cfg["log_segment_size"])
		})
	})

	t.Run("NodeConfigBoolValue", func(t *testing.T) {
		tests := []struct {
			name     string
			spec     *redpandav1alpha2.StretchClusterSpec
			key      string
			expected bool
		}{
			{"nil spec", nil, "key", false},
			{"no config", &redpandav1alpha2.StretchClusterSpec{}, "key", false},
			{"bool true", &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{Node: rawJSON(map[string]any{"pandaproxy_client.use_localhost": true})},
			}, "pandaproxy_client.use_localhost", true},
			{"bool false", &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{Node: rawJSON(map[string]any{"pandaproxy_client.use_localhost": false})},
			}, "pandaproxy_client.use_localhost", false},
			{"string true", &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{Node: rawJSON(map[string]any{"some_key": "true"})},
			}, "some_key", true},
			{"string false", &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{Node: rawJSON(map[string]any{"some_key": "false"})},
			}, "some_key", false},
			{"missing key", &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{Node: rawJSON(map[string]any{"other": true})},
			}, "nonexistent", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.spec.NodeConfigBoolValue(tt.key))
			})
		}
	})

	t.Run("IsAuditLoggingEnabled", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsAuditLoggingEnabled())
		})

		t.Run("empty spec", func(t *testing.T) {
			assert.False(t, (&redpandav1alpha2.StretchClusterSpec{}).IsAuditLoggingEnabled())
		})

		t.Run("all conditions met", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Image:        &redpandav1alpha2.RedpandaImage{Tag: ptr.To("23.3.1")},
				AuditLogging: &redpandav1alpha2.AuditLogging{Enabled: ptr.To(true)},
				Auth:         &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)}},
			}
			assert.True(t, spec.IsAuditLoggingEnabled())
		})

		t.Run("version too old", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Image:        &redpandav1alpha2.RedpandaImage{Tag: ptr.To("23.2.0")},
				AuditLogging: &redpandav1alpha2.AuditLogging{Enabled: ptr.To(true)},
				Auth:         &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)}},
			}
			assert.False(t, spec.IsAuditLoggingEnabled())
		})

		t.Run("audit logging disabled", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Image:        &redpandav1alpha2.RedpandaImage{Tag: ptr.To("23.3.1")},
				AuditLogging: &redpandav1alpha2.AuditLogging{Enabled: ptr.To(false)},
				Auth:         &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(true)}},
			}
			assert.False(t, spec.IsAuditLoggingEnabled())
		})

		t.Run("SASL disabled", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Image:        &redpandav1alpha2.RedpandaImage{Tag: ptr.To("23.3.1")},
				AuditLogging: &redpandav1alpha2.AuditLogging{Enabled: ptr.To(true)},
				Auth:         &redpandav1alpha2.Auth{SASL: &redpandav1alpha2.SASL{Enabled: ptr.To(false)}},
			}
			assert.False(t, spec.IsAuditLoggingEnabled())
		})
	})

	t.Run("IsMetricsReporterEnabled", func(t *testing.T) {
		t.Run("nil spec defaults to true", func(t *testing.T) {
			assert.True(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsMetricsReporterEnabled())
		})

		t.Run("empty spec defaults to true", func(t *testing.T) {
			assert.True(t, (&redpandav1alpha2.StretchClusterSpec{}).IsMetricsReporterEnabled())
		})

		t.Run("cluster config disables it", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{
					Cluster: rawJSON(map[string]any{"enable_metrics_reporter": false}),
				},
			}
			assert.False(t, spec.IsMetricsReporterEnabled())
		})

		t.Run("cluster config enables it", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Config: &redpandav1alpha2.Config{
					Cluster: rawJSON(map[string]any{"enable_metrics_reporter": true}),
				},
			}
			assert.True(t, spec.IsMetricsReporterEnabled())
		})

		t.Run("UsageStats disabled", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Logging: &redpandav1alpha2.StretchLogging{
					UsageStats: &redpandav1alpha2.StretchUsageStats{Enabled: ptr.To(false)},
				},
			}
			assert.False(t, spec.IsMetricsReporterEnabled())
		})

		t.Run("UsageStats enabled", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Logging: &redpandav1alpha2.StretchLogging{
					UsageStats: &redpandav1alpha2.StretchUsageStats{Enabled: ptr.To(true)},
				},
			}
			assert.True(t, spec.IsMetricsReporterEnabled())
		})
	})

	t.Run("GetStorageMinFreeBytes", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.Equal(t, int64(5*redpandav1alpha2.GiB), (*redpandav1alpha2.StretchClusterSpec)(nil).GetStorageMinFreeBytes())
		})

		t.Run("no PV", func(t *testing.T) {
			assert.Equal(t, int64(5*redpandav1alpha2.GiB), (&redpandav1alpha2.StretchClusterSpec{}).GetStorageMinFreeBytes())
		})

		t.Run("small PV uses 5 percent", func(t *testing.T) {
			smallSize := resource.MustParse("20Gi")
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					PersistentVolume: &redpandav1alpha2.PersistentVolume{
						Enabled: ptr.To(true),
						Size:    &smallSize,
					},
				},
			}
			expected := smallSize.Value() * 5 / 100
			assert.Equal(t, expected, spec.GetStorageMinFreeBytes())
		})

		t.Run("large PV capped at 5 GiB", func(t *testing.T) {
			largeSize := resource.MustParse("500Gi")
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					PersistentVolume: &redpandav1alpha2.PersistentVolume{
						Enabled: ptr.To(true),
						Size:    &largeSize,
					},
				},
			}
			assert.Equal(t, int64(5*redpandav1alpha2.GiB), spec.GetStorageMinFreeBytes())
		})
	})

	t.Run("GetTieredStorageCacheSize", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetTieredStorageCacheSize())
		})

		t.Run("empty spec", func(t *testing.T) {
			assert.Nil(t, (&redpandav1alpha2.StretchClusterSpec{}).GetTieredStorageCacheSize())
		})

		t.Run("valid size", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{
						Config: &redpandav1alpha2.StretchTieredConfig{
							CloudStorageCacheSize: ptr.To("10Gi"),
						},
					},
				},
			}
			q := spec.GetTieredStorageCacheSize()
			require.NotNil(t, q)
			expected := resource.MustParse("10Gi")
			assert.True(t, q.Equal(expected))
		})

		t.Run("invalid size returns nil", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{
						Config: &redpandav1alpha2.StretchTieredConfig{
							CloudStorageCacheSize: ptr.To("not-a-quantity"),
						},
					},
				},
			}
			assert.Nil(t, spec.GetTieredStorageCacheSize())
		})
	})

	t.Run("TieredStorage", func(t *testing.T) {
		t.Run("IsTieredStorageEnabled", func(t *testing.T) {
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).IsTieredStorageEnabled())
			assert.False(t, (&redpandav1alpha2.StretchClusterSpec{}).IsTieredStorageEnabled())

			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{
						Config: &redpandav1alpha2.StretchTieredConfig{
							CloudStorageEnabled: ptr.To(true),
						},
					},
				},
			}
			assert.True(t, spec.IsTieredStorageEnabled())

			spec.Storage.Tiered.Config.CloudStorageEnabled = ptr.To(false)
			assert.False(t, spec.IsTieredStorageEnabled())
		})

		t.Run("TieredMountType", func(t *testing.T) {
			assert.Equal(t, "none", (&redpandav1alpha2.StretchClusterSpec{}).TieredMountType())
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{MountType: ptr.To("emptyDir")},
				},
			}
			assert.Equal(t, "emptyDir", spec.TieredMountType())
		})

		t.Run("TieredCacheDirectory", func(t *testing.T) {
			assert.Equal(t, redpandav1alpha2.DefaultTieredStorageCacheDir, (&redpandav1alpha2.StretchClusterSpec{}).TieredCacheDirectory())
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{
						Config: &redpandav1alpha2.StretchTieredConfig{
							CloudStorageCacheDirectory: ptr.To("/custom/cache"),
						},
					},
				},
			}
			assert.Equal(t, "/custom/cache", spec.TieredCacheDirectory())
		})

		t.Run("TieredStorageVolumeName", func(t *testing.T) {
			assert.Equal(t, "tiered-storage-dir", (&redpandav1alpha2.StretchClusterSpec{}).TieredStorageVolumeName())
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{
						PersistentVolume: &redpandav1alpha2.PersistentVolume{
							NameOverwrite: ptr.To("custom-vol"),
						},
					},
				},
			}
			assert.Equal(t, "custom-vol", spec.TieredStorageVolumeName())
		})

		t.Run("TieredStorageHostPath", func(t *testing.T) {
			assert.Equal(t, "", (&redpandav1alpha2.StretchClusterSpec{}).TieredStorageHostPath())
			spec := &redpandav1alpha2.StretchClusterSpec{
				Storage: &redpandav1alpha2.StretchStorage{
					Tiered: &redpandav1alpha2.StretchTiered{HostPath: ptr.To("/mnt/tiered")},
				},
			}
			assert.Equal(t, "/mnt/tiered", spec.TieredStorageHostPath())
		})
	})

	t.Run("GetResourceRequirements", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.Equal(t, corev1.ResourceRequirements{}, (*redpandav1alpha2.StretchClusterSpec)(nil).GetResourceRequirements())
		})

		t.Run("new style Limits and Requests", func(t *testing.T) {
			limits := map[corev1.ResourceName]resource.Quantity{corev1.ResourceCPU: resource.MustParse("2")}
			requests := map[corev1.ResourceName]resource.Quantity{corev1.ResourceMemory: resource.MustParse("4Gi")}
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{
					Limits:   limits,
					Requests: requests,
				},
			}
			reqs := spec.GetResourceRequirements()
			assert.Equal(t, corev1.ResourceList(limits), reqs.Limits)
			assert.Equal(t, corev1.ResourceList(requests), reqs.Requests)
		})

		t.Run("legacy CPU Cores and Memory", func(t *testing.T) {
			cores := resource.MustParse("1")
			mem := resource.MustParse("2.5Gi")
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{
					CPU:    &redpandav1alpha2.CPU{Cores: &cores},
					Memory: &redpandav1alpha2.Memory{Container: &redpandav1alpha2.ContainerResources{Max: &mem}},
				},
			}
			reqs := spec.GetResourceRequirements()
			assert.Equal(t, cores, reqs.Limits[corev1.ResourceCPU])
			assert.Equal(t, mem, reqs.Limits[corev1.ResourceMemory])
			assert.Nil(t, reqs.Requests)
		})

		t.Run("no resources set", func(t *testing.T) {
			spec := &redpandav1alpha2.StretchClusterSpec{Resources: &redpandav1alpha2.StretchResources{}}
			assert.Equal(t, corev1.ResourceRequirements{}, spec.GetResourceRequirements())
		})
	})

	t.Run("GetRedpandaStartFlags", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.Nil(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetRedpandaStartFlags())
		})

		t.Run("new mode Limits and Requests", func(t *testing.T) {
			limits := map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}
			requests := map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{Limits: limits, Requests: requests},
			}
			flags := spec.GetRedpandaStartFlags()
			assert.Equal(t, "0M", flags["--reserve-memory"])
			assert.Equal(t, "2", flags["--smp"])
			assert.Contains(t, flags, "--memory")
		})

		t.Run("sub-core CPU clamps to 1", func(t *testing.T) {
			limits := map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}
			requests := map[corev1.ResourceName]resource.Quantity{corev1.ResourceCPU: resource.MustParse("500m")}
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{Limits: limits, Requests: requests},
			}
			flags := spec.GetRedpandaStartFlags()
			assert.Equal(t, "1", flags["--smp"], "sub-core should clamp to 1")
		})

		t.Run("legacy mode", func(t *testing.T) {
			cores := resource.MustParse("2")
			mem := resource.MustParse("4Gi")
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{
					CPU:    &redpandav1alpha2.CPU{Cores: &cores},
					Memory: &redpandav1alpha2.Memory{Container: &redpandav1alpha2.ContainerResources{Max: &mem}},
				},
			}
			flags := spec.GetRedpandaStartFlags()
			assert.Equal(t, "2", flags["--smp"])
			assert.Contains(t, flags, "--memory")
			assert.Contains(t, flags, "--reserve-memory")
		})
	})

	t.Run("GetOverProvisionValue", func(t *testing.T) {
		t.Run("nil spec", func(t *testing.T) {
			assert.False(t, (*redpandav1alpha2.StretchClusterSpec)(nil).GetOverProvisionValue())
		})

		t.Run("empty spec", func(t *testing.T) {
			assert.False(t, (&redpandav1alpha2.StretchClusterSpec{}).GetOverProvisionValue())
		})

		t.Run("sub-core in new mode is overprovisioned", func(t *testing.T) {
			requests := map[corev1.ResourceName]resource.Quantity{corev1.ResourceCPU: resource.MustParse("500m")}
			limits := map[corev1.ResourceName]resource.Quantity{corev1.ResourceCPU: resource.MustParse("500m")}
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{Limits: limits, Requests: requests},
			}
			assert.True(t, spec.GetOverProvisionValue())
		})

		t.Run("full core in new mode is not overprovisioned", func(t *testing.T) {
			requests := map[corev1.ResourceName]resource.Quantity{corev1.ResourceCPU: resource.MustParse("1")}
			spec := &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{Limits: requests, Requests: requests},
			}
			assert.False(t, spec.GetOverProvisionValue())
		})

		t.Run("legacy sub-core", func(t *testing.T) {
			cores := resource.MustParse("500m")
			spec := &redpandav1alpha2.StretchClusterSpec{Resources: &redpandav1alpha2.StretchResources{CPU: &redpandav1alpha2.CPU{Cores: &cores}}}
			assert.True(t, spec.GetOverProvisionValue())
		})
	})

	t.Run("GetEnableMemoryLocking", func(t *testing.T) {
		tests := []struct {
			name     string
			spec     *redpandav1alpha2.StretchClusterSpec
			expected bool
		}{
			{"nil spec", nil, false},
			{"empty spec", &redpandav1alpha2.StretchClusterSpec{}, false},
			{"enabled", &redpandav1alpha2.StretchClusterSpec{
				Resources: &redpandav1alpha2.StretchResources{
					Memory: &redpandav1alpha2.Memory{EnableMemoryLocking: ptr.To(true)},
				},
			}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.spec.GetEnableMemoryLocking())
			})
		}
	})
}

func TestNodePool(t *testing.T) {
	t.Run("GetReplicas", func(t *testing.T) {
		tests := []struct {
			name     string
			pool     *redpandav1alpha2.NodePool
			expected int32
		}{
			{"default", &redpandav1alpha2.NodePool{}, 1},
			{"custom", &redpandav1alpha2.NodePool{Spec: redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{Replicas: ptr.To(int32(3))}}}, 3},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.pool.GetReplicas())
			})
		}
	})

	t.Run("Suffix", func(t *testing.T) {
		t.Run("unnamed pool", func(t *testing.T) {
			assert.Equal(t, "", (&redpandav1alpha2.NodePool{}).Suffix())
		})

		t.Run("named pool", func(t *testing.T) {
			pool := &redpandav1alpha2.NodePool{}
			pool.Name = "fast"
			assert.Equal(t, "-fast", pool.Suffix())
		})
	})

	t.Run("RedpandaImage", func(t *testing.T) {
		t.Run("default", func(t *testing.T) {
			assert.Equal(t, redpandav1alpha2.DefaultRedpandaRepository+":"+redpandav1alpha2.DefaultRedpandaImageTag, (&redpandav1alpha2.NodePool{}).RedpandaImage())
		})

		t.Run("custom", func(t *testing.T) {
			pool := &redpandav1alpha2.NodePool{Spec: redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Image: &redpandav1alpha2.RedpandaImage{Repository: ptr.To("custom/repo"), Tag: ptr.To("v1.0")},
			}}}
			assert.Equal(t, "custom/repo:v1.0", pool.RedpandaImage())
		})
	})

	t.Run("SidecarImage", func(t *testing.T) {
		t.Run("default", func(t *testing.T) {
			assert.Equal(t, redpandav1alpha2.DefaultSidecarRepository+":"+redpandav1alpha2.DefaultOperatorImageTag, (&redpandav1alpha2.NodePool{}).SidecarImage())
		})

		t.Run("custom", func(t *testing.T) {
			pool := &redpandav1alpha2.NodePool{Spec: redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				SidecarImage: &redpandav1alpha2.RedpandaImage{Repository: ptr.To("custom/sidecar"), Tag: ptr.To("v2.0")},
			}}}
			assert.Equal(t, "custom/sidecar:v2.0", pool.SidecarImage())
		})
	})

	t.Run("InitImage", func(t *testing.T) {
		t.Run("default", func(t *testing.T) {
			assert.Equal(t, redpandav1alpha2.DefaultInitContainerRepository+":"+redpandav1alpha2.DefaultInitContainerImageTag, (&redpandav1alpha2.NodePool{}).InitImage())
		})

		t.Run("custom", func(t *testing.T) {
			pool := &redpandav1alpha2.NodePool{Spec: redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				InitContainerImage: &redpandav1alpha2.InitContainerImage{Repository: ptr.To("custom/init"), Tag: ptr.To("v3.0")},
			}}}
			assert.Equal(t, "custom/init:v3.0", pool.InitImage())
		})
	})
}

func TestInitContainerFlags(t *testing.T) {
	t.Run("FsValidator", func(t *testing.T) {
		t.Run("IsEnabled", func(t *testing.T) {
			tests := []struct {
				name     string
				input    *redpandav1alpha2.FsValidator
				expected bool
			}{
				{"nil receiver", nil, false},
				{"zero value", &redpandav1alpha2.FsValidator{}, false},
				{"enabled", &redpandav1alpha2.FsValidator{Enabled: ptr.To(true)}, true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.expected, tt.input.IsEnabled())
				})
			}
		})

		t.Run("GetExpectedFS", func(t *testing.T) {
			tests := []struct {
				name     string
				input    *redpandav1alpha2.FsValidator
				expected string
			}{
				{"nil receiver", nil, "xfs"},
				{"zero value", &redpandav1alpha2.FsValidator{}, "xfs"},
				{"custom", &redpandav1alpha2.FsValidator{ExpectedFS: ptr.To("ext4")}, "ext4"},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.expected, tt.input.GetExpectedFS())
				})
			}
		})
	})

	t.Run("SetDataDirOwnership", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.SetDataDirOwnership
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.SetDataDirOwnership{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("PoolFSValidator", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.PoolFSValidator
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.PoolFSValidator{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("PoolSetDataDirOwnership", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.PoolSetDataDirOwnership
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.PoolSetDataDirOwnership{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestExternalService(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.ExternalService
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value defaults to true", &redpandav1alpha2.ExternalService{}, true},
			{"explicitly disabled", &redpandav1alpha2.ExternalService{Enabled: ptr.To(false)}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestExternalDNS(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.ExternalDNS
			expected bool
		}{
			{"nil receiver", nil, false},
			{"enabled", &redpandav1alpha2.ExternalDNS{Enabled: ptr.To(true)}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})
}

func TestListener(t *testing.T) {
	t.Run("IsEnabled", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchListener
			expected bool
		}{
			{"nil receiver", nil, false},
			{"zero value defaults to true", &redpandav1alpha2.StretchListener{}, true},
			{"explicitly disabled", &redpandav1alpha2.StretchListener{Enabled: ptr.To(false)}, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.IsEnabled())
			})
		}
	})

	t.Run("GetPort", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.StretchListener
			expected int32
		}{
			{"nil receiver uses default", nil, 9999},
			{"zero value uses default", &redpandav1alpha2.StretchListener{}, 9999},
			{"custom port", &redpandav1alpha2.StretchListener{Port: ptr.To(int32(1234))}, 1234},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetPort(9999))
			})
		}
	})

	t.Run("ExternalListener", func(t *testing.T) {
		t.Run("IsEnabled", func(t *testing.T) {
			tests := []struct {
				name     string
				input    *redpandav1alpha2.StretchExternalListener
				expected bool
			}{
				{"nil receiver", nil, false},
				{"zero value defaults to true", &redpandav1alpha2.StretchExternalListener{}, true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.expected, tt.input.IsEnabled())
				})
			}
		})

		t.Run("GetPort", func(t *testing.T) {
			tests := []struct {
				name     string
				input    *redpandav1alpha2.StretchExternalListener
				expected int32
			}{
				{"nil receiver uses default", nil, 9999},
				{"custom port", &redpandav1alpha2.StretchExternalListener{StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(1234))}}, 1234},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.expected, tt.input.GetPort(9999))
				})
			}
		})

		t.Run("GetAdvertisedPort", func(t *testing.T) {
			tests := []struct {
				name     string
				input    *redpandav1alpha2.StretchExternalListener
				expected int32
			}{
				{"nil receiver uses default", nil, 9999},
				{"with advertised ports", &redpandav1alpha2.StretchExternalListener{AdvertisedPorts: []int32{31092}}, 31092},
				{"zero value uses default", &redpandav1alpha2.StretchExternalListener{}, 9999},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.expected, tt.input.GetAdvertisedPort(9999))
				})
			}
		})
	})
}

func TestPoolFSValidator(t *testing.T) {
	t.Run("GetExpectedFS", func(t *testing.T) {
		tests := []struct {
			name     string
			input    *redpandav1alpha2.PoolFSValidator
			expected string
		}{
			{"nil receiver", nil, "xfs"},
			{"zero value", &redpandav1alpha2.PoolFSValidator{}, "xfs"},
			{"custom", &redpandav1alpha2.PoolFSValidator{ExpectedFS: ptr.To("ext4")}, "ext4"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, tt.input.GetExpectedFS())
			})
		}
	})
}
