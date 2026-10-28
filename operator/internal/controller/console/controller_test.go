// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
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
									Password: &redpandav1alpha2.ValueSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "kafka-secret",
											},
											Key: "password",
										},
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
			name: "gateway-enabled",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-gateway",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ConsoleValues: redpandav1alpha2.ConsoleValues{
						Gateway: &redpandav1alpha2.GatewayConfig{
							Enabled:   ptr.To(true),
							Hostnames: []string{"console.example.com"},
							Path:      ptr.To("/"),
							Annotations: map[string]string{
								"example.com/team": "platform",
							},
							ParentRefs: []redpandav1alpha2.GatewayParentReference{
								{
									Name:      "my-gateway",
									Namespace: ptr.To("gateway-system"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "gateway-custom-path",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-gw-path",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ConsoleValues: redpandav1alpha2.ConsoleValues{
						Gateway: &redpandav1alpha2.GatewayConfig{
							Enabled:   ptr.To(true),
							Hostnames: []string{"console.example.com", "console.internal"},
							Path:      ptr.To("/console"),
							ParentRefs: []redpandav1alpha2.GatewayParentReference{
								{Name: "gw-a"},
								{Name: "gw-b", Namespace: ptr.To("other-ns")},
							},
						},
					},
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
		{
			name: "stretch-cluster-ref",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-stretch-cluster-ref",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ClusterSource: &redpandav1alpha2.ClusterSource{
						ClusterRef: &redpandav1alpha2.ClusterRef{
							Name: "test-stretch",
							Kind: ptr.To(redpandav1alpha2.StretchClusterRefKind),
						},
					},
				},
			},
		},
		{
			name: "pipelines-service-account",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-pipelines-sa",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ConsoleValues: redpandav1alpha2.ConsoleValues{
						Pipelines: &redpandav1alpha2.PipelinesConfig{
							Enabled:    ptr.To(true),
							ClusterRef: ptr.To("test-redpanda"),
							UserRef:    ptr.To("console-pipelines"),
						},
					},
				},
			},
		},
		{
			name: "pipelines-impersonate-user",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-pipelines-impersonate",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ConsoleValues: redpandav1alpha2.ConsoleValues{
						Pipelines: &redpandav1alpha2.PipelinesConfig{
							Enabled:         ptr.To(true),
							ImpersonateUser: ptr.To(true),
						},
					},
				},
			},
		},
		{
			// test-redpanda-pools is in the end state of a NodePool
			// migration: its own StatefulSet is drained and every broker
			// lives in a NodePool.
			name: "cluster-ref-node-pools",
			console: &redpandav1alpha2.Console{
				ObjectMeta: metav1.ObjectMeta{
					Name: "console-cluster-ref-node-pools",
				},
				Spec: redpandav1alpha2.ConsoleSpec{
					ClusterSource: &redpandav1alpha2.ClusterSource{
						ClusterRef: &redpandav1alpha2.ClusterRef{
							Name: "test-redpanda-pools",
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

	allCRDs := crds.All()
	allCRDs = append(allCRDs, loadGatewayAPICRDs(t)...)

	testutils.InstallCRDs(t, ctl, allCRDs...)

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

	require.NoError(t, ctl.Apply(t.Context(), &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-redpanda-pools",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(0),
				},
			},
		},
	}))

	require.NoError(t, ctl.Apply(t.Context(), &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pool",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To[int32](2),
			},
			ClusterRef: redpandav1alpha2.ClusterRef{
				Name: "test-redpanda-pools",
			},
		},
	}))

	require.NoError(t, ctl.Apply(t.Context(), &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stretch",
			Namespace: ns.Name,
		},
	}))

	// TLS / Listeners / ClusterDomain moved off StretchCluster.Spec onto each
	// RedpandaBrokerPool. The Console controller resolves a representative
	// pool to derive those fields for its connection config (it errors out
	// otherwise), so create a minimal pool referencing test-stretch.
	require.NoError(t, ctl.Apply(t.Context(), &redpandav1alpha2.RedpandaBrokerPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stretch-pool",
			Namespace: ns.Name,
		},
		Spec: redpandav1alpha2.BrokerPoolSpec{
			ClusterRef: redpandav1alpha2.ClusterRef{
				Name: "test-stretch",
				Kind: ptr.To(redpandav1alpha2.StretchClusterRefKind),
			},
		},
	}))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The goldens pin rng's JWT signing key, so each case needs its own.
			seed := fnv.New64a()
			_, _ = seed.Write([]byte(tc.name))

			consoleCtrl := Controller{
				Ctl:          ctl,
				UseNodePools: true,
				rng:          rand.New(rand.NewSource(int64(seed.Sum64()))), //nolint:gosec // test-only determinism, not cryptographic
			}

			// Create console CR with namespace set
			console := tc.console.DeepCopy()
			console.Namespace = ns.Name

			require.NoError(t, ctl.Apply(t.Context(), console))

			// Reconcile the console a few times to ensure determinism.
			for range 3 {
				req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: kube.AsKey(console)}, ClusterName: mcmanager.LocalCluster}

				_, err := consoleCtrl.Reconcile(t.Context(), req)
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
				req := mcreconcile.Request{Request: ctrl.Request{NamespacedName: kube.AsKey(console)}, ClusterName: mcmanager.LocalCluster}

				_, err := consoleCtrl.Reconcile(t.Context(), req)
				require.NoError(t, err)
			}

			require.NoError(t, <-doneCh)

			// Assert that all resources have been GC'd.
			require.Empty(t, scrapeControllerObjects(t, ctl, console))
		})
	}
}

// TestPipelinesAdmissionPolicy runs the pipelines RBAC and
// ValidatingAdmissionPolicy against a real API server, which compiles and
// enforces the CEL, acting as Console's ServiceAccount.
func TestPipelinesAdmissionPolicy(t *testing.T) {
	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: controller.UnifiedScheme,
		},
	})

	testutils.InstallCRDs(t, ctl, crds.All()...)

	ns, err := kube.Create(t.Context(), ctl, corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "pipelines"},
	})
	require.NoError(t, err)

	cr := &redpandav1alpha2.Console{
		ObjectMeta: metav1.ObjectMeta{Name: "rpcn", Namespace: ns.Name},
		Spec: redpandav1alpha2.ConsoleSpec{
			ConsoleValues: redpandav1alpha2.ConsoleValues{
				Pipelines: &redpandav1alpha2.PipelinesConfig{
					Enabled:    ptr.To(true),
					ClusterRef: ptr.To("redpanda"),
				},
			},
		},
	}
	// Pipelines on a cluster must not silently run as the bootstrap user.
	require.ErrorContains(t, ctl.Apply(t.Context(), cr), "clusterRef requires userRef")
	cr.Spec.Pipelines.UserRef = ptr.To("console-pipelines")
	require.NoError(t, ctl.Apply(t.Context(), cr))

	consoleCtrl := Controller{Ctl: ctl, rng: rand.New(rand.NewSource(0))} //nolint:gosec // test-only determinism, not cryptographic
	reconcileConsole := func() {
		_, err := consoleCtrl.Reconcile(t.Context(), mcreconcile.Request{Request: ctrl.Request{NamespacedName: kube.AsKey(cr)}, ClusterName: mcmanager.LocalCluster})
		require.NoError(t, err)
	}
	reconcileConsole()

	// Act as Console does: as its ServiceAccount, applying with force.
	cfg := rest.CopyConfig(ctl.RestConfig())
	cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:pipelines:rpcn-console"}
	asConsole, err := kube.FromRESTConfig(cfg, kube.Options{
		Options:      client.Options{Scheme: controller.UnifiedScheme},
		FieldManager: "redpanda-console",
	})
	require.NoError(t, err)

	// pipeline returns the fields Console writes, plus any that mutate adds.
	pipeline := func(name string, labels map[string]string, mutate func(*redpandav1alpha2.PipelineSpec)) *redpandav1alpha2.Pipeline {
		p := &redpandav1alpha2.Pipeline{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   ns.Name,
				Labels:      labels,
				Annotations: map[string]string{"console.redpanda.com/created-by": "alice"},
			},
			Spec: redpandav1alpha2.PipelineSpec{
				ConfigYAML:  "input:\n  generate:\n    mapping: root = \"hello\"\n",
				DisplayName: "Hello",
				Description: "Says hello",
				Tags:        map[string]string{"team": "data"},
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("256Mi")},
				},
				ClusterSource: &redpandav1alpha2.ClusterSource{
					ClusterRef: &redpandav1alpha2.ClusterRef{Name: "redpanda"},
				},
				UserRef: &redpandav1alpha2.PipelineUserRef{Name: "console-pipelines"},
			},
		}
		if mutate != nil {
			mutate(&p.Spec)
		}
		return p
	}
	managed := map[string]string{"console.redpanda.com/managed-by": "console"}

	// The policy and RoleBinding take effect asynchronously.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		err := asConsole.Apply(t.Context(), pipeline("probe", managed, func(s *redpandav1alpha2.PipelineSpec) { s.Image = ptr.To("evil") }), client.ForceOwnership, client.DryRunAll)
		assert.ErrorContains(c, err, "found image")
	}, 30*time.Second, 250*time.Millisecond)

	// Create, stop, and start a pipeline the way Console does. The CRD
	// defaults spec.replicas, which Console doesn't write.
	created := pipeline("created", managed, nil)
	require.NoError(t, asConsole.Apply(t.Context(), created, client.ForceOwnership))
	require.Equal(t, ptr.To[int32](1), created.Spec.Replicas)
	for _, paused := range []bool{true, false} {
		p := pipeline("created", managed, func(s *redpandav1alpha2.PipelineSpec) { s.Paused = paused })
		require.NoError(t, asConsole.Apply(t.Context(), p, client.ForceOwnership))
	}

	creates := []struct {
		name     string
		pipeline *redpandav1alpha2.Pipeline
		err      string
	}{
		{name: "safe-fields", pipeline: pipeline("safe", managed, func(s *redpandav1alpha2.PipelineSpec) { s.Paused = true; s.Replicas = ptr.To[int32](3) })},
		{name: "gpu", pipeline: pipeline("gpu", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.Resources.Limits["nvidia.com/gpu"] = resource.MustParse("1")
		}), err: "may only set cpu and memory"},
		{name: "unlabeled", pipeline: pipeline("unlabeled", nil, nil), err: "may only write Pipelines labeled"},
		{name: "image", pipeline: pipeline("image", managed, func(s *redpandav1alpha2.PipelineSpec) { s.Image = ptr.To("evil") }), err: "found image"},
		{name: "service-account", pipeline: pipeline("sa", managed, func(s *redpandav1alpha2.PipelineSpec) { s.ServiceAccountName = "admin" }), err: "found serviceAccountName"},
		{name: "host-path", pipeline: pipeline("hostpath", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.ExtraVolumes = []corev1.Volume{{Name: "root", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		}), err: "found extraVolumes"},
		{name: "value-sources", pipeline: pipeline("secrets", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.ValueSources = []redpandav1alpha2.NamedValueSource{{Name: "TOKEN", Source: redpandav1alpha2.ValueSource{Inline: ptr.To("x")}}}
		}), err: "found valueSources"},
		{name: "other-user-ref", pipeline: pipeline("user", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.UserRef.Name = "superuser"
		}), err: "may only set spec.userRef.name, to console-pipelines"},
		{name: "no-user-ref", pipeline: pipeline("bootstrap", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.UserRef = nil
		}), err: "must set spec.userRef.name to console-pipelines on Pipelines that set spec.cluster"},
		{name: "no-cluster", pipeline: pipeline("inline", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.ClusterSource, s.UserRef = nil, nil
		})},
		{name: "other-cluster", pipeline: pipeline("other", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.ClusterSource.ClusterRef.Name = "other"
		}), err: "may only set spec.cluster.clusterRef.name, to redpanda"},
		{name: "static-configuration", pipeline: pipeline("static", managed, func(s *redpandav1alpha2.PipelineSpec) {
			s.ClusterSource = &redpandav1alpha2.ClusterSource{StaticConfiguration: &redpandav1alpha2.StaticConfigurationSource{
				Kafka: &redpandav1alpha2.KafkaAPISpec{Brokers: []string{"elsewhere:9092"}},
			}}
			s.UserRef = nil
		}), err: "may only set spec.cluster.clusterRef.name, to redpanda"},
	}
	for _, tc := range creates {
		t.Run("create-"+tc.name, func(t *testing.T) {
			err := asConsole.Apply(t.Context(), tc.pipeline, client.ForceOwnership, client.DryRunAll)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}

	// Pipelines created outside Console are read-only to it, and the policy
	// doesn't apply to anyone else.
	gitops := pipeline("gitops", nil, func(s *redpandav1alpha2.PipelineSpec) { s.Image = ptr.To("custom") })
	require.NoError(t, ctl.Create(t.Context(), gitops))
	require.ErrorContains(t, asConsole.Apply(t.Context(), pipeline("gitops", managed, nil), client.ForceOwnership, client.DryRunAll), "may only update or delete Pipelines labeled")
	require.ErrorContains(t, asConsole.Delete(t.Context(), gitops), "may only update or delete Pipelines labeled")

	// Once an admin adds a field Console doesn't write, Console can't edit the
	// Pipeline, so a compromised Console can't reuse e.g. its valueSources.
	augmented := pipeline("augmented", managed, func(s *redpandav1alpha2.PipelineSpec) {
		s.ValueSources = []redpandav1alpha2.NamedValueSource{{Name: "TOKEN", Source: redpandav1alpha2.ValueSource{Inline: ptr.To("x")}}}
	})
	require.NoError(t, ctl.Create(t.Context(), augmented))
	require.ErrorContains(t, asConsole.Apply(t.Context(), pipeline("augmented", managed, func(s *redpandav1alpha2.PipelineSpec) {
		s.ConfigYAML = "input:\n  generate:\n    mapping: root = env(\"TOKEN\")\noutput:\n  stdout: {}\n"
	}), client.ForceOwnership, client.DryRunAll), "found valueSources")
	require.ErrorContains(t, asConsole.Apply(t.Context(), pipeline("created", managed, func(s *redpandav1alpha2.PipelineSpec) {
		s.ClusterSource.ClusterRef.Name = "elsewhere"
	}), client.ForceOwnership, client.DryRunAll), "may only set spec.cluster.clusterRef.name, to redpanda")
	require.ErrorContains(t, asConsole.Apply(t.Context(), pipeline("created", managed, func(s *redpandav1alpha2.PipelineSpec) {
		s.UserRef = nil
	}), client.ForceOwnership, client.DryRunAll), "must set spec.userRef.name to console-pipelines")

	require.NoError(t, asConsole.Delete(t.Context(), created))

	// allowBootstrapUser lets cluster-bound Pipelines omit userRef, which
	// stays pinned when set.
	cr.Spec.Pipelines.AllowBootstrapUser = ptr.To(true)
	require.NoError(t, ctl.Apply(t.Context(), cr))
	reconcileConsole()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, asConsole.Apply(t.Context(), pipeline("bootstrap", managed, func(s *redpandav1alpha2.PipelineSpec) { s.UserRef = nil }), client.ForceOwnership, client.DryRunAll))
	}, 30*time.Second, 250*time.Millisecond)
	require.NoError(t, asConsole.Apply(t.Context(), pipeline("pinned", managed, nil), client.ForceOwnership, client.DryRunAll))
	require.ErrorContains(t, asConsole.Apply(t.Context(), pipeline("other", managed, func(s *redpandav1alpha2.PipelineSpec) {
		s.UserRef.Name = "superuser"
	}), client.ForceOwnership, client.DryRunAll), "may only set spec.userRef.name, to console-pipelines")
}

// scrapeControllerObjects finds all objects created by the console controller using ownership labels
func scrapeControllerObjects(t *testing.T, ctl *kube.Ctl, c *redpandav1alpha2.Console) []kube.Object {
	// Get ownership labels used by the controller
	ownershipLabels := map[string]string{
		"app.kubernetes.io/name":       "console",
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/instance":   c.Name,
	}

	var objects []kube.Object
	for _, objType := range console.Types() {
		// skip ServiceMonitor here as it is optional and created only when monitoring.enabled is true
		if _, ok := objType.(*monitoringv1.ServiceMonitor); ok {
			continue
		}
		list, err := kube.ListFor(ctl.Scheme(), objType)
		require.NoError(t, err)

		err = ctl.List(
			t.Context(),
			c.Namespace,
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
	secret, err := kube.Get[corev1.Secret](t.Context(), ctl, kube.ObjectKey{Namespace: c.Namespace, Name: c.Name + "-jwt-secret"})
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

// loadGatewayAPICRDs loads Gateway API CRDs from the sigs.k8s.io/gateway-api
// module in the Go module cache. This is needed for envtest to support
// HTTPRoute resources.
func loadGatewayAPICRDs(t *testing.T) []*apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	// Resolve the gateway-api module directory from the module cache.
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "sigs.k8s.io/gateway-api")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "failed to resolve gateway-api module directory")

	crdDir := filepath.Join(strings.TrimSpace(out.String()), "config", "crd", "standard")

	scheme := runtime.NewScheme()
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	entries, err := os.ReadDir(crdDir)
	require.NoError(t, err)

	var result []*apiextensionsv1.CustomResourceDefinition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(crdDir, entry.Name()))
		require.NoError(t, err)

		objs, err := kube.DecodeYAML(data, scheme)
		if err != nil {
			// Skip non-CRD YAML files (e.g. ValidatingAdmissionPolicy).
			continue
		}

		for _, obj := range objs {
			if crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition); ok {
				result = append(result, crd)
			}
		}
	}

	require.NotEmpty(t, result, "no Gateway API CRDs found in %s", crdDir)
	return result
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

	// Clean deployment-specific dynamic fields
	if deploy, ok := obj.(*appsv1.Deployment); ok {
		for i, container := range deploy.Spec.Template.Spec.Containers {
			for j, env := range container.Env {
				if env.Name == "REDPANDA_METRICS_K8S_CLUSTER_ID" {
					deploy.Spec.Template.Spec.Containers[i].Env[j].Value = "00000000-0000-0000-0000-000000000000"
				}
				if env.Name == "REDPANDA_METRICS_K8S_VERSION" {
					deploy.Spec.Template.Spec.Containers[i].Env[j].Value = "v1.32.0"
				}
			}
		}
	}
}
