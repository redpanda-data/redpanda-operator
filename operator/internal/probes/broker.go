// Copyright 2026 Redpanda Data, Inc.
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
	"errors"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/afero"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

// ErrPreRestartProbeUnsupported is returned by IsBrokerSafeToRestart when the
// broker doesn't expose the /v1/broker/pre_restart_probe endpoint — typically
// Redpanda < 25.1. Callers should fall back to the cluster-wide health check
// when they receive this sentinel.
var ErrPreRestartProbeUnsupported = errors.New("broker pre-restart probe endpoint not available (Redpanda < 25.1)")

// ErrPostRestartProbeUnsupported is the analogous sentinel for the
// post-restart probe at /v1/broker/post_restart_probe. Both endpoints landed
// together in Redpanda 25.1, but they're queried independently — keeping a
// separate sentinel lets callers treat the two endpoints as independently-
// negotiable feature gates if a cluster ever ships one without the other.
var ErrPostRestartProbeUnsupported = errors.New("broker post-restart probe endpoint not available (Redpanda < 25.1)")

// DefaultPostRestartCaughtUpPercent is the default threshold for "this
// broker has finished its post-restart recovery": LoadReclaimedPercent must
// be at least this high for the broker to be considered caught up. 100 means
// every in-sync replica this broker hosts has been recovered to current
// state — the strictest interpretation of the RFC's "wait for post-restart
// probe" step.
const DefaultPostRestartCaughtUpPercent = 100

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

// IsBrokerSafeToRestart asks the broker whether restarting it right now is
// expected to affect partitions, via the per-broker /v1/broker/pre_restart_probe
// endpoint (Redpanda 25.1+). A broker is considered safe to restart when none
// of the dangerous risk categories are populated:
//
//   - acks1_data_loss                 (acks=1 producers may lose data)
//   - unavailable                     (produce+consume reject)
//   - full_acks_produce_unavailable   (acks=-1 produce reject)
//
// rf1_offline is treated as acceptable risk: an RF=1 topic already has no
// redundancy, and bringing those partitions offline for the duration of the
// restart is the user's stated tolerance.
//
// On clusters older than 25.1 the endpoint returns 404; this function then
// returns ErrPreRestartProbeUnsupported so the caller can fall back to the
// cluster-wide health overview.
func (p *Prober) IsBrokerSafeToRestart(ctx context.Context, brokerURL string) (bool, error) {
	client, _, err := p.getClient(ctx, brokerURL)
	if err != nil {
		return false, fmt.Errorf("initializing client to check broker restart safety: %w", err)
	}
	defer client.Close()

	result, err := client.PreRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			return false, ErrPreRestartProbeUnsupported
		}
		return false, fmt.Errorf("fetching broker pre-restart probe: %w", err)
	}

	if n := len(result.Risks.Acks1DataLoss); n > 0 {
		p.logger.Info("broker not safe to restart: would risk acks=1 data loss", "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.Unavailable); n > 0 {
		p.logger.Info("broker not safe to restart: partitions would become unavailable", "partitions", n)
		return false, nil
	}
	if n := len(result.Risks.FullAcksProduceUnavailable); n > 0 {
		p.logger.Info("broker not safe to restart: acks=-1 produce would be rejected", "partitions", n)
		return false, nil
	}
	// rf1_offline is acceptable — log for visibility but don't block.
	if n := len(result.Risks.RF1Offline); n > 0 {
		p.logger.V(1).Info("broker restart will briefly offline RF=1 partitions", "partitions", n)
	}
	return true, nil
}

// IsBrokerCaughtUp asks the broker whether it has finished its post-restart
// recovery via /v1/broker/post_restart_probe (Redpanda 25.1+). A broker is
// considered caught up when LoadReclaimedPercent is at least threshold; pass
// DefaultPostRestartCaughtUpPercent (100) for the strictest reading. The
// caller scopes this client to a specific broker via ForHost.
//
// On clusters older than 25.1 the endpoint returns 404; this function then
// returns ErrPostRestartProbeUnsupported so the caller can fall back to
// whatever pod-state signal it had previously.
func (p *Prober) IsBrokerCaughtUp(ctx context.Context, brokerURL string, threshold int) (bool, error) {
	client, _, err := p.getClient(ctx, brokerURL)
	if err != nil {
		return false, fmt.Errorf("initializing client to check post-restart recovery: %w", err)
	}
	defer client.Close()

	result, err := client.PostRestartProbe(ctx, 0)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) && httpErr.Response != nil && httpErr.Response.StatusCode == http.StatusNotFound {
			return false, ErrPostRestartProbeUnsupported
		}
		return false, fmt.Errorf("fetching broker post-restart probe: %w", err)
	}

	if result.LoadReclaimedPercent < threshold {
		p.logger.Info("broker still post-restart recovering",
			"load_reclaimed_pc", result.LoadReclaimedPercent, "threshold", threshold)
		return false, nil
	}
	return true, nil
}

// IsClusterBrokerHealthy checks that a cluster broker is up and ready to serve requests.
// It's a relaxed form of our previous readiness probe which only looked at the `IsHealthy`
// return value from the cluster health overview endpoint. This had the unfortunate effect
// of marking all brokers as "unready" when a single broker was marked as a downed node due
// to being a reflection of the overall cluster health rather than an individual broker's
// health.
//
// NOTE: this function intentionally does NOT consult /v1/broker/pre_restart_probe.
// The pre-restart probe answers "would restarting me cause partition risks?" — a
// strictly stricter signal than "am I serving traffic?". During cluster bootstrap
// (internal-topic creation, partition leader election) the probe transiently reports
// risks even though the broker is happily serving Kafka traffic. Wiring it here would
// keep pods NotReady through bootstrap and stall helm installs (#1547 first attempt).
// PreRestartProbe is the right signal for the operator's rolling-restart roll gate —
// see brokerSafeToRestart in operator/internal/controller/redpanda/redpanda_controller.go.
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
