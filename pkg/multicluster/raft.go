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
	"sync"
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

// restartBroadcaster provides a broadcast notification mechanism.
// Calling notify() wakes ALL goroutines waiting on channel(), unlike a
// regular channel send which wakes only one receiver.
type restartBroadcaster struct {
	mu sync.Mutex
	ch chan struct{}
}

func newRestartBroadcaster() *restartBroadcaster {
	return &restartBroadcaster{ch: make(chan struct{})}
}

// notify wakes all goroutines currently blocked on channel().
func (b *restartBroadcaster) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.ch)
	b.ch = make(chan struct{})
}

// channel returns the current broadcast channel. Callers should select on
// it; when it closes (via notify), they must call channel() again to get
// the fresh replacement.
func (b *restartBroadcaster) channel() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

// RaftCluster describes a single cluster participating in raft-based
// leader election.
type RaftCluster struct {
	// Name uniquely identifies this cluster within the raft group.
	Name string
	// Address is the host:port where this cluster's raft transport listens.
	Address string
	// KubeconfigFile is an optional path to a kubeconfig for this cluster.
	KubeconfigFile string
	// Kubeconfig is an optional pre-loaded REST config for this cluster.
	Kubeconfig *rest.Config
}

// RaftConfiguration holds the full configuration for a raft-based multicluster
// runtime manager, including cluster identity, peer topology, TLS, and optional
// local leader election.
type RaftConfiguration struct {
	// Name uniquely identifies this node within the raft group (must match
	// one of the Peers entries).
	Name string
	// Address is the host:port this node's gRPC transport listens on.
	Address string
	// Peers is a list of all clusters in the raft group, including this one.
	Peers []RaftCluster
	// ElectionTimeout is the raft election timeout. Zero uses the default (10s).
	ElectionTimeout time.Duration
	// HeartbeatInterval is the raft heartbeat interval. Zero uses the default (1s).
	HeartbeatInterval time.Duration
	// GRPCMaxBackoff caps the exponential backoff delay between gRPC
	// reconnection attempts to peers. A shorter value speeds up recovery
	// when a peer restarts. Zero uses the default (5s).
	GRPCMaxBackoff time.Duration
	// Meta is opaque metadata attached to this node, accessible via the
	// transport's Check RPC.
	Meta []byte

	// Scheme is the runtime scheme used for all controller-runtime clusters.
	Scheme *runtime.Scheme
	// Logger is used for all raft and manager logging.
	Logger logr.Logger
	// Metrics configures the metrics server. Nil disables metrics.
	Metrics *metricsserver.Options
	// HealthProbeAddress is the bind address for the health probe endpoint.
	HealthProbeAddress string
	// Webhooks configures the webhook server. Nil disables webhooks.
	Webhooks webhook.Server
	// RestConfig is the Kubernetes REST config for the local cluster.
	// If nil, it is loaded from the default kubeconfig.
	RestConfig *rest.Config
	// BaseContext optionally provides a base context for the manager.
	BaseContext func() context.Context

	// Insecure disables TLS on the gRPC transport.
	Insecure bool
	// CAFile is the path to the CA certificate (used when Insecure is false
	// and TLS options are not provided).
	CAFile string
	// PrivateKeyFile is the path to the TLS private key.
	PrivateKeyFile string
	// CertificateFile is the path to the TLS certificate.
	CertificateFile string
	// ClientTLSOptions are custom TLS config mutators for outbound gRPC connections.
	ClientTLSOptions []func(*tls.Config)
	// ServerTLSOptions are custom TLS config mutators for the inbound gRPC listener.
	ServerTLSOptions []func(*tls.Config)

	// Bootstrap enables bootstrap mode, where the raft leader fetches
	// kubeconfigs from follower clusters to dynamically discover them.
	Bootstrap bool
	// KubernetesAPIServer is the advertised API server address used when
	// generating kubeconfigs in bootstrap mode.
	KubernetesAPIServer string
	// KubeconfigNamespace is the namespace for the bootstrap kubeconfig secret.
	KubeconfigNamespace string
	// KubeconfigName is the name of the bootstrap kubeconfig secret.
	KubeconfigName string

	// SkipNameValidation allows multiple controllers with the same name,
	// needed for multicluster run in the same testing process.
	SkipNameValidation bool

	// LocalLeaderElection configures K8s lease-based leader election within
	// the local cluster. When non-nil, only the pod holding the local lease
	// participates in raft. This allows running multiple operator replicas
	// per cluster for high availability — "double leader-election": first
	// within the local cluster (K8s lease), then across clusters (raft).
	LocalLeaderElection *LocalLeaderElectionConfig
}

// LocalLeaderElectionConfig holds the configuration for K8s lease-based
// leader election within the local cluster.
type LocalLeaderElectionConfig struct {
	// ID is the name of the K8s Lease resource used for leader election.
	ID string
	// Namespace is the namespace where the Lease resource is created.
	Namespace string
	// LeaseDuration is how long a non-leader waits before attempting to
	// acquire the lease.
	LeaseDuration time.Duration
	// RenewDeadline is how long the current leader retries renewing before
	// giving up.
	RenewDeadline time.Duration
	// RetryPeriod is the interval between lease acquisition attempts by
	// non-leaders.
	RetryPeriod time.Duration
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

// NewRaftRuntimeManager creates a Manager backed by raft-based cross-cluster
// leader election. Only the raft leader's manager starts controller runnables.
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
				o.Logger = config.Logger.WithName("clusterProvider").WithValues("peerName", peer.Name)
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
				o.Logger = config.Logger.WithName("clusterProvider").WithValues("peerName", peer.Name)
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
		GRPCMaxBackoff:    config.GRPCMaxBackoff,
		Logger:            &raftLogr{logger: config.Logger.WithName("raft")},
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
		LeaderElection: config.LocalLeaderElection != nil,
		Logger:         config.Logger.WithName("multicluster-manager"),
		WebhookServer:  config.Webhooks,
		BaseContext:    config.BaseContext,
	}
	if lle := config.LocalLeaderElection; lle != nil {
		opts.LeaderElectionID = lle.ID
		opts.LeaderElectionNamespace = lle.Namespace
		opts.LeaderElectionReleaseOnCancel = true
		opts.LeaderElectionResourceLock = "leases"
		if lle.LeaseDuration > 0 {
			opts.LeaseDuration = &lle.LeaseDuration
		}
		if lle.RenewDeadline > 0 {
			opts.RenewDeadline = &lle.RenewDeadline
		}
		if lle.RetryPeriod > 0 {
			opts.RetryPeriod = &lle.RetryPeriod
		}
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

	broadcaster := newRestartBroadcaster()

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
						o.Logger = config.Logger.WithName("clusterProvider").WithValues("peerName", peer.Name)
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
					broadcaster.notify()

					<-ctx.Done()
					return nil
				})
			}
		}
	}

	manager, err := newManager(config.Name, config.LocalLeaderElection != nil, config.Logger.WithName("manager"), restConfig, clusterProvider, broadcaster, func() string {
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
		broadcaster.notify()
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
	localClusterName    string
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

func (m *raftManager) GetLocalClusterName() string {
	return m.localClusterName
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

func newManager(localClusterName string, localLeaderElection bool, logger logr.Logger, config *rest.Config, provider multicluster.Provider, broadcaster *restartBroadcaster, getLeader func() string, getClusters func() map[string]cluster.Cluster, addOrReplaceCluster func(ctx context.Context, clusterName string, cl cluster.Cluster) error, manager *leaderelection.LeaderManager, opts manager.Options) (Manager, error) {
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

	runnable := &leaderRunnable{manager: manager, logger: logger.WithName("leader-runnable"), broadcaster: broadcaster, getClusters: getClusters, needsLocalLeaderElection: localLeaderElection}
	if err := mgr.Add(runnable); err != nil {
		return nil, err
	}
	return &raftManager{Manager: mgr, manager: manager, runnable: runnable, logger: logger.WithName("raft-manager"), localClusterName: localClusterName, getLeader: getLeader, getClusters: getClusters, addOrReplaceCluster: addOrReplaceCluster}, nil
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
	runnables                []mcmanager.Runnable
	manager                  *leaderelection.LeaderManager
	logger                   logr.Logger
	broadcaster              *restartBroadcaster
	getClusters              func() map[string]cluster.Cluster
	needsLocalLeaderElection bool
}

func (l *leaderRunnable) NeedLeaderElection() bool {
	return l.needsLocalLeaderElection
}

func (l *leaderRunnable) Add(r mcmanager.Runnable) {
	doEngage := func(ctx context.Context) {
		for name, cluster := range l.getClusters() {
			l.logger.Info("engaging cluster", "cluster", name)

			if err := r.Engage(ctx, name, cluster); err != nil {
				l.logger.Error(err, "error engaging cluster", "cluster", name)
				// Schedule a retry so transient failures are recovered.
				go func() {
					select {
					case <-ctx.Done():
					case <-time.After(10 * time.Second):
						l.broadcaster.notify()
					}
				}()
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

func (l *leaderRunnable) wrapStart(doEngage func(context.Context), fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		doEngage(cancelCtx)

		go func() {
			for {
				ch := l.broadcaster.channel()
				select {
				case <-cancelCtx.Done():
					return
				case <-ch:
					doEngage(cancelCtx)
				}
			}
		}()

		return fn(cancelCtx)
	}
}
