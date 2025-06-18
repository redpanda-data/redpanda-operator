package resources_unit_tests

import (
	"context"
	"testing"

	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRenderIngressPandaProxy(t *testing.T) {
	tests := []struct {
		name                 string
		cluster              *vectorizedv1alpha1.Cluster
		tlsSecretName        string
		clusterIssuer        string
		shouldCreateIngress  bool
		expectedHost         string
		expectedServiceName  string
		expectedServicePort  string
		expectedAnnotations  map[string]string
	}{
		{
			name:          "PandaProxy ingress disabled (from golden template)",
			tlsSecretName: "rp-d0j2drs1952b2k2orgm0-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rp-d0j2drs1952b2k2orgm0",
					Namespace: "redpanda",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								Port: 30082,
								External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
									ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
										Enabled:   true,
										Subdomain: "d0j2drs1952b2k2orgm0.fmc.ppd.cloud.redpanda.com",
									},
									Ingress: &vectorizedv1alpha1.IngressConfig{
										Enabled: &[]bool{false}[0],
									},
								},
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled: true,
								},
							},
						},
					},
				},
			},
			shouldCreateIngress: false,
		},
		{
			name:          "PandaProxy ingress enabled",
			tlsSecretName: "test-cluster-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								Port: 30082,
								External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
									ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
										Enabled:   true,
										Subdomain: "test.example.com",
									},
								},
								TLS: vectorizedv1alpha1.PandaproxyAPITLS{
									Enabled: true,
									IssuerRef: &cmmetav1.ObjectReference{
										Name: "letsencrypt-dns-prod",
									},
								},
							},
						},
					},
				},
			},
			shouldCreateIngress: true,
			expectedHost:        "test.example.com",
			expectedServiceName: "test-cluster-cluster",
			expectedServicePort: "proxy-external",
			expectedAnnotations: map[string]string{
				"nginx.ingress.kubernetes.io/ssl-passthrough": "true",
			},
		},
		{
			name:          "PandaProxy with custom endpoint and user annotations",
			tlsSecretName: "test-cluster-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			cluster: &vectorizedv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ClusterSpec{
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						PandaproxyAPI: []vectorizedv1alpha1.PandaproxyAPI{
							{
								Port: 30082,
								External: vectorizedv1alpha1.PandaproxyExternalConnectivityConfig{
									ExternalConnectivityConfig: vectorizedv1alpha1.ExternalConnectivityConfig{
										Enabled:   true,
										Subdomain: "test.example.com",
									},
									Ingress: &vectorizedv1alpha1.IngressConfig{
										Enabled:  &[]bool{true}[0],
										Endpoint: "proxy", // User-configured endpoint
										Annotations: map[string]string{
											"custom.annotation":                        "user-value",
											"nginx.ingress.kubernetes.io/ssl-passthrough": "false", // User overrides default
										},
									},
								},
							},
						},
					},
				},
			},
			shouldCreateIngress: true,
			expectedHost:        "proxy.test.example.com",
			expectedServiceName: "test-cluster-cluster",
			expectedServicePort: "proxy-external",
			expectedAnnotations: map[string]string{
				"custom.annotation":                        "user-value",
				"nginx.ingress.kubernetes.io/ssl-passthrough": "false", // User overrides default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Extract subdomain from cluster like the lifecycle does
			var subdomain string
			if proxyAPIExternal := tt.cluster.FirstPandaproxyAPIExternal(); proxyAPIExternal != nil {
				subdomain = proxyAPIExternal.External.Subdomain
			}

			ingress, err := resources.RenderIngress(ctx, tt.cluster, subdomain, tt.tlsSecretName, tt.clusterIssuer)
			require.NoError(t, err)

			if !tt.shouldCreateIngress {
				assert.Nil(t, ingress, "Expected no ingress to be created")
				return
			}

			require.NotNil(t, ingress, "Expected ingress to be created")
			ing, ok := ingress.(*networkingv1.Ingress)
			require.True(t, ok, "Expected *networkingv1.Ingress type")

			// Verify basic properties
			assert.Equal(t, tt.cluster.Name, ing.Name)
			assert.Equal(t, tt.cluster.Namespace, ing.Namespace)
			assert.Equal(t, "nginx", *ing.Spec.IngressClassName)
			require.Len(t, ing.Spec.Rules, 1)

			rule := ing.Spec.Rules[0]
			assert.Equal(t, tt.expectedHost, rule.Host)

			path := rule.HTTP.Paths[0]
			assert.Equal(t, tt.expectedServiceName, path.Backend.Service.Name)
			assert.Equal(t, tt.expectedServicePort, path.Backend.Service.Port.Name)

			// Verify expected annotations
			for k, v := range tt.expectedAnnotations {
				assert.Equal(t, v, ing.Annotations[k], "Expected annotation %s to be %s", k, v)
			}

			// Verify TLS configuration
			assert.Equal(t, tt.clusterIssuer, ing.Annotations["cert-manager.io/cluster-issuer"])
			assert.Equal(t, "true", ing.Annotations["nginx.ingress.kubernetes.io/force-ssl-redirect"])
			require.Len(t, ing.Spec.TLS, 1)
			assert.Equal(t, tt.tlsSecretName, ing.Spec.TLS[0].SecretName)
		})
	}
}

func TestRenderIngressConsole(t *testing.T) {
	tests := []struct {
		name                 string
		console              *vectorizedv1alpha1.Console
		subdomain            string
		tlsSecretName        string
		clusterIssuer        string
		shouldCreateIngress  bool
		expectedHost         string
		expectedServiceName  string
		expectedAnnotations  map[string]string
	}{
		{
			name:          "Console ingress enabled",
			subdomain:     "test.example.com",
			tlsSecretName: "test-console-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			console: &vectorizedv1alpha1.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-console",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ConsoleSpec{
					Ingress: &vectorizedv1alpha1.IngressConfig{
						Enabled: &[]bool{true}[0],
					},
				},
			},
			shouldCreateIngress: true,
			expectedHost:        "console.test.example.com",
			expectedServiceName: "test-console",
			expectedAnnotations: map[string]string{
				"nginx.ingress.kubernetes.io/server-snippet": "if ($request_uri ~* ^/(debug|admin)) {\n\treturn 403;\n\t}",
			},
		},
		{
			name:          "Console ingress disabled",
			subdomain:     "test.example.com",
			tlsSecretName: "test-console-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			console: &vectorizedv1alpha1.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-console",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ConsoleSpec{
					Ingress: &vectorizedv1alpha1.IngressConfig{
						Enabled: &[]bool{false}[0],
					},
				},
			},
			shouldCreateIngress: false,
		},
		{
			name:          "Console with custom endpoint and user annotations",
			subdomain:     "test.example.com",
			tlsSecretName: "test-console-redpanda",
			clusterIssuer: "letsencrypt-dns-prod",
			console: &vectorizedv1alpha1.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-console",
					Namespace: "default",
				},
				Spec: vectorizedv1alpha1.ConsoleSpec{
					Ingress: &vectorizedv1alpha1.IngressConfig{
						Enabled:  &[]bool{true}[0],
						Endpoint: "admin", // User-configured endpoint
						Annotations: map[string]string{
							"custom.annotation": "user-value",
							"nginx.ingress.kubernetes.io/server-snippet": "custom snippet", // User overrides default
						},
					},
				},
			},
			shouldCreateIngress: true,
			expectedHost:        "admin.test.example.com",
			expectedServiceName: "test-console",
			expectedAnnotations: map[string]string{
				"custom.annotation": "user-value",
				"nginx.ingress.kubernetes.io/server-snippet": "custom snippet", // User overrides default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			ingress, err := resources.RenderIngress(ctx, tt.console, tt.subdomain, tt.tlsSecretName, tt.clusterIssuer)
			require.NoError(t, err)

			if !tt.shouldCreateIngress {
				assert.Nil(t, ingress, "Expected no ingress to be created")
				return
			}

			require.NotNil(t, ingress, "Expected ingress to be created")
			ing, ok := ingress.(*networkingv1.Ingress)
			require.True(t, ok, "Expected *networkingv1.Ingress type")

			// Verify basic properties
			assert.Equal(t, tt.console.Name, ing.Name)
			assert.Equal(t, tt.console.Namespace, ing.Namespace)
			assert.Equal(t, "nginx", *ing.Spec.IngressClassName)
			require.Len(t, ing.Spec.Rules, 1)

			rule := ing.Spec.Rules[0]
			assert.Equal(t, tt.expectedHost, rule.Host)

			path := rule.HTTP.Paths[0]
			assert.Equal(t, tt.expectedServiceName, path.Backend.Service.Name)
			assert.Equal(t, "http", path.Backend.Service.Port.Name)

			// Verify expected annotations
			for k, v := range tt.expectedAnnotations {
				assert.Equal(t, v, ing.Annotations[k], "Expected annotation %s to be %s", k, v)
			}

			// Verify TLS configuration
			assert.Equal(t, tt.clusterIssuer, ing.Annotations["cert-manager.io/cluster-issuer"])
			assert.Equal(t, "true", ing.Annotations["nginx.ingress.kubernetes.io/force-ssl-redirect"])
			require.Len(t, ing.Spec.TLS, 1)
			assert.Equal(t, tt.tlsSecretName, ing.Spec.TLS[0].SecretName)
		})
	}
}

