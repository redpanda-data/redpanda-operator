// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/kube/kubetest"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestController(t *testing.T) {
	golden := testutil.NewTxTar(t, "testdata/controller-tests.golden.txtar")

	testCases := []struct {
		name    string
		console *redpandav1alpha2.Console
	}{
		{
			name: "static-config",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-static",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ClusterSource: &redpandav1alpha2.ClusterSource{
						StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
							Kafka: &redpandav1alpha2.KafkaAPISpec{
								Brokers: []string{"kafka-broker:9092"},
								SASL: &redpandav1alpha2.KafkaSASL{
									Username:  "testuser",
									Mechanism: redpandav1alpha2.SASLMechanismPlain,
									Password: redpandav1alpha2.SecretKeyRef{
										Name: "kafka-secret",
										Key:  "password",
									},
								},
							},
							Admin: &redpandav1alpha2.AdminAPISpec{
								URLs: []string{"http://admin-api:9644"},
							},
						},
					},
				},
			},
		},
		{
			name: "cluster-ref",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-cluster-ref",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ClusterSource: &redpandav1alpha2.ClusterSource{
						ClusterRef: &redpandav1alpha2.ClusterRef{
							Name: "test-redpanda",
						},
					},
				},
			},
		},
		{
			name: "no-cluster-source",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-no-cluster",
				},
			},
		},
		{
			name: "jwt-set",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-jwt-set",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ConsoleValues: redpandav1alpha2.ConsoleValues{
						Secret: redpandav1alpha2.SecretConfig{
							Authentication: &redpandav1alpha2.AuthenticationSecrets{
								JWTSigningKey: ptr.To("some-secret-key"),
							},
						},
					},
				},
			},
		},
	}

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: controller.UnifiedScheme,
		},
	})

	require.NoError(t, kube.ApplyAllAndWait(t.Context(), ctl, func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
		if err != nil {
			return false, err
		}

		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established {
				return cond.Status == apiextensionsv1.ConditionTrue, nil
			}
		}

		return false, nil
	}, crds.All()...))

	// Create namespace
	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
		},
	})
	require.NoError(t, err)

	require.NoError(t, ctl.Apply(t.Context(), &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-redpanda",
			Namespace: ns.Name,
		},
	}))

	consoleCtrl := Controller{
		Ctl: ctl,
		rng: rand.New(rand.NewSource(0)),
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create console CR with namespace set
			console := tc.console.DeepCopy()
			console.Namespace = ns.Name

			require.NoError(t, ctl.Apply(t.Context(), console))

			// Reconcile the console a few times to ensure determinism.
			for range 3 {
				_, err = consoleCtrl.Reconcile(t.Context(), ctrl.Request{NamespacedName: kube.AsKey(console)})
				require.NoError(t, err)

				// Get updated console status
				require.NoError(t, ctl.Get(t.Context(), kube.AsKey(console), console))

				// Assert that ObservedGeneration has been advanced.
				require.Equal(t, console.Generation, console.Status.ObservedGeneration)
				// And that we've had a finalizer added.
				require.NotEmpty(t, console.Finalizers)

				// Scrape all objects created by the controller using ownership labels
				objects := scrapeControllerObjects(t, ctl, console)

				manifest, err := yaml.Marshal(objects)
				require.NoError(t, err)

				golden.AssertGolden(t, testutil.YAML, tc.name, manifest)
			}

			// Delete the CR to verify GC'ing works as expected. Use a bounded
			// context to prevent this test from hanging if something goes
			// wrong.
			doneCh := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()

				doneCh <- ctl.DeleteAndWait(ctx, console)

				close(doneCh)
			}()

			// Reconcile the deletion a few times.
			for range 3 {
				_, err = consoleCtrl.Reconcile(t.Context(), ctrl.Request{NamespacedName: kube.AsKey(console)})
				require.NoError(t, err)
			}

			require.NoError(t, <-doneCh)

			// Assert that all resources have been GC'd.
			require.Empty(t, scrapeControllerObjects(t, ctl, console))
		})
	}
}

// scrapeControllerObjects finds all objects created by the console controller using ownership labels
func scrapeControllerObjects(t *testing.T, ctl *kube.Ctl, console *redpandav1alpha2.Console) []kube.Object {
	// Get ownership labels used by the controller
	ownershipLabels := map[string]string{
		"app.kubernetes.io/name":       "console",
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/instance":   console.Name,
	}

	var objects []kube.Object
	for _, objType := range consolechart.Types() {
		list, err := kube.ListFor(ctl.Scheme(), objType)
		require.NoError(t, err)

		err = ctl.List(
			t.Context(),
			console.Namespace,
			list,
			client.MatchingLabels(ownershipLabels),
		)
		require.NoError(t, err)

		objs, err := kube.Items[kube.Object](list)
		require.NoError(t, err)

		for _, obj := range objs {
			cleanObjectForGolden(ctl.Scheme(), obj)
			objects = append(objects, obj)
		}
	}

	// If a JWT secret has been created, pull that as well.
	secret, err := kube.Get[corev1.Secret](t.Context(), ctl, kube.ObjectKey{Namespace: console.Namespace, Name: console.Name + "-jwt-secret"})
	if err == nil {
		cleanObjectForGolden(ctl.Scheme(), secret)
		objects = append(objects, secret)
	}

	slices.SortFunc(objects, func(i, j client.Object) int {
		iKey := fmt.Sprintf("%T%s%s", i, i.GetNamespace(), i.GetName())
		jKey := fmt.Sprintf("%T%s%s", j, j.GetNamespace(), j.GetName())
		return strings.Compare(iKey, jKey)
	})

	return objects
}

// cleanObjectForGolden removes dynamic fields that change between test runs
func cleanObjectForGolden(scheme *runtime.Scheme, obj client.Object) {
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		panic(err) // unlikely to occur.
	}
	obj.GetObjectKind().SetGroupVersionKind(gvks[0])

	// Clear dynamic metadata fields
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetFinalizers(nil)
	obj.SetGeneration(0)
	obj.SetManagedFields(nil)
	obj.SetOwnerReferences(nil)
	obj.SetResourceVersion("")
	obj.SetUID("")

	// Clean service-specific dynamic fields
	if svc, ok := obj.(*corev1.Service); ok {
		svc.Spec.ClusterIP = ""
		svc.Spec.ClusterIPs = nil
	}
}
