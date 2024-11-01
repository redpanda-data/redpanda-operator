// Copyright 2024 Redpanda Data, Inc.
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
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
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

	container, err := redpanda.Run(ctx, "docker.redpanda.com/redpandadata/redpanda:v23.2.8",
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

	err = redpandav1alpha2.AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	factory := internalclient.NewFactory(cfg, c)

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
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "superuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{adminAPI},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "superuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistrySpec{
				URLs: []string{schemaRegistry},
				SASL: &redpandav1alpha2.SchemaRegistrySASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "superuser",
						Key:  "password",
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
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "invalidsuperuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			Admin: &redpandav1alpha2.AdminAPISpec{
				URLs: []string{adminAPI},
				SASL: &redpandav1alpha2.AdminSASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "invalidsuperuser",
						Key:  "password",
					},
					Mechanism: redpandav1alpha2.SASLMechanismScramSHA256,
				},
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistrySpec{
				URLs: []string{schemaRegistry},
				SASL: &redpandav1alpha2.SchemaRegistrySASL{
					Username: "superuser",
					Password: redpandav1alpha2.SecretKeyRef{
						Name: "invalidsuperuser",
						Key:  "password",
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
		Reconciler:                 NewResourceController(c, factory, reconciler, "Test"),
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
		APIVersion: redpandav1alpha2.GroupVersion.Identifier(),
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

	require.NoError(t, apiextensionsv1.AddToScheme(scheme.Scheme))

	reconciler := &testReconciler{}
	redpandav1alpha2.SchemeBuilder.Register(&testObject{})
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

	err := environment.Factory.Client.Create(ctx, crd)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		key := client.ObjectKeyFromObject(crd)
		err = environment.Factory.Client.Get(ctx, key, crd)
		require.NoError(t, err)

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true
			}
		}

		return false
	}, 60*time.Second, 1*time.Second)

	doReconcileLifecycle := func(obj *testObject, delete bool) {
		require.NoError(t, environment.Factory.Client.Create(ctx, obj))

		key := client.ObjectKeyFromObject(obj)
		req := ctrl.Request{NamespacedName: key}

		_, err := environment.Reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		require.NoError(t, environment.Factory.Client.Get(ctx, key, obj))
		require.Contains(t, obj.Finalizers, FinalizerKey)

		if delete {
			require.NoError(t, environment.Factory.Client.Delete(ctx, obj))

			_, err := environment.Reconciler.Reconcile(ctx, req)
			require.NoError(t, err)

			require.Error(t, environment.Factory.Client.Get(ctx, key, obj))
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
