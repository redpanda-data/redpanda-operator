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
	"github.com/fluxcd/pkg/runtime/events"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/vectorized/v1alpha1"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/controller/pvcunbinder"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/controller/redpanda"
	adminutils "github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/admin"
	consolepkg "github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/console"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/resources"
	redpandawebhooks "github.com/redpanda-data/redpanda-operator/src/go/k8s/webhooks/redpanda"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (r *runner) setV1Globals() {
	vectorizedv1alpha1.SuperUsersPrefix = r.superUsersPrefix
	vectorizedv1alpha1.AllowConsoleAnyNamespace = r.allowConsoleAnyNamespace
	vectorizedv1alpha1.AllowDownscalingInWebhook = r.allowDownscalingInWebhook
}

func (r *runner) addV1Reconcilers(mgr ctrl.Manager) *SetupError {
	ctrl.Log.Info("running in v1", "mode", OperatorV1Mode)

	configurator := resources.ConfiguratorSettings{
		ConfiguratorBaseImage: r.configuratorBaseImage,
		ConfiguratorTag:       r.configuratorTag,
		ImagePullPolicy:       corev1.PullPolicy(r.configuratorImagePullPolicy),
	}

	if err := (&redpandacontrollers.ClusterReconciler{
		Client:                    mgr.GetClient(),
		Log:                       ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Cluster"),
		Scheme:                    mgr.GetScheme(),
		AdminAPIClientFactory:     adminutils.NewInternalAdminAPI,
		DecommissionWaitInterval:  r.decommissionWaitInterval,
		MetricsTimeout:            r.metricsTimeout,
		RestrictToRedpandaVersion: r.restrictToRedpandaVersion,
		GhostDecommissioning:      r.ghostbuster,
		AutoDeletePVCs:            r.autoDeletePVCs,
	}).WithClusterDomain(r.clusterDomain).WithConfiguratorSettings(configurator).SetupWithManager(mgr); err != nil {
		return setupError(err, "Unable to create controller", "controller", "Cluster")
	}

	if err := (&redpandacontrollers.ClusterConfigurationDriftReconciler{
		Client:                    mgr.GetClient(),
		Log:                       ctrl.Log.WithName("controllers").WithName("redpanda").WithName("ClusterConfigurationDrift"),
		Scheme:                    mgr.GetScheme(),
		AdminAPIClientFactory:     adminutils.NewInternalAdminAPI,
		RestrictToRedpandaVersion: r.restrictToRedpandaVersion,
	}).WithClusterDomain(r.clusterDomain).SetupWithManager(mgr); err != nil {
		return setupError(err, "Unable to create controller", "controller", "ClusterConfigurationDrift")
	}

	if err := redpandacontrollers.NewClusterMetricsController(mgr.GetClient()).
		SetupWithManager(mgr); err != nil {
		return setupError(err, "Unable to create controller", "controller", "ClustersMetrics")
	}

	if err := (&redpandacontrollers.ConsoleReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		Log:                     ctrl.Log.WithName("controllers").WithName("redpanda").WithName("Console"),
		AdminAPIClientFactory:   adminutils.NewInternalAdminAPI,
		Store:                   consolepkg.NewStore(mgr.GetClient(), mgr.GetScheme()),
		EventRecorder:           mgr.GetEventRecorderFor("Console"),
		KafkaAdminClientFactory: consolepkg.NewKafkaAdmin,
	}).WithClusterDomain(r.clusterDomain).SetupWithManager(mgr); err != nil {
		return setupError(err, "unable to create controller", "controller", "Console")
	}

	var err error
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

	if r.unbindPVCsAfter <= 0 {
		r.setupLog.Info("PVCUnbinder controller not active", "flag", r.unbindPVCsAfter)
	} else {
		r.setupLog.Info("starting PVCUnbinder controller", "flag", r.unbindPVCsAfter)

		if err := (&pvcunbinder.Reconciler{
			Client:  mgr.GetClient(),
			Timeout: r.unbindPVCsAfter,
		}).SetupWithManager(mgr); err != nil {
			return setupError(err, "unable to create controller", "controller", "PVCUnbinder")
		}
	}

	// Setup webhooks
	if r.webhookEnabled {
		r.setupLog.Info("Setup webhook")
		if err = (&vectorizedv1alpha1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
			return setupError(err, "Unable to create webhook", "webhook", "RedpandaCluster")
		}
		hookServer := mgr.GetWebhookServer()
		hookServer.Register("/mutate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
			Handler: &redpandawebhooks.ConsoleDefaulter{
				Client:  mgr.GetClient(),
				Decoder: admission.NewDecoder(scheme),
			},
		})
		hookServer.Register("/validate-redpanda-vectorized-io-v1alpha1-console", &webhook.Admission{
			Handler: &redpandawebhooks.ConsoleValidator{
				Client:  mgr.GetClient(),
				Decoder: admission.NewDecoder(scheme),
			},
		})
	}

	return nil
}
