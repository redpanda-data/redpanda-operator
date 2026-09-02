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
	"testing"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// ClusterConfigForTesting exposes the unexported clusterConfigFor to package
// redpanda_test.
func (r *RedpandaReconciler) ClusterConfigForTesting(ctx context.Context, rp *redpandav1alpha2.Redpanda, schema rpadmin.ConfigSchema, cl cluster.Cluster) (map[string]any, []error, error) {
	return r.clusterConfigFor(ctx, rp, schema, cl)
}

func SetupTestManager(t *testing.T, ctx context.Context, cfg *rest.Config, c client.Client) multicluster.Manager {
	t.Helper()

	mgr, err := multicluster.NewSingleClusterManager(cfg, manager.Options{
		LeaderElection: false,
		NewClient: func(_ *rest.Config, _ client.Options) (client.Client, error) {
			return c, nil
		},
	})
	require.NoError(t, err)
	go mgr.Start(ctx)
	<-mgr.Elected()

	return mgr
}
