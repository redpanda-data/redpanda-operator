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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	redpandachart "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestStretchClusterResourceClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Minute)
	defer cancel()

	server := &envtest.APIServer{}
	etcd := &envtest.Etcd{}

	environment := &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: server,
			Etcd:      etcd,
		},
	}
	config, err := environment.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Fatal(err)
		}
	})

	logger := testr.NewWithOptions(t, testr.Options{Verbosity: 6})

	manager, err := multicluster.NewSingleClusterManager(config, ctrl.Options{
		Scheme: controller.V2Scheme,
		Logger: logger,
		Metrics: metricsserver.Options{
			// disable metrics
			BindAddress: "0",
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: append(redpandachart.Types(), &redpandav1alpha2.StretchCluster{}, &corev1.Namespace{}),
			},
		},
	})
	require.NoError(t, err)

	go func() {
		if err := manager.Start(ctx); err != nil {
			panic(err)
		}
	}()

	casesArchive, err := txtar.ParseFile("testdata/stretch-cluster-cases.txtar")
	require.NoError(t, err)

	goldenPools := testutil.NewTxTar(t, "testdata/stretch-cluster-cases.pools.golden.txtar")
	goldenResources := testutil.NewTxTar(t, "testdata/stretch-cluster-cases.resources.golden.txtar")
	goldenValues := testutil.NewTxTar(t, "testdata/stretch-cluster-cases.values.golden.txtar")

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

	resourceClient := NewMulticlusterResourceClient(manager, StretchClusterResourceManagers(redpandaImage, sidecarImage, cloudSecrets))

	require.EqualValues(t, redpandachart.Types(), resourceClient.simpleResourceRenderer.WatchedResourceTypes())

	decoder := serializer.NewCodecFactory(controller.MulticlusterScheme).UniversalDecoder(redpandav1alpha2.SchemeGroupVersion)

	decode := func(t *testing.T, manifests []byte) (*redpandav1alpha2.StretchCluster, []*redpandav1alpha2.NodePool) {
		var cluster *redpandav1alpha2.StretchCluster
		pools := []*redpandav1alpha2.NodePool{}
		for _, obj := range strings.Split(string(manifests), "---") {
			obj := strings.Trim(obj, " ")
			if obj == "" {
				continue
			}
			decoded, gvk, err := decoder.Decode([]byte(obj), nil, nil)
			require.NoError(t, err)
			switch o := decoded.(type) {
			case *redpandav1alpha2.StretchCluster:
				require.Nil(t, cluster)
				cluster = o
			case *redpandav1alpha2.NodePool:
				pools = append(pools, o)
			default:
				t.Fatalf("invalid type found in manifest: %s", gvk.String())
			}
		}

		if cluster == nil {
			cluster = &redpandav1alpha2.StretchCluster{}
		}

		return cluster, pools
	}

	for _, file := range casesArchive.Files {
		t.Run(file.Name, func(t *testing.T) {
			t.Parallel()

			stretchCluster, pools := decode(t, file.Data)

			// override name and namespace to make it unique
			stretchCluster.Name = file.Name
			stretchCluster.Namespace = file.Name
			cluster := NewStretchClusterWithPools(stretchCluster, []string{""}, pools...)

			ownerLabels := resourceClient.ownershipResolver.GetOwnerLabels(cluster)

			require.NotEmpty(t, ownerLabels)

			state, err := resourceClient.nodePoolRenderer.(*StretchNodePoolRenderer).convertToRender(ctx, cluster, "")
			require.NoError(t, err)

			yamlBytes, err := yaml.Marshal(map[string]any{
				"values": state.Values,
				"pools":  state.Pools,
			})
			require.NoError(t, err)
			goldenValues.AssertGolden(t, testutil.YAML, file.Name, yamlBytes)

			sets, err := resourceClient.nodePoolRenderer.Render(ctx, cluster, "")
			require.NoError(t, err)

			assertOwnership := func(object client.Object) {
				labels := object.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				// copied from the resource client syncer
				labels["cluster.redpanda.com/owner"] = cluster.Name
				labels["cluster.redpanda.com/namespace"] = cluster.Namespace
				object.SetLabels(labels)

				require.Equal(t, client.ObjectKeyFromObject(cluster), *resourceClient.ownershipResolver.OwnerForObject(object))
			}

			for _, pool := range sets {
				assertOwnership(pool)
				require.True(t, resourceClient.nodePoolRenderer.IsNodePool(pool))
			}

			poolBytes, err := yaml.Marshal(sets)
			require.NoError(t, err)
			goldenPools.AssertGolden(t, testutil.YAML, file.Name, poolBytes)

			resources, err := resourceClient.simpleResourceRenderer.Render(ctx, cluster, "")
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
