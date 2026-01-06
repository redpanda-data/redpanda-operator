// Copyright 2026 Redpanda Data, Inc.
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
	"crypto/tls"
	"errors"
	"hash/fnv"
	"net/http"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection"
	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

func stringToHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

type RaftCluster struct {
	Name           string
	Address        string
	KubeconfigFile string
	Kubeconfig     *rest.Config
}

type RaftConfiguration struct {
	Name              string
	Address           string
	Peers             []RaftCluster
	ElectionTimeout   time.Duration
	HeartbeatInterval time.Duration
	Meta              []byte

	Scheme             *runtime.Scheme
	Logger             logr.Logger
	Metrics            *metricsserver.Options
	HealthProbeAddress string
	Webhooks           webhook.Server
	RestConfig         *rest.Config
	BaseContext        func() context.Context

	Insecure bool
	// these are only used when the Insecure flag is set to false
	CAFile           string
	PrivateKeyFile   string
	CertificateFile  string
	ClientTLSOptions []func(*tls.Config)
	ServerTLSOptions []func(*tls.Config)

	// these are used when bootstrapping mode is enabled
	Bootstrap           bool
	KubernetesAPIServer string
	KubeconfigNamespace string
	KubeconfigName      string

	// For multicluster runtime to be able to run multiple controllers with the same name
	SkipNameValidation bool
}

func (r RaftConfiguration) validate() error {
	if r.Name == "" {
		return errors.New("name must be specified")
	}
	if r.Address == "" {
		return errors.New("address must be specified")
	}
	if !r.Insecure && (len(r.ClientTLSOptions) == 0 || len(r.ServerTLSOptions) == 0) {
		if len(r.CAFile) == 0 {
			return errors.New("ca must be specified")
		}
		if len(r.PrivateKeyFile) == 0 {
			return errors.New("private key must be specified")
		}
		if len(r.CertificateFile) == 0 {
			return errors.New("certificate must be specified")
		}
	}
	if len(r.Peers) == 0 {
		return errors.New("peers must be set")
	}

	return nil
}

func NewRaftRuntimeManager(config RaftConfiguration) (Manager, error) {
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

	peers := []string{}
	peerAddresses := []string{}
	for _, peer := range config.Peers {
		peers = append(peers, peer.Name)
		peerAddresses = append(peerAddresses, peer.Address)
	}

	config.Logger.Info("initializing raft-based runtime manager", "node", config.Name, "peers", peers, "peerAddresses", peerAddresses)
	raftPeers := []leaderelection.LockerNode{}
	idsToNames := map[uint64]string{}
	clusterProvider := clusters.New()
	for _, peer := range config.Peers {
		id := stringToHash(peer.Name)
		raftPeers = append(raftPeers, leaderelection.LockerNode{
			ID:      id,
			Address: peer.Address,
		})
		idsToNames[id] = peer.Name

		if peer.Name == config.Name {
			continue
		}

		if peer.KubeconfigFile != "" {
			kubeConfig, err := loadKubeconfig(peer.KubeconfigFile)
			if err != nil {
				return nil, err
			}
			c, err := cluster.New(kubeConfig, func(o *cluster.Options) {
				o.Scheme = config.Scheme
				o.Logger = config.Logger
			})
			if err != nil {
				return nil, err
			}
			if err := clusterProvider.Add(context.Background(), peer.Name, c); err != nil {
				return nil, err
			}
		}
		if peer.Kubeconfig != nil {
			c, err := cluster.New(peer.Kubeconfig, func(o *cluster.Options) {
				o.Scheme = config.Scheme
				o.Logger = config.Logger
			})
			if err != nil {
				return nil, err
			}
			if err := clusterProvider.Add(context.Background(), peer.Name, c); err != nil {
				return nil, err
			}
		}
	}

	raftConfig := leaderelection.LockConfiguration{
		ID:                stringToHash(config.Name),
		Address:           config.Address,
		Peers:             raftPeers,
		Meta:              config.Meta,
		Insecure:          config.Insecure,
		ElectionTimeout:   config.ElectionTimeout,
		HeartbeatInterval: config.HeartbeatInterval,
		Logger:            &raftLogr{logger: config.Logger},
	}

	if config.Bootstrap {
		raftConfig.Fetcher = leaderelection.KubeconfigFetcherFn(func(ctx context.Context) ([]byte, error) {
			data, err := bootstrap.CreateRemoteKubeconfig(ctx, &bootstrap.RemoteKubernetesConfiguration{
				RESTConfig: restConfig,
				APIServer:  config.KubernetesAPIServer,
				Namespace:  config.KubeconfigNamespace,
				Name:       config.KubeconfigName,
			})
			if err != nil {
				return nil, err
			}

			return data, nil
		})
	}

	if !config.Insecure && (len(config.ClientTLSOptions) == 0 || len(config.ServerTLSOptions) == 0) {
		var err error

		raftConfig.CA, err = os.ReadFile(config.CAFile)
		if err != nil {
			return nil, err
		}

		raftConfig.Certificate, err = os.ReadFile(config.CertificateFile)
		if err != nil {
			return nil, err
		}

		raftConfig.PrivateKey, err = os.ReadFile(config.PrivateKeyFile)
		if err != nil {
			return nil, err
		}
	} else if len(config.ClientTLSOptions) != 0 && len(config.ServerTLSOptions) != 0 {
		raftConfig.ClientTLSOptions = config.ClientTLSOptions
		raftConfig.ServerTLSOptions = config.ServerTLSOptions
	}

	opts := manager.Options{
		Scheme:         config.Scheme,
		LeaderElection: false,
		Logger:         config.Logger,
		WebhookServer:  config.Webhooks,
		BaseContext:    config.BaseContext,
	}
	if config.Metrics == nil {
		opts.Metrics = server.Options{
			BindAddress: "0",
		}
	} else {
		opts.Metrics = *config.Metrics
	}

	if config.HealthProbeAddress != "" {
		opts.HealthProbeBindAddress = config.HealthProbeAddress
	}

	if config.SkipNameValidation {
		opts.Controller = ctrlconfig.Controller{
			SkipNameValidation: ptr.To(true),
		}
	}

	var currentLeader atomic.Uint64
	lockManager := leaderelection.NewRaftLockManager(raftConfig, func(leader uint64) {
		currentLeader.Store(leader)
	})

	restart := make(chan struct{}, 1)

	if config.Bootstrap {
		for i, peer := range config.Peers {
			if peer.Name != config.Name && peer.KubeconfigFile == "" {
				config.Logger.Info("registering leader routine", "peer", peer.Name)
				lockManager.RegisterRoutine(func(ctx context.Context) error {
					config.Logger.Info("fetching client for peer", "peer", peer.Name)
					client, err := leaderelection.ClientFor(raftConfig, raftConfig.Peers[i])
					if err != nil {
						config.Logger.Error(err, "fetching client for peer", "peer", peer.Name)
						return err
					}
					config.Logger.Info("fetching kubeconfig for peer", "peer", peer.Name)
					response, err := client.Kubeconfig(ctx, &transportv1.KubeconfigRequest{})
					if err != nil {
						config.Logger.Error(err, "fetching kubeconfig for peer", "peer", peer.Name)
						return err
					}

					config.Logger.Info("loading kubeconfig for peer", "peer", peer.Name)
					kubeConfig, err := loadKubeconfigFromBytes(response.Payload)
					if err != nil {
						config.Logger.Error(err, "loading kubeconfig for peer", "peer", peer.Name)
						return err
					}
					config.Logger.Info("initializing cluster for peer", "peer", peer.Name)
					c, err := cluster.New(kubeConfig, func(o *cluster.Options) {
						o.Scheme = config.Scheme
						o.Logger = config.Logger
					})
					if err != nil {
						config.Logger.Error(err, "initializing cluster for peer", "peer", peer.Name)
						return err
					}

					config.Logger.Info("adding cluster for peer", "peer", peer.Name)
					if err := clusterProvider.AddOrReplace(ctx, peer.Name, c, nil); err != nil {
						config.Logger.Error(err, "adding cluster for peer", "peer", peer.Name)
						return err
					}
					select {
					case restart <- struct{}{}:
					default:
					}

					<-ctx.Done()
					return nil
				})
			}
		}
	}

	manager, err := newManager(config.Logger, restConfig, clusterProvider, restart, func() string {
		return idsToNames[currentLeader.Load()]
	}, func() map[string]cluster.Cluster {
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

type raftManager struct {
	mcmanager.Manager
	runnable            *leaderRunnable
	manager             *leaderelection.LeaderManager
	logger              logr.Logger
	getLeader           func() string
	getClusters         func() map[string]cluster.Cluster
	addOrReplaceCluster func(ctx context.Context, clusterName string, cl cluster.Cluster) error
}

func (m *raftManager) AddOrReplaceCluster(ctx context.Context, clusterName string, cl cluster.Cluster) error {
	return m.addOrReplaceCluster(ctx, clusterName, cl)
}

func (m *raftManager) GetClusterNames() []string {
	clusters := []string{mcmanager.LocalCluster}
	if m.getClusters == nil {
		return clusters
	}

	for cluster := range m.getClusters() {
		clusters = append(clusters, cluster)
	}
	sort.Strings(clusters)
	return clusters
}

func (m *raftManager) GetLeader() string {
	if m.getLeader == nil {
		return ""
	}
	return m.getLeader()
}

func (m *raftManager) Health(req *http.Request) error {
	return m.manager.Health(req)
}

func newManager(logger logr.Logger, config *rest.Config, provider multicluster.Provider, restart chan struct{}, getLeader func() string, getClusters func() map[string]cluster.Cluster, addOrReplaceCluster func(ctx context.Context, clusterName string, cl cluster.Cluster) error, manager *leaderelection.LeaderManager, opts manager.Options) (Manager, error) {
	mgr, err := mcmanager.New(config, provider, opts)
	if err != nil {
		return nil, err
	}

	manager.RegisterRoutine(func(ctx context.Context) error {
		logger.Info("got leader")
		<-ctx.Done()
		logger.Info("lost leader")
		return nil
	})

	runnable := &leaderRunnable{manager: manager, logger: logger, restart: restart, getClusters: getClusters}
	if err := mgr.Add(runnable); err != nil {
		return nil, err
	}
	return &raftManager{Manager: mgr, manager: manager, runnable: runnable, logger: logger, getLeader: getLeader, getClusters: getClusters, addOrReplaceCluster: addOrReplaceCluster}, nil
}

func (m *raftManager) Add(r mcmanager.Runnable) error {
	if _, ok := r.(reconcile.TypedReconciler[mcreconcile.Request]); ok {
		m.logger.Info("adding multicluster reconciler")
		m.runnable.Add(r)
		return nil
	}

	if _, ok := r.(manager.LeaderElectionRunnable); ok {
		m.logger.Info("adding leader election runnable")
		m.runnable.Add(r)
		return nil
	}

	return m.Manager.Add(r)
}

type warmupRunnable interface {
	Warmup(context.Context) error
}

type leaderRunnable struct {
	runnables   []mcmanager.Runnable
	manager     *leaderelection.LeaderManager
	logger      logr.Logger
	restart     chan struct{}
	getClusters func() map[string]cluster.Cluster
}

func (l *leaderRunnable) Add(r mcmanager.Runnable) {
	doEngage := func() {
		for name, cluster := range l.getClusters() {
			l.logger.Info("engaging cluster", "cluster", name)

			// engage any static clusters
			if err := r.Engage(context.Background(), name, cluster); err != nil {
				l.logger.Error(err, "error engaging cluster", "cluster", name)
			}
		}
	}

	l.runnables = append(l.runnables, r)
	if warmup, ok := r.(warmupRunnable); ok {
		// start caches and sources
		l.manager.RegisterRoutine(l.wrapStart(doEngage, warmup.Warmup))
	}
	l.manager.RegisterRoutine(l.wrapStart(doEngage, r.Start))
}

func (l *leaderRunnable) Engage(ctx context.Context, s string, c cluster.Cluster) error {
	for _, runnable := range l.runnables {
		if err := runnable.Engage(ctx, s, c); err != nil {
			l.logger.Info("engaging runnable")
			return err
		}
	}
	return nil
}

func (l *leaderRunnable) Start(ctx context.Context) error {
	return l.manager.Run(ctx)
}

func (l *leaderRunnable) wrapStart(doEngage func(), fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		doEngage()

		go func() {
			for {
				select {
				case <-cancelCtx.Done():
					return
				case <-l.restart:
					// re-engage
					doEngage()
				}
			}
		}()

		return fn(cancelCtx)
	}
}
