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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

// OIDCConfig configures a Dex OIDC provider for integration testing.
// The fields are interpolated into both the Dex server config and the
// Redpanda bootstrap config.
type OIDCConfig struct {
	// Email is the static user's email address used for authentication.
	Email string
	// Password is the static user's password (plaintext — will be bcrypt-hashed for Dex).
	Password string
	// Groups is the list of groups the static user belongs to.
	Groups []string
	// ClientID is the OAuth2 client ID (used by both Dex and Redpanda).
	ClientID string
	// ClientSecret is the OAuth2 client secret for the Dex static client.
	ClientSecret string
	// Scopes is the OAuth2 scopes to request (e.g., "openid groups email").
	Scopes string
}

func (c *OIDCConfig) dexConfigYAML() string {
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("bcrypt hash: %v", err))
	}

	groups := ""
	for _, g := range c.Groups {
		groups += fmt.Sprintf("\n      - %q", g)
	}
	return fmt.Sprintf(`issuer: http://dex:5556/dex
storage:
  type: memory
web:
  http: 0.0.0.0:5556
enablePasswordDB: true
oauth2:
  passwordConnector: local
staticClients:
  - id: %s
    secret: %s
    name: 'Test'
    redirectURIs:
      - 'http://localhost/callback'
staticPasswords:
  - email: %q
    hash: %q
    username: %q
    userID: "test-user-1"
    groups:%s
`, c.ClientID, c.ClientSecret, c.Email, string(hash), c.Email, groups)
}

// TestOption configures InitializeResourceReconcilerTest.
type TestOption func(*testOptions)

type testOptions struct {
	oidc *OIDCConfig
}

// WithOIDC enables OIDC integration testing. A Dex container and shared Docker
// network are created, and Redpanda is configured with OAUTHBEARER SASL backed
// by the Dex OIDC provider.
func WithOIDC(config OIDCConfig) TestOption {
	return func(o *testOptions) {
		o.oidc = &config
	}
}

type ResourceReconcilerTestEnvironment[T any, U Resource[T]] struct {
	Reconciler                 *ResourceController[T, U]
	Factory                    *internalclient.Factory
	ClusterSourceValid         *redpandav1alpha2.ClusterSource
	ClusterSourceNoSASL        *redpandav1alpha2.ClusterSource
	ClusterSourceBadPassword   *redpandav1alpha2.ClusterSource
	ClusterSourceInvalidRef    *redpandav1alpha2.ClusterSource
	SyncedCondition            metav1.Condition
	InvalidClusterRefCondition metav1.Condition
	ClientErrorCondition       metav1.Condition
	AdminURL                   string
	KafkaURL                   string
	SchemaRegistryURL          string

	oidcConfig *OIDCConfig
	dexURL     string
}

// FetchOIDCToken obtains a JWT from the Dex OIDC provider using the OAuth2
// password grant with the credentials from the OIDCConfig. Panics if WithOIDC
// was not used.
func (e *ResourceReconcilerTestEnvironment[T, U]) FetchOIDCToken(t *testing.T) string {
	t.Helper()
	require.NotNil(t, e.oidcConfig, "FetchOIDCToken requires WithOIDC")

	data := url.Values{
		"grant_type": {"password"},
		"username":   {e.oidcConfig.Email},
		"password":   {e.oidcConfig.Password},
		"scope":      {e.oidcConfig.Scopes},
		"client_id":  {e.oidcConfig.ClientID},
	}

	req, err := http.NewRequest(http.MethodPost, e.dexURL+"/dex/token", strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(e.oidcConfig.ClientID, e.oidcConfig.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "Dex token endpoint returned non-200 status")

	var tok struct {
		IDToken string `json:"id_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tok))
	require.NotEmpty(t, tok.IDToken, "Dex returned empty id_token")

	return tok.IDToken
}

func InitializeResourceReconcilerTest[T any, U Resource[T]](t *testing.T, ctx context.Context, reconciler ResourceReconciler[U], opts ...TestOption) *ResourceReconcilerTestEnvironment[T, U] {
	var options testOptions
	for _, opt := range opts {
		opt(&options)
	}

	server := &envtest.APIServer{}
	etcd := &envtest.Etcd{}

	testEnv := testutils.RedpandaTestEnv{
		Environment: envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: server,
				Etcd:      etcd,
			},
		},
	}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Cleanup(func() {
		_ = testEnv.Stop()
	})

	testImage := os.Getenv("TEST_REDPANDA_REPO") + ":" + os.Getenv("TEST_REDPANDA_VERSION")

	rpOpts := []testcontainers.ContainerCustomizer{
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("superuser"),
		redpanda.WithNewServiceAccount("superuser", "password"),
	}

	var dexURL string
	if options.oidc != nil {
		// Create shared Docker network for Dex <-> Redpanda communication.
		testNet, err := tcnetwork.New(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = testNet.Remove(context.Background()) })

		// Start Dex OIDC provider.
		dexContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "dexidp/dex:v2.45.0",
				ExposedPorts: []string{"5556/tcp"},
				Cmd:          []string{"dex", "serve", "/etc/dex/config.yaml"},
				Files: []testcontainers.ContainerFile{{
					Reader:            strings.NewReader(options.oidc.dexConfigYAML()),
					ContainerFilePath: "/etc/dex/config.yaml",
					FileMode:          0o644,
				}},
				WaitingFor: wait.ForHTTP("/dex/.well-known/openid-configuration").WithPort("5556/tcp"),
				Networks:   []string{testNet.Name},
				NetworkAliases: map[string][]string{
					testNet.Name: {"dex"},
				},
			},
			Started: true,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = dexContainer.Terminate(context.Background()) })
		dumpContainerLogsOnFailure(t, "Dex", dexContainer)

		dexHost, err := dexContainer.Host(ctx)
		require.NoError(t, err)
		dexPort, err := dexContainer.MappedPort(ctx, "5556/tcp")
		require.NoError(t, err)
		dexURL = fmt.Sprintf("http://%s:%s", dexHost, dexPort.Port())

		rpOpts = append(rpOpts,
			redpanda.WithBootstrapConfig("sasl_mechanisms", `["SCRAM", "OAUTHBEARER"]`),
			redpanda.WithBootstrapConfig("oidc_discovery_url", "http://dex:5556/dex/.well-known/openid-configuration"),
			redpanda.WithBootstrapConfig("oidc_token_audience", options.oidc.ClientID),
			tcnetwork.WithNetwork([]string{"redpanda"}, testNet),
		)
	}

	container, err := redpanda.Run(ctx, testImage, rpOpts...)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})
	dumpContainerLogsOnFailure(t, "Redpanda", container)

	kafkaAddress, err := container.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	adminAPI, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)

	schemaRegistry, err := container.SchemaRegistryAddress(ctx)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: controller.UnifiedScheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	mgr := SetupTestManager(t, ctx, cfg, c)
	factory := internalclient.NewFactory(mgr, nil)

	// ensure we have a secret which we can pull a password from
	err = c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "superuser",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"password": []byte("password"),
		},
	})
	require.NoError(t, err)

	err = c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalidsuperuser",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"password": []byte("invalid"),
		},
	})
	require.NoError(t, err)

	validClusterSource := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{kafkaAddress},
				SASL: &redpandav1alpha2.KafkaSASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "superuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{adminAPI},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "superuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistrySpec{
				URLs: []string{schemaRegistry},
				SASL: &redpandav1alpha2.SchemaRegistrySASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "superuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
		},
	}

	invalidAuthClusterSourceBadPassword := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{kafkaAddress},
				SASL: &redpandav1alpha2.KafkaSASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "invalidsuperuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{adminAPI},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "invalidsuperuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistrySpec{
				URLs: []string{schemaRegistry},
				SASL: &redpandav1alpha2.SchemaRegistrySASL{
					Username: "superuser",
					Password: &redpandav1alpha2.ValueSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "invalidsuperuser",
							},
							Key: "password",
						},
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
		},
	}

	invalidAuthClusterSourceNoSASL := &redpandav1alpha2.ClusterSource{
		StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
			Kafka: &redpandav1alpha2.KafkaAPISpec{
				Brokers: []string{kafkaAddress},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{adminAPI},
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistrySpec{
				URLs: []string{schemaRegistry},
			},
		},
	}

	invalidClusterRefSource := &redpandav1alpha2.ClusterSource{
		ClusterRef: &redpandav1alpha2.ClusterRef{
			Name: "nonexistent",
		},
	}

	syncedClusterRefCondition := redpandav1alpha2.ResourceSyncedCondition("test")

	invalidClusterRefCondition := redpandav1alpha2.ResourceNotSyncedCondition(
		redpandav1alpha2.ResourceConditionReasonClusterRefInvalid, errors.New("test"),
	)

	clientErrorCondition := redpandav1alpha2.ResourceNotSyncedCondition(
		redpandav1alpha2.ResourceConditionReasonTerminalClientError, errors.New("test"),
	)

	return &ResourceReconcilerTestEnvironment[T, U]{
		Reconciler:                 NewResourceController(mgr, factory, reconciler, "Test"),
		Factory:                    factory,
		ClusterSourceValid:         validClusterSource,
		ClusterSourceNoSASL:        invalidAuthClusterSourceNoSASL,
		ClusterSourceBadPassword:   invalidAuthClusterSourceBadPassword,
		ClusterSourceInvalidRef:    invalidClusterRefSource,
		SyncedCondition:            syncedClusterRefCondition,
		InvalidClusterRefCondition: invalidClusterRefCondition,
		ClientErrorCondition:       clientErrorCondition,
		AdminURL:                   adminAPI,
		KafkaURL:                   kafkaAddress,
		SchemaRegistryURL:          schemaRegistry,
		oidcConfig:                 options.oidc,
		dexURL:                     dexURL,
	}
}

type testReconciler struct {
	syncs   atomic.Int32
	deletes atomic.Int32
}

type testObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

func (t *testObject) DeepCopyObject() runtime.Object {
	return &testObject{
		TypeMeta:   t.TypeMeta,
		ObjectMeta: t.ObjectMeta,
	}
}

func (r *testReconciler) FinalizerPatch(request ResourceRequest[*testObject]) client.Patch {
	request.object.Finalizers = []string{FinalizerKey}
	request.object.TypeMeta = metav1.TypeMeta{
		Kind:       "testObject",
		APIVersion: redpandav1alpha2.GroupVersion.String(),
	}
	return client.Apply //nolint:staticcheck // TODO: migrate to client.Client.Apply()
}

func (r *testReconciler) SyncResource(ctx context.Context, request ResourceRequest[*testObject]) (client.Patch, error) {
	r.syncs.Add(1)
	return nil, nil
}

func (r *testReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*testObject]) error {
	r.deletes.Add(1)
	return nil
}

func TestResourceController(t *testing.T) { // nolint:funlen // These tests have clear subtests.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	require.NoError(t, apiextensionsv1.AddToScheme(controller.UnifiedScheme))
	controller.UnifiedScheme.AddKnownTypes(redpandav1alpha2.SchemeGroupVersion, &testObject{})

	reconciler := &testReconciler{}
	environment := InitializeResourceReconcilerTest(t, ctx, reconciler)

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testobjects." + redpandav1alpha2.GroupVersion.Group,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: redpandav1alpha2.GroupVersion.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "testobjects",
				Singular: "testobject",
				Kind:     "testObject",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: redpandav1alpha2.GroupVersion.Version,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
					},
				},
				Served:  true,
				Storage: true,
			}},
			Scope: apiextensionsv1.NamespaceScoped,
		},
	}

	k8sClient, err := environment.Factory.GetClient(ctx, mcmanager.LocalCluster)
	require.NoError(t, err)
	err = k8sClient.Create(ctx, crd)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		key := client.ObjectKeyFromObject(crd)
		err = k8sClient.Get(ctx, key, crd)
		require.NoError(t, err)

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}

		return false
	}, 60*time.Second, 1*time.Second)

	doReconcileLifecycle := func(obj *testObject, delete bool) {
		require.NoError(t, k8sClient.Create(ctx, obj))

		key := client.ObjectKeyFromObject(obj)
		req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: key}, ClusterName: mcmanager.LocalCluster}

		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, key, obj))
		require.Contains(t, obj.Finalizers, FinalizerKey)

		if delete {
			require.NoError(t, k8sClient.Delete(ctx, obj))

			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.Error(t, k8sClient.Get(ctx, key, obj))
		}
	}

	size := 100
	for i := 0; i < size; i++ {
		doReconcileLifecycle(&testObject{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-%d", i),
				Namespace: metav1.NamespaceDefault,
			},
		}, i%2 == 0)
	}

	require.Equal(t, int32(size/2), reconciler.deletes.Load())
	require.Equal(t, int32(size), reconciler.syncs.Load())
}

func TestIsNetworkDialError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "plain error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "net.OpError directly",
			err:      &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
			expected: true,
		},
		{
			name: "net.OpError wrapped once (franz-go style)",
			err: fmt.Errorf("unable to dial: %w",
				&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}),
			expected: true,
		},
		{
			name: "net.OpError wrapped twice (recordErrorEvent + franz-go)",
			err: fmt.Errorf("deleting topic (test) library error: %w",
				fmt.Errorf("unable to dial: %w",
					&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})),
			expected: true,
		},
		{
			name: "realistic connection refused error",
			err: fmt.Errorf("deleting topic (test-topic) library error: %w",
				fmt.Errorf("unable to dial: %w",
					&net.OpError{
						Op:   "dial",
						Net:  "tcp",
						Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9092},
						Err:  &net.OpError{Op: "connect", Err: errors.New("connection refused")},
					})),
			expected: true,
		},
		{
			name: "DNS resolution error",
			err: fmt.Errorf("unable to dial: %w",
				&net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: &net.DNSError{Err: "no such host", Name: "nonexistent.local"},
				}),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNetworkDialError(tt.err)
			assert.Equal(t, tt.expected, result, "error: %v", tt.err)
		})
	}
}

// dumpContainerLogsOnFailure registers a t.Cleanup that dumps the given
// container's logs when the test has failed. This is invaluable for debugging
// server-side issues (e.g., OIDC key fetch failures, auth errors).
func dumpContainerLogsOnFailure(t *testing.T, name string, container testcontainers.Container) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		// Use a fresh context since the test context is likely canceled by now.
		logCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		reader, err := container.Logs(logCtx)
		if err != nil {
			t.Logf("Failed to get %s container logs: %v", name, err)
			return
		}
		defer reader.Close()
		logs, err := io.ReadAll(reader)
		if err != nil {
			t.Logf("Failed to read %s container logs: %v", name, err)
			return
		}
		t.Logf("=== %s container logs ===\n%s\n=== end %s logs ===", name, string(logs), name)
	})
}
