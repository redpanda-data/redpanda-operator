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
	"hash/fnv"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	flag "github.com/spf13/pflag"
	raftv4 "go.etcd.io/raft/v3"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/multicluster-runtime/providers/clusters"

	"github.com/redpanda-data/redpanda-operator/pkg/locking"
	"github.com/redpanda-data/redpanda-operator/pkg/locking/multicluster/bootstrap"
	"github.com/redpanda-data/redpanda-operator/pkg/locking/raft"
	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/locking/raft/proto/gen/transport/v1"
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
}

type RaftConfiguration struct {
	Name              string
	Address           string
	Peers             []RaftCluster
	ElectionTimeout   time.Duration
	HeartbeatInterval time.Duration
	Meta              []byte

	Scheme     *runtime.Scheme
	Logger     logr.Logger
	Metrics    bool
	RestConfig *rest.Config

	// the are only used when the Insecure flag is set to false
	Insecure        bool
	CAFile          string
	PrivateKeyFile  string
	CertificateFile string

	// these are used when bootstrapping mode is enabled
	Bootstrap           bool
	KubernetesAPIServer string
	KubeconfigNamespace string
	KubeconfigName      string
}

func (r RaftConfiguration) validate() error {
	if r.Name == "" {
		return errors.New("name must be specified")
	}
	if r.Address == "" {
		return errors.New("address must be specified")
	}
	if !r.Insecure {
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

var (
	cliRaftConfiguration      RaftConfiguration
	cliRaftConfigurationPeers []string
)

func AddRaftConfigurationFlags(set *flag.FlagSet) {
	set.StringVar(&cliRaftConfiguration.Name, "raft-node-name", "", "raft node name")
	set.StringVar(&cliRaftConfiguration.Address, "raft-node-address", "", "raft node address")
	set.BoolVar(&cliRaftConfiguration.Insecure, "raft-insecure", false, "raft no tls")
	set.StringVar(&cliRaftConfiguration.CAFile, "raft-ca-file", "", "raft ca file")
	set.StringVar(&cliRaftConfiguration.PrivateKeyFile, "raft-private-key-file", "", "raft private key file")
	set.StringVar(&cliRaftConfiguration.CertificateFile, "raft-certificate-file", "", "raft certificate file")
	set.DurationVar(&cliRaftConfiguration.ElectionTimeout, "raft-election-timeout", 10*time.Second, "raft election timeout")
	set.DurationVar(&cliRaftConfiguration.HeartbeatInterval, "raft-heartbeat-interval", 1*time.Second, "raft heartbeat interval")
	set.StringSliceVar(&cliRaftConfigurationPeers, "raft-peers", []string{}, "raft peers")
	set.BoolVar(&cliRaftConfiguration.Bootstrap, "raft-bootstrap", false, "raft internally bootstrap kubeconfigs")
	set.StringVar(&cliRaftConfiguration.KubernetesAPIServer, "raft-k8s-api-address", "", "raft kubernetes api server address")
	set.StringVar(&cliRaftConfiguration.KubeconfigNamespace, "raft-kubeconfig-namespace", "default", "raft kubeconfig namespace")
	set.StringVar(&cliRaftConfiguration.KubeconfigName, "raft-kubeconfig-name", "multicluster-kubeconfig", "raft kubeconfig name")
}

func RaftConfigurationFromFlags() (RaftConfiguration, error) {
	for _, peer := range cliRaftConfigurationPeers {
		cluster, err := peerFromFlag(cliRaftConfiguration, peer)
		if err != nil {
			return RaftConfiguration{}, err
		}
		cliRaftConfiguration.Peers = append(cliRaftConfiguration.Peers, cluster)
	}

	return cliRaftConfiguration, nil
}

func peerFromFlag(configuration RaftConfiguration, value string) (RaftCluster, error) {
	parsed, err := url.Parse(value)
	if err != nil || (!configuration.Bootstrap && parsed.Path == "") {
		return RaftCluster{}, errors.New("format of peer flag is name://address/path/to/kubeconfig")
	}
	return RaftCluster{
		Name:           parsed.Scheme,
		Address:        parsed.Host,
		KubeconfigFile: parsed.Path,
	}, nil
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

	config.Logger.V(0).Info("initializing raft-based runtime manager", "node", config.Name, "peers", peers, "peerAddresses", peerAddresses)
	raftPeers := []raft.LockerNode{}
	idsToNames := map[uint64]string{}
	clusterProvider := clusters.New()
	for _, peer := range config.Peers {
		id := stringToHash(peer.Name)
		raftPeers = append(raftPeers, raft.LockerNode{
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
			c, err := cluster.New(kubeConfig)
			if err != nil {
				return nil, err
			}
			if err := clusterProvider.Add(context.Background(), peer.Name, c); err != nil {
				return nil, err
			}
		}
	}

	raftConfig := raft.LockConfiguration{
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
		raftConfig.Fetcher = raft.KubeconfigFetcherFn(func(ctx context.Context) ([]byte, error) {
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

	if !config.Insecure {
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
	}

	opts := manager.Options{
		Scheme:         config.Scheme,
		LeaderElection: false,
		Logger:         config.Logger,
	}
	if !config.Metrics {
		opts.Metrics = server.Options{
			BindAddress: "0",
		}
	}

	var currentLeader atomic.Uint64
	lockManager := locking.NewLeaderTrackingRaftLockManager(raftConfig, func(leader uint64) {
		currentLeader.Store(leader)
	})

	restart := make(chan struct{}, 1)

	if config.Bootstrap {
		for i, peer := range config.Peers {
			if peer.Name != config.Name && peer.KubeconfigFile == "" {
				config.Logger.Info("registering leader routine", "peer", peer.Name)
				lockManager.RegisterRoutine(func(ctx context.Context) error {
					config.Logger.Info("fetching client for peer", "peer", peer.Name)
					client, err := raft.ClientFor(raftConfig, raftConfig.Peers[i])
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

type raftLogr struct {
	logger logr.Logger
}

func (r *raftLogr) Debug(v ...any) {
	r.logger.V(1).Info("DEBUG", v...)
}

func (r *raftLogr) Debugf(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("DEBUG", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[DEBUG] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Error(v ...any) {
	r.logger.Error(errors.New("an error occurred"), "ERROR", v...)
}

func (r *raftLogr) Errorf(format string, v ...any) {
	if format == "" {
		r.logger.Error(errors.New("an error occurred"), "ERROR", v...)
	} else {
		text := fmt.Sprintf(format, v...)
		r.logger.Error(errors.New(text), text)
	}
}

func (r *raftLogr) Info(v ...any) {
	r.logger.V(0).Info("INFO", v...)
}

func (r *raftLogr) Infof(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("INFO", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[INFO] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Warning(v ...any) {
	r.logger.V(0).Info("WARN", v...)
}

func (r *raftLogr) Warningf(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("WARN", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[WARN] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Fatal(v ...any) {
	r.Error(v...)
	os.Exit(1)
}

func (r *raftLogr) Fatalf(format string, v ...any) {
	r.Errorf(format, v...)
	os.Exit(1)
}

func (r *raftLogr) Panic(v ...any) {
	r.Error(v...)
	panic("unexpected error")
}

func (r *raftLogr) Panicf(format string, v ...any) {
	r.Errorf(format, v...)
	panic(fmt.Sprintf(format, v...))
}

var _ raftv4.Logger = (*raftLogr)(nil)
