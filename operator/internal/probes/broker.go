// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package probes

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

type Option func(*Prober)

func WithLogger(logger logr.Logger) Option {
	return func(prober *Prober) {
		prober.logger = logger
	}
}

func WithFS(fs afero.Fs) Option {
	return func(prober *Prober) {
		prober.fs = fs
	}
}

// Prober wraps the logic used in checking broker health and readiness.
type Prober struct {
	logger     logr.Logger
	factory    internalclient.ClientFactory
	configPath string
	fs         afero.Fs
}

func NewProber(factory internalclient.ClientFactory, configPath string, options ...Option) *Prober {
	prober := &Prober{
		configPath: configPath,
		fs:         afero.NewOsFs(),
		logger:     logr.Discard(),
		factory:    factory,
	}

	for _, opt := range options {
		opt(prober)
	}

	return prober
}

// IsClusterBrokerHealthy checks that a cluster broker is up and ready to serve requests
// but additionally serves as a gate for StatefulSet rolling restarts. It's a relaxed form
// of our previous readiness probe which only looked at the `IsHealthy` return value from
// the cluster health overview endpoint. This had the unfortunate effect of marking all brokers
// as "unready" when a single broker was marked as a downed node due to being a reflection of
// the overall cluster health rather than an individual broker's health.
func (p *Prober) IsClusterBrokerHealthy(ctx context.Context, brokerURL string) (bool, error) {
	client, brokerID, err := p.getClient(ctx, brokerURL)
	if err != nil {
		return false, fmt.Errorf("initializing client to check broker health: %w", err)
	}
	defer client.Close()

	// This is essentially the old health check of our broker nodes, it is here
	// to act as a fall back for certain 24.x and 23.x clusters that have a broken
	// /v1/partitions/local_summary endpoint that doesn't properly report on under
	// replicated partitions (in testing, it generally shows under-replication when
	// there is none). By placing this here we essentially say, "if the strict cluster
	// overview health check passes, then don't run the relaxed health check". This has
	// the benefit of basically acting like our old health check in clusters that are
	// otherwise broken.
	//
	// Reference: https://github.com/redpanda-data/redpanda/pull/24837
	healthOverview, err := client.GetHealthOverview(ctx)
	if err != nil {
		return false, fmt.Errorf("fetching cluser health: %w", err)
	}

	if healthOverview.IsHealthy {
		return true, nil
	}

	// This check is a more relaxed version of our previous "readiness" probe that was
	// based solely off of the cluster health. It works via doing the following:
	//
	// 1. Gets the broker status, making sure it's marked as active and is not in maintenance mode.
	// 2. Lists the local summary of partitions for the broker, making sure there are no leaderless or
	//    underreplicated partitions.
	// 3. Gets the cluster health and makes sure that the broker is part of the quorum by having a
	//    controller id set.
	//
	// These allow us to ensure we aren't fully coupled to the overall cluster health status,
	// which previously caused brokers to be marked as "not ready" when *any* broker in the cluster
	// was unhealthy.

	broker, err := client.Broker(ctx, brokerID)
	if err != nil {
		return false, fmt.Errorf("fetching broker status: %w", err)
	}

	// is the broker marked as active and not being decommissioned
	if broker.MembershipStatus != rpadmin.MembershipStatusActive {
		p.logger.Info("broker membership not active")
		return false, nil
	}

	status, err := client.MaintenanceStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("fetching broker maintenance status: %w", err)
	}

	// is the broker in maintenance mode and currently draining
	if status.Draining {
		p.logger.Info("broker is draining")
		return false, nil
	}

	summary, err := client.GetLocalPartitionsSummary(ctx)
	if err != nil {
		return false, fmt.Errorf("fetching broker partitions: %w", err)
	}

	// do we have any leaderless or under-replicated nodes?
	if summary.Leaderless != 0 || summary.UnderReplicated != 0 {
		p.logger.Info("broker has leaderless or under-replicated partitions", "leaderless", summary.Leaderless, "under-replicated", summary.UnderReplicated)
		return false, nil
	}

	clusterHealth, err := client.GetHealthOverview(ctx)
	if err != nil {
		return false, fmt.Errorf("fetching cluster health: %w", err)
	}

	// do we have an elected cluster leader (i.e. are we part of quorum)
	if clusterHealth.ControllerID < 0 {
		p.logger.Info("broker has no elected cluster leader")
		return false, nil
	}

	return true, nil
}

// IsClusterBrokerReady checks if a cluster broker should be servicing requests. It does
// this by issuing a request to the given broker which should get proxied through to the
// underlying Kafka API, checking that the broker is marked as alive, not in maintenance
// mode, and that we actually get a response should inform us that:
//
// 1. The Kafka API is up and servicing requests (through the broker active membership check).
// 2. The Admin API is up and servicing requests (through receiving a valid request).
// 3. We aren't currently draining in maintenance mode.
func (p *Prober) IsClusterBrokerReady(ctx context.Context, brokerURL string) (bool, error) {
	client, brokerID, err := p.getClient(ctx, brokerURL)
	if err != nil {
		return false, fmt.Errorf("initializing client to check broker readiness: %w", err)
	}
	defer client.Close()

	// unlike the above check which includes cluster quorum and partition status, this just
	// checks that a broker is up and not in maintenance mode

	broker, err := client.Broker(ctx, brokerID)
	if err != nil {
		return false, fmt.Errorf("fetching broker status: %w", err)
	}

	// is the broker marked as active and not being decommissioned
	if broker.MembershipStatus != rpadmin.MembershipStatusActive {
		return false, nil
	}

	status, err := client.MaintenanceStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("fetching broker maintenance status: %w", err)
	}

	// is the broker in maintenance mode and currently draining
	if status.Draining {
		return false, nil
	}

	return true, nil
}

func (p *Prober) getClient(ctx context.Context, brokerURL string) (*rpadmin.AdminAPI, int, error) {
	profile, err := p.readProfile()
	if err != nil {
		return nil, -1, fmt.Errorf("reading profile: %w", err)
	}

	client, err := p.factory.RedpandaAdminClient(ctx, profile)
	if err != nil {
		return nil, -1, fmt.Errorf("initializing client: %w", err)
	}
	defer client.Close()

	scopedClient, err := client.ForHost(brokerURL)
	if err != nil {
		return nil, -1, fmt.Errorf("initializing client for broker %q: %w", brokerURL, err)
	}

	config, err := scopedClient.GetNodeConfig(ctx)
	if err != nil {
		return nil, -1, fmt.Errorf("fetching node config for broker %q: %w", brokerURL, err)
	}

	return scopedClient, config.NodeID, nil
}

func (p *Prober) readProfile() (*rpkconfig.RpkProfile, error) {
	params := rpkconfig.Params{ConfigFlag: p.configPath}

	config, err := params.Load(p.fs)
	if err != nil {
		return nil, fmt.Errorf("loading profile: %w", err)
	}

	return config.VirtualProfile(), nil
}
