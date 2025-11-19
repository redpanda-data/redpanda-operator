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
	"net/url"
	"os"
	"time"

	"github.com/andrewstucki/locking/multicluster"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
)

//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;create;update;patch

type RaftCluster struct {
	Name    string
	Address string
}

type MulticlusterOptions struct {
	Name                string
	Address             string
	Peers               []RaftCluster
	ElectionTimeout     time.Duration
	HeartbeatInterval   time.Duration
	CAFile              string
	PrivateKeyFile      string
	CertificateFile     string
	KubernetesAPIServer string
	KubeconfigNamespace string
	KubeconfigName      string
	LogLevel            string

	peersStrings []string
}

func (o *MulticlusterOptions) validate() error {
	if o.Name == "" {
		return errors.New("name must be specified")
	}
	if o.Address == "" {
		return errors.New("address must be specified")
	}
	if len(o.CAFile) == 0 {
		return errors.New("ca must be specified")
	}
	if len(o.PrivateKeyFile) == 0 {
		return errors.New("private key must be specified")
	}
	if len(o.CertificateFile) == 0 {
		return errors.New("certificate must be specified")
	}
	if len(o.peersStrings) < 3 {
		return errors.New("peers must be set and contain 3 or more nodes")
	}

	for _, peer := range o.peersStrings {
		cluster, err := peerFromFlag(peer)
		if err != nil {
			return fmt.Errorf("parsing peer flag %q: %v", peer, err)
		}
		o.Peers = append(o.Peers, cluster)
	}

	return nil
}

func peerFromFlag(value string) (RaftCluster, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return RaftCluster{}, errors.New("format of peer flag is name://address")
	}
	return RaftCluster{
		Name:    parsed.Scheme,
		Address: parsed.Host,
	}, nil
}

func (o *MulticlusterOptions) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Name, "name", "", "raft node name")
	cmd.Flags().StringVar(&o.Address, "address", "", "raft node address")
	cmd.Flags().StringVar(&o.CAFile, "ca-file", "", "raft ca file")
	cmd.Flags().StringVar(&o.PrivateKeyFile, "private-key-file", "", "raft private key file")
	cmd.Flags().StringVar(&o.CertificateFile, "certificate-file", "", "raft certificate file")
	cmd.Flags().DurationVar(&o.ElectionTimeout, "election-timeout", 10*time.Second, "raft election timeout")
	cmd.Flags().DurationVar(&o.HeartbeatInterval, "heartbeat-interval", 1*time.Second, "raft heartbeat interval")
	cmd.Flags().StringSliceVar(&o.peersStrings, "peer", []string{}, "raft peers")
	cmd.Flags().StringVar(&o.KubernetesAPIServer, "kubernetes-api-address", "", "raft kubernetes api server address")
	cmd.Flags().StringVar(&o.KubeconfigNamespace, "kubeconfig-namespace", "default", "raft kubeconfig namespace")
	cmd.Flags().StringVar(&o.KubeconfigName, "kubeconfig-name", "multicluster-kubeconfig", "raft kubeconfig name")
	cmd.Flags().StringVar(&o.LogLevel, "log-level", "info", "log level")
}

func Command() *cobra.Command {
	var options MulticlusterOptions

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the redpanda operator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(cmd.Context(), &options)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

func Run(
	ctx context.Context,
	opts *MulticlusterOptions,
) error {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	log := zerologr.New(&logger)
	ctrl.SetLogger(log)

	if err := opts.validate(); err != nil {
		return err
	}
	k8sConfig, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	config := multicluster.RaftConfiguration{
		Name:                opts.Name,
		Address:             opts.Address,
		ElectionTimeout:     opts.ElectionTimeout,
		HeartbeatInterval:   opts.HeartbeatInterval,
		Logger:              log,
		Metrics:             true,
		RestConfig:          k8sConfig,
		Scheme:              controller.MulticlusterScheme,
		CAFile:              opts.CAFile,
		CertificateFile:     opts.CertificateFile,
		PrivateKeyFile:      opts.PrivateKeyFile,
		Bootstrap:           true,
		KubernetesAPIServer: opts.KubernetesAPIServer,
		KubeconfigNamespace: opts.KubeconfigNamespace,
		KubeconfigName:      opts.KubeconfigName,
	}

	for _, peer := range opts.Peers {
		config.Peers = append(config.Peers, multicluster.RaftCluster{
			Name:    peer.Name,
			Address: peer.Address,
		})
	}

	manager, err := multicluster.NewRaftRuntimeManager(config)
	if err != nil {
		return fmt.Errorf("initializing cluster: %w", err)
	}

	if err := redpandacontrollers.SetupMulticlusterController(ctx, manager); err != nil {
		log.Error(err, "unable to create controller", "controller", "Multicluster")
		return err
	}

	return manager.Start(ctrl.SetupSignalHandler())
}
