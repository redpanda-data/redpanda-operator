// Copyright 2025 Redpanda Data, Inc.
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
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var parentCtx = context.Background()

func setupContext() (context.Context, context.CancelFunc) {
	log.SetLogger(logr.Discard())

	return context.WithTimeout(parentCtx, 1*time.Minute)
}

func (tt *clientTest) setupManager(ctx context.Context, t *testing.T) ctrl.Manager {
	t.Helper()

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

	runtimeScheme := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(runtimeScheme))
	require.NoError(t, apiextensionsv1.AddToScheme(runtimeScheme))
	require.NoError(t, AddToScheme(runtimeScheme))

	opts := []zap.Opts{
		zap.UseDevMode(true), zap.Level(zapcore.DebugLevel),
	}

	if !testing.Verbose() {
		opts = append(opts, zap.WriteTo(io.Discard))
	}

	logger := zap.New(opts...)

	manager, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: runtimeScheme,
		Logger: logger,
		Metrics: metricsserver.Options{
			// disable metrics
			BindAddress: "0",
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: append(append(tt.additionalObjects, &appsv1.StatefulSet{}), tt.resources...),
			},
		},
	})
	require.NoError(t, err)

	go func() {
		if err := manager.Start(ctx); err != nil {
			panic(err)
		}
	}()

	client := manager.GetClient()

	require.NoError(t, InstallCRDs(ctx, client))

	return manager
}

type clientTest struct {
	watchedResources     []client.Object
	resources            []client.Object
	resourcesRenderError error
	nodePools            []*appsv1.StatefulSet
	nodePoolsRenderError error
	additionalObjects    []client.Object
}

type clientTestInstances struct {
	resolver         *MockOwnershipResolver
	updater          *MockClusterStatusUpdater
	nodeRenderer     *MockNodePoolRenderer
	resourceRenderer *MockSimpleResourceRenderer
	resourceClient   *ResourceClient[MockCluster, *MockCluster]
	manager          ctrl.Manager
	k8sClient        client.Client
}

func (i *clientTestInstances) checkObject(ctx context.Context, t *testing.T, o client.Object) error {
	t.Helper()

	kind, err := getGroupVersionKind(i.k8sClient.Scheme(), o)
	require.NoError(t, err)
	initialized, err := i.k8sClient.Scheme().New(*kind)
	require.NoError(t, err)
	initializedO := initialized.(client.Object)

	return i.k8sClient.Get(ctx, client.ObjectKeyFromObject(o), initializedO)
}

func (tt *clientTest) setupClient(ctx context.Context, t *testing.T) (*clientTestInstances, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	manager := tt.setupManager(ctx, t)

	resolver, updater, nodeRenderer, resourceRenderer, factory := MockResourceManagersSetup()
	resourceClient := NewResourceClient[MockCluster, *MockCluster](manager, factory)

	return &clientTestInstances{
		resolver:         resolver,
		updater:          updater,
		nodeRenderer:     nodeRenderer,
		resourceRenderer: resourceRenderer,
		resourceClient:   resourceClient,
		manager:          manager,
		k8sClient:        manager.GetClient(),
	}, cancel
}

func (tt *clientTest) HasDeleteableResources() bool {
	if tt.resourcesRenderError != nil || tt.nodePoolsRenderError != nil {
		return false
	}
	return len(tt.resources) != 0 || len(tt.nodePools) != 0
}

func (tt *clientTest) AdditionalResources() []client.Object {
	return tt.additionalObjects
}

func (tt *clientTest) InitialResources() []client.Object {
	initialResources := []client.Object{}

	if tt.resourcesRenderError == nil {
		initialResources = append(initialResources, tt.resources...)
	}

	if tt.nodePoolsRenderError == nil {
		for _, pool := range tt.nodePools {
			initialResources = append(initialResources, pool)
		}
	}

	return initialResources
}

func (tt *clientTest) Run(ctx context.Context, t *testing.T, name string, fn func(t *testing.T, instances *clientTestInstances, cluster *MockCluster)) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		t.Parallel()

		i, cancel := tt.setupClient(ctx, t)
		defer cancel()

		cluster := &MockCluster{}
		cluster.Name = name
		cluster.Namespace = metav1.NamespaceDefault

		require.NoError(t, i.k8sClient.Create(ctx, cluster))

		require.NoError(t, i.k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), cluster))

		i.resolver.AddOwner(cluster, map[string]string{
			"cluster-name": name,
		})

		if tt.nodePoolsRenderError != nil {
			i.nodeRenderer.SetError(cluster, tt.nodePoolsRenderError)
		} else {
			for _, resource := range tt.nodePools {
				resource.SetLabels(map[string]string{
					"cluster-name": name,
				})
			}
			i.nodeRenderer.SetPools(cluster, tt.nodePools)
		}

		i.resourceRenderer.SetWatchedResources(tt.watchedResources)
		if tt.resourcesRenderError != nil {
			i.resourceRenderer.SetError(cluster, tt.resourcesRenderError)
		} else {
			for _, resource := range tt.resources {
				resource.SetLabels(map[string]string{
					"cluster-name": name,
				})
			}
			i.resourceRenderer.SetResources(cluster, tt.resources)
		}

		for _, resource := range tt.additionalObjects {
			require.NoError(t, i.k8sClient.Create(ctx, resource))
		}

		fn(t, i, cluster)

		for _, resource := range tt.additionalObjects {
			require.NoError(t, i.k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), resource))
			require.Nil(t, resource.GetDeletionTimestamp())
		}
	})
}

func TestClientDeleteAll(t *testing.T) {
	for name, tt := range map[string]*clientTest{
		"no-op": {},
		"overlapping-resources": {
			watchedResources: []client.Object{
				&corev1.ConfigMap{},
				&corev1.Secret{},
			},
			resources: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "overlapping-resources-resource",
						Namespace: metav1.NamespaceDefault,
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "overlapping-resources-resource",
						Namespace: metav1.NamespaceDefault,
					},
				},
			},
		},
		"resources-with-finalizer": {
			watchedResources: []client.Object{
				&corev1.ConfigMap{},
			},
			resources: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "resources-with-finalizer",
						Namespace:  metav1.NamespaceDefault,
						Finalizers: []string{"cluster.test/test-finalizer"},
					},
				},
			},
		},
		"resources-and-pools": {
			watchedResources: []client.Object{
				&corev1.ConfigMap{},
			},
			resources: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resources-and-pools-resource",
						Namespace: metav1.NamespaceDefault,
					},
				},
			},
			additionalObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resources-and-pools-other",
						Namespace: metav1.NamespaceDefault,
					},
				},
			},
			nodePools: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "resources-and-pools-pool",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"label": "label"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"label": "label"},
							},
						},
					},
				},
			},
		},
	} {
		tt.Run(parentCtx, t, name, func(t *testing.T, instances *clientTestInstances, cluster *MockCluster) {
			ctx, cancel := setupContext()
			defer cancel()

			for _, resource := range tt.InitialResources() {
				// we need to set an owner ref since making sure those are present on namespace-scoped resources
				require.NoError(t, controllerutil.SetOwnerReference(cluster, resource, instances.manager.GetScheme()))
				require.NoError(t, instances.k8sClient.Create(ctx, resource))
			}

			deleted, err := instances.resourceClient.DeleteAll(ctx, cluster)
			require.NoError(t, err)
			require.Equal(t, tt.HasDeleteableResources(), deleted)

			deleted, err = instances.resourceClient.DeleteAll(ctx, cluster)
			require.NoError(t, err)
			require.False(t, deleted, "found stray resource")

			for _, pool := range tt.nodePools {
				if err := instances.k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), pool); err != nil {
					require.True(t, k8sapierrors.IsNotFound(err))
				} else {
					require.NotNil(t, pool.GetDeletionTimestamp())
				}
			}
			for _, resource := range tt.resources {
				if err := instances.k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
					require.True(t, k8sapierrors.IsNotFound(err))
				} else {
					require.NotNil(t, resource.GetDeletionTimestamp())
				}
			}
		})
	}
}

func TestClientWatchResources(t *testing.T) {
	for name, tt := range map[string]struct {
		watchedResources []string
		ownedResources   []string
		testParams       clientTest
	}{
		"base": {
			ownedResources: []string{"*v1.StatefulSet"},
		},
		"cluster-scoped-resources": {
			ownedResources:   []string{"*v1.StatefulSet"},
			watchedResources: []string{"*v1.PersistentVolume"},
			testParams: clientTest{
				watchedResources: []client.Object{
					&corev1.PersistentVolume{},
				},
			},
		},
		"namespace-and-cluster-scoped-resources": {
			ownedResources:   []string{"*v1.StatefulSet", "*v1.PersistentVolumeClaim", "*v1.Pod", "*v1.Secret", "*v1.ConfigMap"},
			watchedResources: []string{"*v1.PersistentVolume"},
			testParams: clientTest{
				watchedResources: []client.Object{
					&corev1.PersistentVolume{},
					&corev1.PersistentVolumeClaim{},
					&corev1.Pod{},
					&corev1.Secret{},
					&corev1.ConfigMap{},
				},
			},
		},
	} {
		tt.testParams.Run(parentCtx, t, name, func(t *testing.T, instances *clientTestInstances, cluster *MockCluster) {
			builder := NewMockBuilder(instances.manager)

			require.NoError(t, instances.resourceClient.WatchResources(builder, &MockCluster{}))
			require.Equal(t, "*lifecycle.MockCluster", builder.Base())
			require.ElementsMatch(t, tt.ownedResources, builder.Owned())
			require.ElementsMatch(t, tt.watchedResources, builder.Watched())
		})
	}
}

func TestClientSyncAll(t *testing.T) {
	for name, tt := range map[string]struct {
		renderLoops [][]client.Object
		testParams  clientTest
	}{
		"no-op": {},
		"render-error": {
			testParams: clientTest{
				resourcesRenderError: errors.New("render"),
			},
		},
		"overlapping-resource-names": {
			renderLoops: [][]client.Object{
				{
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "overlapping-resources-resource",
							Namespace: metav1.NamespaceDefault,
						},
					},
				},
			},
			testParams: clientTest{
				watchedResources: []client.Object{
					&corev1.ConfigMap{},
					&corev1.Secret{},
					&rbacv1.ClusterRole{},
				},
				resources: []client.Object{
					&corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "overlapping-resources-resource",
							Namespace: metav1.NamespaceDefault,
						},
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "overlapping-resources-resource",
							Namespace: metav1.NamespaceDefault,
						},
					},
					&rbacv1.ClusterRole{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "overlapping-resources-resource",
							Namespace: metav1.NamespaceDefault,
						},
					},
				},
			},
		},
	} {
		tt.testParams.Run(parentCtx, t, name, func(t *testing.T, instances *clientTestInstances, cluster *MockCluster) {
			ctx, cancel := setupContext()
			defer cancel()

			ensureSynced := func(resources []client.Object) {
				objects, err := instances.resourceClient.listAllOwnedResources(ctx, cluster, false)
				require.NoError(t, err)
				require.Len(t, objects, len(resources))
			}

			for _, resource := range tt.testParams.resources {
				err := instances.checkObject(ctx, t, resource)
				require.Error(t, err)
				require.True(t, k8sapierrors.IsNotFound(err))
			}

			err := instances.resourceClient.SyncAll(ctx, cluster)
			if tt.testParams.resourcesRenderError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.testParams.resourcesRenderError)
				return
			}

			require.NoError(t, err)

			ensureSynced(tt.testParams.resources)

			for _, resources := range tt.renderLoops {
				instances.resourceRenderer.SetResources(cluster, resources)

				require.NoError(t, instances.resourceClient.SyncAll(ctx, cluster))
				ensureSynced(resources)
			}
		})
	}
}

func TestClientFetchExistingAndDesiredPools(t *testing.T) {
	for name, tt := range map[string]*clientTest{
		"no-op": {},
		"render-error": {
			nodePoolsRenderError: errors.New("render"),
		},
		"single-pool": {
			nodePools: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pool-1",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"label": "label"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"label": "label"},
							},
						},
					},
				},
			},
		},
		"multiple-pools": {
			nodePools: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-pool-1",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"label": "label"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"label": "label"},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-pool-2",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"label": "label"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"label": "label"},
							},
						},
					},
				},
			},
		},
	} {
		tt.Run(parentCtx, t, name, func(t *testing.T, instances *clientTestInstances, cluster *MockCluster) {
			ctx, cancel := setupContext()
			defer cancel()

			tracker, err := instances.resourceClient.FetchExistingAndDesiredPools(ctx, cluster, "version")
			if tt.nodePoolsRenderError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.nodePoolsRenderError)
				return
			}

			pools := []string{}
			for _, pool := range tt.nodePools {
				pools = append(pools, client.ObjectKeyFromObject(pool).String())
			}

			require.NoError(t, err)
			require.Len(t, tracker.ExistingStatefulSets(), 0)
			require.ElementsMatch(t, pools, tracker.DesiredStatefulSets())

			for _, pool := range tt.nodePools {
				require.NoError(t, instances.resourceClient.PatchNodePoolSet(ctx, cluster, pool))
				require.NoError(t, instances.checkObject(ctx, t, pool))
			}

			tracker, err = instances.resourceClient.FetchExistingAndDesiredPools(ctx, cluster, "version")
			require.NoError(t, err)
			require.ElementsMatch(t, pools, tracker.ExistingStatefulSets())
			require.ElementsMatch(t, pools, tracker.DesiredStatefulSets())
		})
	}
}
