// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"context"
	"time"

	helmController "github.com/fluxcd/helm-controller/shim"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/metrics"
	helmSourceController "github.com/fluxcd/source-controller/shim"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/controller/redpanda"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *runner) addV2Reconcilers(ctx context.Context, mgr ctrl.Manager) *SetupError {
	ctrl.Log.Info("running in v2", "mode", OperatorV2Mode, "helm controllers enabled", r.enableHelmControllers, "namespace", r.namespace)

	// if we enable these controllers then run them, otherwise, do not
	//nolint:nestif // not really nested, required.
	if r.enableHelmControllers {
		storageAddr := ":9090"
		storageAdvAddr = redpandacontrollers.DetermineAdvStorageAddr(storageAddr, r.setupLog)
		storage := redpandacontrollers.MustInitStorage("/tmp", storageAdvAddr, 60*time.Second, 2, r.setupLog)

		metricsH := helper.NewMetrics(mgr, metrics.MustMakeRecorder())

		// TODO fill this in with options
		helmOpts := helmController.HelmReleaseReconcilerOptions{
			DependencyRequeueInterval: 30 * time.Second, // The interval at which failing dependencies are reevaluated.
			HTTPRetry:                 9,                // The maximum number of retries when failing to fetch artifacts over HTTP.
			RateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(30*time.Second, 60*time.Second),
		}

		// Helm Release Controller
		var err error
		var helmReleaseEventRecorder *events.Recorder
		if helmReleaseEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "HelmReleaseReconciler"); err != nil {
			return setupError(err, "unable to create event recorder for: HelmReleaseReconciler")
		}

		// Helm Release Controller
		helmRelease := helmController.HelmReleaseReconcilerFactory{
			Client:           mgr.GetClient(),
			EventRecorder:    helmReleaseEventRecorder,
			ClientOpts:       r.clientOptions,
			KubeConfigOpts:   r.kubeConfigOpts,
			FieldManager:     helmReleaseControllerName,
			Metrics:          metricsH,
			GetClusterConfig: ctrl.GetConfig,
		}
		if err := helmRelease.SetupWithManager(ctx, mgr, helmOpts); err != nil {
			return setupError(err, "Unable to create controller", "controller", "HelmRelease")
		}

		// Helm Chart Controller
		var helmChartEventRecorder *events.Recorder
		if helmChartEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "HelmChartReconciler"); err != nil {
			return setupError(err, "unable to create event recorder for: HelmChartReconciler")
		}

		cacheRecorder := helmSourceController.MustMakeCacheMetrics()
		indexTTL := 15 * time.Minute
		helmIndexCache := helmSourceController.NewCache(0, indexTTL)
		chartOpts := helmSourceController.HelmChartReconcilerOptions{
			RateLimiter: helper.GetDefaultRateLimiter(),
		}
		repoOpts := helmSourceController.HelmRepositoryReconcilerOptions{
			RateLimiter: helper.GetDefaultRateLimiter(),
		}
		helmChart := helmSourceController.HelmChartReconcilerFactory{
			Cache:                   helmIndexCache,
			CacheRecorder:           cacheRecorder,
			TTL:                     indexTTL,
			Client:                  mgr.GetClient(),
			RegistryClientGenerator: redpandacontrollers.ClientGenerator,
			Getters:                 getters,
			Metrics:                 metricsH,
			Storage:                 storage,
			EventRecorder:           helmChartEventRecorder,
			ControllerName:          helmChartControllerName,
		}
		if err := helmChart.SetupWithManager(ctx, mgr, chartOpts); err != nil {
			return setupError(err, "Unable to create controller", "controller", "HelmChart")
		}

		// Helm Repository Controller
		var helmRepositoryEventRecorder *events.Recorder
		if helmRepositoryEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "HelmRepositoryReconciler"); err != nil {
			return setupError(err, "unable to create event recorder for: HelmRepositoryReconciler")
		}

		helmRepository := helmSourceController.HelmRepositoryReconcilerFactory{
			Client:         mgr.GetClient(),
			EventRecorder:  helmRepositoryEventRecorder,
			Getters:        getters,
			ControllerName: helmRepositoryControllerName,
			Cache:          helmIndexCache,
			CacheRecorder:  cacheRecorder,
			TTL:            indexTTL,
			Metrics:        metricsH,
			Storage:        storage,
		}

		if err := helmRepository.SetupWithManager(ctx, mgr, repoOpts); err != nil {
			return setupError(err, "Unable to create controller", "controller", "HelmRepository")
		}

		go func() {
			// Block until our controller manager is elected leader. We presume our
			// entire process will terminate if we lose leadership, so we don't need
			// to handle that.
			<-mgr.Elected()

			redpandacontrollers.StartFileServer(storage.BasePath, storageAddr, r.setupLog)
		}()
	}

	// Redpanda Reconciler
	var err error
	var redpandaEventRecorder *events.Recorder
	if redpandaEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "RedpandaReconciler"); err != nil {
		return setupError(err, "unable to create event recorder for: RedpandaReconciler")
	}

	if err := (&redpandacontrollers.RedpandaReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: redpandaEventRecorder,
	}).SetupWithManager(ctx, mgr); err != nil {
		return setupError(err, "unable to create controller", "controller", "Redpanda")
	}

	var topicEventRecorder *events.Recorder
	if topicEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "TopicReconciler"); err != nil {
		return setupError(err, "unable to create event recorder for: TopicReconciler")
	}

	if err := (&redpandacontrollers.TopicReconciler{
		Client:        mgr.GetClient(),
		Factory:       internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()),
		Scheme:        mgr.GetScheme(),
		EventRecorder: topicEventRecorder,
	}).SetupWithManager(mgr); err != nil {
		return setupError(err, "unable to create controller", "controller", "Topic")
	}

	var managedDecommissionEventRecorder *events.Recorder
	if managedDecommissionEventRecorder, err = events.NewRecorder(mgr, ctrl.Log, r.eventsAddr, "ManagedDecommissionReconciler"); err != nil {
		return setupError(err, "unable to create event recorder for: ManagedDecommissionReconciler")
	}

	if err := (&redpandacontrollers.ManagedDecommissionReconciler{
		Client:        mgr.GetClient(),
		EventRecorder: managedDecommissionEventRecorder,
	}).SetupWithManager(mgr); err != nil {
		return setupError(err, "unable to create controller", "controller", "ManagedDecommission")
	}

	if runThisController(NodeController, r.additionalControllers) {
		if err := (&redpandacontrollers.RedpandaNodePVCReconciler{
			Client:       mgr.GetClient(),
			OperatorMode: r.operatorMode,
		}).SetupWithManager(mgr); err != nil {
			return setupError(err, "unable to create controller", "controller", "RedpandaNodePVCReconciler")
		}
	}

	if runThisController(DecommissionController, r.additionalControllers) {
		if err := (&redpandacontrollers.DecommissionReconciler{
			Client:                   mgr.GetClient(),
			OperatorMode:             r.operatorMode,
			DecommissionWaitInterval: r.decommissionWaitInterval,
		}).SetupWithManager(mgr); err != nil {
			return setupError(err, "unable to create controller", "controller", "DecommissionReconciler")
		}
	}

	if r.webhookEnabled {
		r.setupLog.Info("Setup Redpanda conversion webhook")
		if err := (&redpandav1alpha2.Redpanda{}).SetupWebhookWithManager(mgr); err != nil {
			return setupError(err, "Unable to create webhook", "webhook", "RedpandaConversion")
		}
	}

	return nil
}
