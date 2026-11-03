// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestV2ResourceClient(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Minute)
	defer cancel()

	manager := startV2Manager(ctx, t)

	casesArchive, err := txtar.ParseFile("testdata/redpanda-cases.txtar")
	require.NoError(t, err)

	goldenPools := testutil.NewTxTar(t, "testdata/redpanda-cases.pools.golden.txtar")
	goldenResources := testutil.NewTxTar(t, "testdata/redpanda-cases.resources.golden.txtar")
	goldenValues := testutil.NewTxTar(t, "testdata/redpanda-cases.values.golden.txtar")

	cloudSecrets := CloudSecretsFlags{
		CloudSecretsEnabled: false,
	}
	redpandaImage := Image{
		Repository: os.Getenv("TEST_REDPANDA_REPO"),
		Tag:        os.Getenv("TEST_REDPANDA_VERSION"),
	}
	sidecarImage := Image{
		Repository: "localhost/redpanda-operator",
		Tag:        "dev",
	}

	resourceClient := NewResourceClient(manager, V2ResourceManagers(redpandaImage, sidecarImage, cloudSecrets), false)

	require.EqualValues(t, redpandachart.Types(), resourceClient.simpleResourceRenderer.WatchedResourceTypes())

	decoder := serializer.NewCodecFactory(controller.V2Scheme).UniversalDecoder(redpandav1alpha2.SchemeGroupVersion)

	decode := func(t *testing.T, manifests []byte) (*redpandav1alpha2.Redpanda, []*redpandav1alpha2.NodePool) {
		var cluster *redpandav1alpha2.Redpanda
		pools := []*redpandav1alpha2.NodePool{}
		for _, obj := range strings.Split(string(manifests), "---") {
			obj := strings.Trim(obj, " ")
			if obj == "" {
				continue
			}
			decoded, gvk, err := decoder.Decode([]byte(obj), nil, nil)
			require.NoError(t, err)
			switch o := decoded.(type) {
			case *redpandav1alpha2.Redpanda:
				require.Nil(t, cluster)
				cluster = o
			case *redpandav1alpha2.NodePool:
				pools = append(pools, o)
			default:
				t.Fatalf("invalid type found in manifest: %s", gvk.String())
			}
		}

		if cluster == nil {
			cluster = &redpandav1alpha2.Redpanda{}
		}

		return cluster, pools
	}

	for _, file := range casesArchive.Files {
		t.Run(file.Name, func(t *testing.T) {
			t.Parallel()

			redpanda, pools := decode(t, file.Data)

			// override name and namespace to make it unique
			redpanda.Name = file.Name
			redpanda.Namespace = file.Name
			cluster := NewClusterWithPools(redpanda, pools...)

			ownerLabels := resourceClient.ownershipResolver.GetOwnerLabels(cluster)

			state, err := resourceClient.nodePoolRenderer.(*V2NodePoolRenderer).convertToRender(cluster)
			require.NoError(t, err)

			yamlBytes, err := yaml.Marshal(map[string]any{
				"values": state.Values,
				"pools":  state.Pools,
			})
			require.NoError(t, err)
			goldenValues.AssertGolden(t, testutil.YAML, file.Name, yamlBytes)

			sets, err := resourceClient.nodePoolRenderer.Render(ctx, cluster, mcmanager.LocalCluster)
			require.NoError(t, err)

			assertOwnership := func(object client.Object) {
				labels := object.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				// copied from the original redpanda_controller normalization code
				labels["helm.toolkit.fluxcd.io/name"] = cluster.Name
				labels["helm.toolkit.fluxcd.io/namespace"] = cluster.Namespace
				object.SetLabels(labels)

				for label, value := range ownerLabels {
					objectLabel, ok := labels[label]
					require.True(t, ok, "no label %q found on %q: %s", label, object.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(object).String())
					require.Equal(t, objectLabel, value)
				}

				require.Equal(t, client.ObjectKeyFromObject(cluster), *resourceClient.ownershipResolver.OwnerForObject(object))

				for label, value := range resourceClient.ownershipResolver.AddLabels(cluster) {
					labels[label] = value
				}
				object.SetLabels(labels)
			}

			for _, pool := range sets {
				assertOwnership(pool)
				require.True(t, resourceClient.nodePoolRenderer.IsNodePool(pool))
				for _, container := range pool.Spec.Template.Spec.Containers {
					// make these deterministic for the test
					for i, env := range container.Env {
						if env.Name == "REDPANDA_METRICS_K8S_CLUSTER_ID" {
							env.Value = "00000000-0000-0000-0000-000000000000"
						}
						if env.Name == "REDPANDA_METRICS_K8S_VERSION" {
							env.Value = "v1.32.0"
						}
						container.Env[i] = env
					}
				}
			}

			poolBytes, err := yaml.Marshal(sets)
			require.NoError(t, err)
			goldenPools.AssertGolden(t, testutil.YAML, file.Name, poolBytes)

			resources, err := resourceClient.simpleResourceRenderer.Render(ctx, cluster, mcmanager.LocalCluster)
			require.NoError(t, err)

			for _, resource := range resources {
				assertOwnership(resource)
				require.False(t, resourceClient.nodePoolRenderer.IsNodePool(resource))
			}

			resourceBytes, err := yaml.Marshal(resources)
			require.NoError(t, err)

			goldenResources.AssertGolden(t, testutil.YAML, file.Name, resourceBytes)
		})
	}
}

// TestV2OperatorUpgradeDoesNotRollBrokers applies the operator's StatefulSets
// to an apiserver, upgrades the operator version and applies them again. A
// StatefulSet spec change is what makes the StatefulSet controller cut a new
// ControllerRevision, after which PodsToRoll restarts every broker on the old
// one; the apiserver bumps metadata.generation on exactly those changes. On
// Redpanda versions that read telemetry from the metrics ConfigMap, the
// upgrade must reach the ConfigMap and leave the generation alone.
//
// Not parallel: it sets the operator version global that rendering reads.
func TestV2OperatorUpgradeDoesNotRollBrokers(t *testing.T) {
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Minute)
	defer cancel()

	manager := startV2Manager(ctx, t)

	origVersion := version.Version
	t.Cleanup(func() { version.Version = origVersion })

	for _, tc := range []struct {
		redpandaTag string
		wantRoll    bool
	}{
		{redpandaTag: "v26.2.6", wantRoll: true},
		{redpandaTag: "v26.3.1", wantRoll: false},
	} {
		t.Run(tc.redpandaTag, func(t *testing.T) {
			resourceClient := NewResourceClient(manager, V2ResourceManagers(
				Image{Repository: "redpandadata/redpanda", Tag: tc.redpandaTag},
				Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
				CloudSecretsFlags{},
			), false)

			ctl, err := resourceClient.Ctl(ctx, mcmanager.LocalCluster)
			require.NoError(t, err)

			namespace := "upgrade-" + strings.ReplaceAll(tc.redpandaTag, ".", "-")
			require.NoError(t, ctl.Apply(ctx, &corev1.Namespace{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{Name: namespace},
			}, client.ForceOwnership))

			cluster := NewClusterWithPools(&redpandav1alpha2.Redpanda{
				TypeMeta:   metav1.TypeMeta{APIVersion: redpandav1alpha2.GroupVersion.String(), Kind: "Redpanda"},
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: namespace, UID: types.UID(namespace)},
			})

			// upgrade renders and applies everything as operatorVersion and
			// returns each StatefulSet's generation and the metrics ConfigMap.
			upgrade := func(operatorVersion string) (map[string]int64, *corev1.ConfigMap) {
				version.Version = operatorVersion

				sets, err := resourceClient.nodePoolRenderer.Render(ctx, cluster, mcmanager.LocalCluster)
				require.NoError(t, err)
				generations := map[string]int64{}
				for _, set := range sets {
					require.NoError(t, resourceClient.PatchPoolSet(ctx, cluster, newMulticlusterStatefulSet(set, mcmanager.LocalCluster, mcmanager.LocalCluster)))

					var live appsv1.StatefulSet
					require.NoError(t, ctl.Get(ctx, client.ObjectKeyFromObject(set), &live))
					generations[live.Name] = live.Generation
				}

				resources, err := resourceClient.simpleResourceRenderer.Render(ctx, cluster, mcmanager.LocalCluster)
				require.NoError(t, err)
				for _, resource := range resources {
					if cm, ok := resource.(*corev1.ConfigMap); ok && cm.Name == redpanda.MetricsConfigMapName("rp") {
						return generations, cm
					}
				}
				return generations, nil
			}

			before, _ := upgrade("v26.2.1")
			after, cm := upgrade("v26.2.2")

			if tc.wantRoll {
				require.NotEqual(t, before, after, "StatefulSet generations")
				require.Nil(t, cm)
				return
			}
			require.Equal(t, before, after, "StatefulSet generations")
			require.NotNil(t, cm)
			require.Equal(t, "v26.2.2", cm.Data["REDPANDA_METRICS_K8S_CHART_VERSION"])
		})
	}
}

// startV2Manager starts an apiserver and a manager with the V2 scheme.
func startV2Manager(ctx context.Context, t *testing.T) multicluster.Manager {
	environment := &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: &envtest.APIServer{},
			Etcd:      &envtest.Etcd{},
		},
	}
	config, err := environment.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Fatal(err)
		}
	})

	manager, err := multicluster.NewSingleClusterManager(config, ctrl.Options{
		Scheme: controller.V2Scheme,
		Logger: testr.NewWithOptions(t, testr.Options{Verbosity: 6}),
		Metrics: metricsserver.Options{
			// disable metrics
			BindAddress: "0",
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: append(redpandachart.Types(), &redpandav1alpha2.Redpanda{}, &corev1.Namespace{}),
			},
		},
	})
	require.NoError(t, err)

	go func() {
		if err := manager.Start(ctx); err != nil {
			panic(err)
		}
	}()
	return manager
}
