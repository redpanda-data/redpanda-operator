// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

// This file defines the seam between the stretch/multicluster controllers and
// the chart-coupled client/config-sync machinery in the rest of the operator.
// The operator constructs concrete implementations (see enterprise_adapters.go)
// and injects them at controller setup time, keeping the stretch controllers
// free of the chart stack so they can move to the enterprise module.

// ClientFactory is the subset of the admin-client factory that the stretch
// controllers need. The returned *rpadmin.AdminAPI is chart-free (common-go);
// only its construction is chart-coupled, hence the injection.
type ClientFactory interface {
	// RedpandaAdminClient initializes an admin client that load-balances
	// across the cluster's brokers.
	RedpandaAdminClient(ctx context.Context, sc *redpandav1alpha2.StretchCluster) (*rpadmin.AdminAPI, error)
	// RedpandaAdminClientForStretchPod initializes an admin client targeting a
	// SINGLE broker pod, identified by its admin API endpoint.
	RedpandaAdminClientForStretchPod(ctx context.Context, sc *redpandav1alpha2.StretchCluster, endpoint string) (*rpadmin.AdminAPI, error)
}

// ClusterConfigSyncResult mirrors the subset of the cluster-config sync status
// that the stretch controller consumes.
type ClusterConfigSyncResult struct {
	NeedsRestart                  bool
	PropertiesThatNeedRestartHash string
}

// ConfigSyncMode mirrors the syncclusterconfig.SyncerMode enum: how cluster
// configuration is reconciled against a running cluster. Values match that
// enum so an injected adapter can convert 1:1.
type ConfigSyncMode int

const (
	ConfigSyncModeAdditive ConfigSyncMode = iota
	ConfigSyncModeDeclarative
	ConfigSyncModeDisabled
)

// ClusterConfigSyncer applies desired cluster configuration to a running
// cluster. The concrete implementation (syncclusterconfig.Syncer) is
// chart/admin-coupled, so it is injected.
type ClusterConfigSyncer interface {
	Sync(ctx context.Context, admin *rpadmin.AdminAPI, mode ConfigSyncMode, desired map[string]any, superusers []string) (ClusterConfigSyncResult, error)
}

// FeatureGate exposes the operator feature flags the stretch controllers read.
// The concrete implementation (the feature package's annotation flags) is
// injected at setup.
type FeatureGate interface {
	// V2Managed reports whether this object is managed by the v2 controllers.
	V2Managed(ctx context.Context, obj client.Object) bool
	// RestartOnConfigChange reports whether a config-version change should
	// trigger a rolling restart.
	RestartOnConfigChange(ctx context.Context, obj client.Object) bool
	// ClusterConfigSyncMode returns the configured cluster-config sync mode.
	ClusterConfigSyncMode(ctx context.Context, obj client.Object) ConfigSyncMode
	// SetDefaults applies default feature-flag annotations to obj, returning
	// true if it mutated obj.
	SetDefaults(ctx context.Context, obj client.Object) bool
}

// ReconcilerWrapper wraps a multicluster reconciler with the operator's
// generic observability machinery (reconcile metrics + tracing). The generic
// metrics are registered in internal/observability, so the wrapper is passed
// into the controller Setup functions rather than referenced directly.
// (The StretchCluster-specific metrics, by contrast, move with the stretch
// controllers.)
type ReconcilerWrapper func(inner reconcile.TypedReconciler[mcreconcile.Request], controller string, defaultRequeueTimeout time.Duration) reconcile.TypedReconciler[mcreconcile.Request]

// applyWrap applies wrap to r, or returns r unwrapped when wrap is nil.
func applyWrap(wrap ReconcilerWrapper, r reconcile.TypedReconciler[mcreconcile.Request], controller string, defaultRequeueTimeout time.Duration) reconcile.TypedReconciler[mcreconcile.Request] {
	if wrap == nil {
		return r
	}
	return wrap(r, controller, defaultRequeueTimeout)
}

// MulticlusterSetupParams carries everything SetupMulticlusterController
// needs: the injected seam implementations plus the operator flag values that
// previously arrived as positional arguments.
type MulticlusterSetupParams struct {
	RedpandaImage lifecycle.Image
	SidecarImage  lifecycle.Image
	CloudSecrets  lifecycle.CloudSecretsFlags

	// ClientFactory may be nil in tests that never reach the admin-client
	// paths; the other seams are required (Features in particular gates every
	// reconcile pass).
	ClientFactory ClientFactory
	ConfigSyncer  ClusterConfigSyncer
	Features      FeatureGate
	// Wrap may be nil, in which case reconcilers are registered unwrapped.
	Wrap ReconcilerWrapper

	// IsTerminalClientError reports whether an admin/Kafka client error is
	// terminal (retries cannot help until the spec changes). Nil means no
	// error is treated as terminal.
	IsTerminalClientError func(error) bool
	// IsNoRepresentativePoolError reports the client factory's "no
	// representative pool" condition. Nil means never.
	IsNoRepresentativePoolError func(error) bool

	ReconcileTimeout                   time.Duration
	BrokerPodNodeUnavailableToleration time.Duration
	PostRestartCaughtUpPercent         int
	ClearMaintenanceModeAfter          time.Duration
	StaleDiskWipeNotReadyThreshold     time.Duration
}
