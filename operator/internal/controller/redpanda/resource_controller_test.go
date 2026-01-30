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
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
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
}

func InitializeResourceReconcilerTest[T any, U Resource[T]](t *testing.T, ctx context.Context, reconciler ResourceReconciler[U]) *ResourceReconcilerTestEnvironment[T, U] {
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

	container, err := redpanda.Run(ctx, os.Getenv("TEST_REDPANDA_REPO")+":"+os.Getenv("TEST_REDPANDA_VERSION"),
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableSASL(),
		redpanda.WithSuperusers("superuser"),
		redpanda.WithNewServiceAccount("superuser", "password"),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

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
	return client.Apply
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
