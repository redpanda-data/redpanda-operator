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

	"github.com/redpanda-data/common-go/rpadmin"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
	syncclusterconfig "github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
)

// This file holds the operator-side implementations of the seams defined in
// seam.go. It stays in the OSS operator when the stretch controllers move to
// the enterprise module: the enterprise controllers define the interfaces,
// this file adapts the chart-coupled machinery to them.

// OSSMulticlusterSeams populates the seam fields of params with the
// operator's implementations (admin-client factory, cluster-config syncer,
// feature flags, observability wrapper, client error classifiers) and returns
// the result. Non-seam fields of params are left untouched. factory may be
// nil (tests that never reach the admin-client paths).
func OSSMulticlusterSeams(params MulticlusterSetupParams, factory internalclient.ClientFactory) MulticlusterSetupParams {
	if factory != nil {
		params.ClientFactory = clientFactoryAdapter{factory: factory}
	}
	params.ConfigSyncer = configSyncerAdapter{}
	params.Features = OSSFeatureGate()
	params.Wrap = OSSReconcilerWrapper()
	params.IsTerminalClientError = internalclient.IsTerminalClientError
	params.IsNoRepresentativePoolError = internalclient.IsNoRepresentativePoolError
	return params
}

// OSSFeatureGate returns the FeatureGate backed by the feature package's
// annotation flags.
func OSSFeatureGate() FeatureGate {
	return featureGateAdapter{}
}

// OSSReconcilerWrapper returns the ReconcilerWrapper backed by the generic
// observability reconcile wrapper (metrics + tracing).
func OSSReconcilerWrapper() ReconcilerWrapper {
	return observability.Wrap[mcreconcile.Request]
}

type clientFactoryAdapter struct {
	factory internalclient.ClientFactory
}

var _ ClientFactory = clientFactoryAdapter{}

func (a clientFactoryAdapter) RedpandaAdminClient(ctx context.Context, sc *redpandav1alpha2.StretchCluster) (*rpadmin.AdminAPI, error) {
	return a.factory.RedpandaAdminClient(ctx, sc)
}

func (a clientFactoryAdapter) RedpandaAdminClientForStretchPod(ctx context.Context, sc *redpandav1alpha2.StretchCluster, endpoint string) (*rpadmin.AdminAPI, error) {
	return a.factory.RedpandaAdminClientForStretchPod(ctx, sc, endpoint)
}

type configSyncerAdapter struct{}

var _ ClusterConfigSyncer = configSyncerAdapter{}

func (configSyncerAdapter) Sync(ctx context.Context, admin *rpadmin.AdminAPI, mode ConfigSyncMode, desired map[string]any, superusers []string) (ClusterConfigSyncResult, error) {
	syncer := syncclusterconfig.Syncer{Client: admin, Mode: syncerModeFromConfigSyncMode(mode)}
	status, err := syncer.Sync(ctx, desired, superusers)
	if err != nil {
		return ClusterConfigSyncResult{}, err
	}
	return ClusterConfigSyncResult{
		NeedsRestart:                  status.NeedsRestart,
		PropertiesThatNeedRestartHash: status.PropertiesThatNeedRestartHash,
	}, nil
}

// syncerModeFromConfigSyncMode converts the seam enum to the syncclusterconfig
// enum. The values match 1:1 by contract (asserted in enterprise_adapters_test.go).
func syncerModeFromConfigSyncMode(mode ConfigSyncMode) syncclusterconfig.SyncerMode {
	return syncclusterconfig.SyncerMode(mode)
}

// configSyncModeFromSyncerMode converts the syncclusterconfig enum to the seam
// enum, for the FeatureGate adapter.
func configSyncModeFromSyncerMode(mode syncclusterconfig.SyncerMode) ConfigSyncMode {
	return ConfigSyncMode(mode)
}

type featureGateAdapter struct{}

var _ FeatureGate = featureGateAdapter{}

func (featureGateAdapter) V2Managed(ctx context.Context, obj client.Object) bool {
	return feature.V2Managed.Get(ctx, obj)
}

func (featureGateAdapter) RestartOnConfigChange(ctx context.Context, obj client.Object) bool {
	return feature.RestartOnConfigChange.Get(ctx, obj)
}

func (featureGateAdapter) ClusterConfigSyncMode(ctx context.Context, obj client.Object) ConfigSyncMode {
	return configSyncModeFromSyncerMode(feature.ClusterConfigSyncMode.Get(ctx, obj))
}

func (featureGateAdapter) SetDefaults(ctx context.Context, obj client.Object) bool {
	return feature.SetDefaults(ctx, feature.V2Flags, obj)
}
