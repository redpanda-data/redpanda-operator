// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package flux

import (
	"context"
	"time"

	helmController "github.com/fluxcd/helm-controller/shim"
	"github.com/fluxcd/pkg/runtime/client"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/metrics"
	helmSourceController "github.com/fluxcd/source-controller/shim"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	helmReleaseControllerName    = "redpanda-helmrelease-controller"
	helmChartControllerName      = "redpanda-helmchart-reconciler"
	helmRepositoryControllerName = "redpanda-helmrepository-controller"

	indexTTL    = 15 * time.Minute
	storageAddr = ":9090"
)

var getters = getter.Providers{
	getter.Provider{
		Schemes: []string{"http", "https"},
		New:     getter.NewHTTPGetter,
	},
	getter.Provider{
		Schemes: []string{"oci"},
		New:     getter.NewOCIGetter,
	},
}

type ControllerSetup func(context.Context, ctrl.Manager) error

func (fn ControllerSetup) SetupWithManager(ctx context.Context, manager ctrl.Manager) error {
	return fn(ctx, manager)
}

func NewFluxControllers(
	mgr ctrl.Manager,
	clientOptions client.Options,
	kubeConfigOptions client.KubeConfigOptions,
) []ControllerSetup {
	cacheRecorder := helmSourceController.MustMakeCacheMetrics()
	helmIndexCache := helmSourceController.NewCache(0, indexTTL)
	metrics := helper.NewMetrics(mgr, metrics.MustMakeRecorder())

	storageAdvAddr := DetermineAdvStorageAddr(storageAddr, mgr.GetLogger())
	storage := MustInitStorage("/tmp", storageAdvAddr, 60*time.Second, 2, mgr.GetLogger())

	return []ControllerSetup{
		NewHelmChartController(mgr, helmIndexCache, cacheRecorder, metrics, storage),
		NewHelmRepositoryController(mgr, cacheRecorder, helmIndexCache, metrics, storage),
		NewHelmReleaseController(mgr, metrics, clientOptions, kubeConfigOptions),
	}
}

func NewHelmRepositoryController(
	mgr ctrl.Manager,
	cacheRecorder helmSourceController.CacheRecorder,
	cache helmSourceController.Cache,
	metrics helper.Metrics,
	storage helmSourceController.Storage,
) ControllerSetup {
	recorder := mgr.GetEventRecorderFor("HelmReleaseReconciler")

	controller := helmSourceController.HelmRepositoryReconcilerFactory{
		Client:         mgr.GetClient(),
		EventRecorder:  recorder,
		Getters:        getters,
		ControllerName: helmRepositoryControllerName,
		Cache:          cache,
		CacheRecorder:  cacheRecorder,
		TTL:            indexTTL,
		Metrics:        metrics,
		Storage:        storage,
	}

	return func(ctx context.Context, m ctrl.Manager) error {
		go func() {
			// Block until our controller manager is elected leader. We presume our
			// entire process will terminate if we lose leadership, so we don't need
			// to handle that.
			<-mgr.Elected()

			StartFileServer(storage.BasePath, storageAddr, m.GetLogger())
		}()

		opts := helmSourceController.HelmRepositoryReconcilerOptions{
			RateLimiter: helper.GetDefaultRateLimiter(),
		}

		return controller.SetupWithManager(ctx, mgr, opts)
	}
}

func NewHelmChartController(
	mgr ctrl.Manager,
	cache helmSourceController.Cache,
	cacheRecorder helmSourceController.CacheRecorder,
	metrics helper.Metrics,
	storage helmSourceController.Storage,
) ControllerSetup {
	helmChart := helmSourceController.HelmChartReconcilerFactory{
		Cache:                   cache,
		CacheRecorder:           cacheRecorder,
		TTL:                     indexTTL,
		Client:                  mgr.GetClient(),
		RegistryClientGenerator: ClientGenerator,
		Getters:                 getters,
		Metrics:                 metrics,
		Storage:                 storage,
		EventRecorder:           mgr.GetEventRecorderFor("HelmChartReconciler"),
		ControllerName:          helmChartControllerName,
	}

	return func(ctx context.Context, m ctrl.Manager) error {
		chartOpts := helmSourceController.HelmChartReconcilerOptions{
			RateLimiter: helper.GetDefaultRateLimiter(),
		}

		return helmChart.SetupWithManager(ctx, mgr, chartOpts)
	}
}

func NewHelmReleaseController(
	mgr ctrl.Manager,
	metrics helper.Metrics,
	clientOptions client.Options,
	kubeConfigOptions client.KubeConfigOptions,
) ControllerSetup {
	// Helm Release Controller
	helmRelease := helmController.HelmReleaseReconcilerFactory{
		Client:         mgr.GetClient(),
		EventRecorder:  mgr.GetEventRecorderFor("HelmReleaseReconciler"),
		ClientOpts:     clientOptions,
		KubeConfigOpts: kubeConfigOptions,
		FieldManager:   helmReleaseControllerName,
		Metrics:        metrics,
		GetClusterConfig: func() (*rest.Config, error) {
			return mgr.GetConfig(), nil
		},
	}

	return func(ctx context.Context, m ctrl.Manager) error {
		// TODO fill this in with options
		helmOpts := helmController.HelmReleaseReconcilerOptions{
			DependencyRequeueInterval: 30 * time.Second, // The interval at which failing dependencies are reevaluated.
			HTTPRetry:                 9,                // The maximum number of retries when failing to fetch artifacts over HTTP.
			RateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(30*time.Second, 60*time.Second),
		}

		return helmRelease.SetupWithManager(ctx, mgr, helmOpts)
	}
}
