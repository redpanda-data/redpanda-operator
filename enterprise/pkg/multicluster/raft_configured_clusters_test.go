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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// K8S-891 regression: in bootstrap mode, remote peers are registered with the
// runtime provider by leader routines that only run after raft leadership is
// acquired, so GetClusterNames under-reports the cluster set until every
// AddOrReplaceCluster has completed. GetConfiguredClusterNames must expose
// the full static peer topology from construction time so callers (e.g. the
// StretchCluster broker-pool view gate) can tell a genuinely single-cluster
// view apart from a not-yet-registered peer.
func TestRaftManagerConfiguredClusterNamesPrecedePeerRegistration(t *testing.T) {
	mgr, err := NewRaftRuntimeManager(&RaftConfiguration{
		Name:    "cluster-1",
		Address: "127.0.0.1:0",
		Peers: []RaftCluster{
			// No kubeconfigs: in bootstrap mode these peers are registered
			// only after leadership is acquired, which never happens in this
			// test — exactly the pre-registration window being verified.
			{Name: "cluster-1", Address: "127.0.0.1:0"},
			{Name: "cluster-3", Address: "127.0.0.1:0"},
			{Name: "cluster-2", Address: "127.0.0.1:0"},
		},
		Bootstrap: true,
		Insecure:  true,
		Logger:    logr.Discard(),
		// Never dialed: the manager is not started in this test.
		RestConfig: &rest.Config{Host: "https://127.0.0.1:1"},
	})
	require.NoError(t, err)

	// The manager was never started and never acquired leadership, so no
	// peer has been registered: the runtime provider only knows the local
	// cluster...
	require.Equal(t, []string{mcmanager.LocalCluster}, mgr.GetClusterNames())

	// ...but the configured set must already be complete (local-cluster
	// sentinel plus the sorted remote peer names, mirroring
	// GetClusterNames' shape).
	require.Equal(t, []string{mcmanager.LocalCluster, "cluster-2", "cluster-3"}, mgr.GetConfiguredClusterNames())
}
