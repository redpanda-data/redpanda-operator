// Copyright 2025 Redpanda Data, Inc.
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

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

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
