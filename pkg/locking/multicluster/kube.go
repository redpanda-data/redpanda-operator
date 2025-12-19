// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	"github.com/redpanda-data/redpanda-operator/pkg/locking"
	"github.com/redpanda-data/redpanda-operator/pkg/locking/kube"
)

type KubernetesClusterConfig struct {
	Name       string
	KubeConfig string
}

type KubernetesConfiguration struct {
	ID        string
	Namespace string
	Name      string

	RestConfig          *rest.Config
	MulticlusterConfigs []KubernetesClusterConfig

	Cache                  cache.Options
	Scheme                 *runtime.Scheme
	Logger                 logr.Logger
	Metrics                bool
	BaseContext            func() context.Context
	PprofBindAddress       string
	HealthProbeBindAddress string
	WebhookServer          webhook.Server
	MetricsServer          metricsserver.Options

	LeaderLabels     map[string]string
	RenewDeadline    time.Duration
	LeaseDuration    time.Duration
	RetryPeriod      time.Duration
	RecorderProvider recorder.Provider
}

func (r KubernetesConfiguration) validate() error {
	if r.Name == "" {
		return errors.New("name must be specified")
	}
	if r.Namespace == "" {
		return errors.New("namespace must be specified")
	}

	for i, cluster := range r.MulticlusterConfigs {
		if cluster.Name == "" {
			return fmt.Errorf("cluster config %d must have a name specified", i)
		}
		if cluster.KubeConfig == "" {
			return fmt.Errorf("cluster config %d must have a kubernetes config file specified", i)
		}
	}

	return nil
}

func NewKubernetesRuntimeManager(config KubernetesConfiguration) (Manager, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	restConfig := config.RestConfig
	if restConfig == nil {
		var err error
		restConfig, err = ctrl.GetConfig()
		if err != nil {
			return nil, err
		}
	}

	additionalClusters := []string{}
	for _, cluster := range config.MulticlusterConfigs {
		additionalClusters = append(additionalClusters, cluster.Name)
	}

	configs := []kube.ClusterNamespaceConfig{{
		Namespace: config.Namespace,
		Config:    restConfig,
	}}

	config.Logger.V(0).Info("initializing kubernetes lock-based runtime manager", "node", config.Name, "additionalClusters", additionalClusters)
	clusterProvider := clusters.New()
	for _, clusterConfig := range config.MulticlusterConfigs {
		kubeConfig, err := loadKubeconfig(clusterConfig.KubeConfig)
		if err != nil {
			return nil, err
		}
		c, err := cluster.New(kubeConfig)
		if err != nil {
			return nil, err
		}
		if err := clusterProvider.Add(context.Background(), clusterConfig.Name, c); err != nil {
			return nil, err
		}
		configs = append(configs, kube.ClusterNamespaceConfig{
			Namespace: config.Namespace,
			Config:    kubeConfig,
		})
	}

	kubeConfig := kube.LockConfiguration{
		ID:               config.ID,
		Name:             config.Name,
		Configs:          configs,
		LeaderLabels:     config.LeaderLabels,
		RenewDeadline:    config.RenewDeadline,
		LeaseDuration:    config.LeaseDuration,
		RetryPeriod:      config.RetryPeriod,
		RecorderProvider: config.RecorderProvider,
	}

	opts := manager.Options{
		Scheme:                 config.Scheme,
		LeaderElection:         false,
		Logger:                 config.Logger,
		Cache:                  config.Cache,
		BaseContext:            config.BaseContext,
		PprofBindAddress:       config.PprofBindAddress,
		HealthProbeBindAddress: config.HealthProbeBindAddress,
		Metrics:                config.MetricsServer,
		WebhookServer:          config.WebhookServer,
	}
	if !config.Metrics {
		opts.Metrics = server.Options{
			BindAddress: "0",
		}
	}

	lockManager := locking.NewKubernetesLockManager(kubeConfig)

	restart := make(chan struct{}, 1)
	manager, err := newManager(config.Logger, restConfig, clusterProvider, restart, nil, func() map[string]cluster.Cluster {
		clusters := map[string]cluster.Cluster{}
		for _, name := range clusterProvider.ClusterNames() {
			if c, err := clusterProvider.Get(context.Background(), name); err == nil {
				clusters[name] = c
			}
		}
		return clusters
	}, func(ctx context.Context, clusterName string, cl cluster.Cluster) error {
		if err := clusterProvider.AddOrReplace(ctx, clusterName, cl, nil); err != nil {
			return err
		}
		select {
		case restart <- struct{}{}:
		default:
		}
		return nil
	}, lockManager, opts)
	if err != nil {
		return nil, err
	}

	return manager, nil
}
