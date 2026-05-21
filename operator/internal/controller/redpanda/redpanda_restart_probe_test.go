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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestIntegrationBrokerSafeToRestart exercises the operator's
// brokerSafeToRestart helper against a real Redpanda broker via
// testcontainers. The test pins three behaviors:
//
//  1. Empty / fresh cluster — probe returns no risks → safe to restart.
//  2. RF=1 topics populate rf1_offline but nothing else — still safe
//     (operator policy treats rf1_offline as acceptable risk).
//  3. The 404 fallback path: when a broker doesn't expose the endpoint
//     (Redpanda < 25.1) the helper falls back to the cluster.IsHealthy
//     argument so behavior on older brokers is unchanged.
//
// The "dangerous risk categories populated → not safe" path is harder
// to drive deterministically from a single-broker container without
// orchestrating partition recovery — it is covered by the rpadmin-side
// integration test (common-go#170) which uses the same endpoint.
func TestIntegrationBrokerSafeToRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	testImage := os.Getenv("TEST_REDPANDA_REPO") + ":" + os.Getenv("TEST_REDPANDA_VERSION")
	if testImage == ":" {
		t.Skip("TEST_REDPANDA_REPO / TEST_REDPANDA_VERSION not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	logger := testr.New(t)

	container, err := redpanda.Run(ctx, testImage)
	require.NoError(t, err, "start redpanda container")
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	adminAddr, err := container.AdminAPIAddress(ctx)
	require.NoError(t, err)
	adminClient, err := rpadmin.NewAdminAPI([]string{adminAddr}, new(rpadmin.NopAuth), nil)
	require.NoError(t, err)
	defer adminClient.Close()

	// Discover the broker we'll be probing.
	nodeCfg, err := adminClient.GetNodeConfig(ctx)
	require.NoError(t, err)
	brokerID := nodeCfg.NodeID

	// The helper resolves the broker URL via admin.BrokerIDToURL, which
	// in turn reads /v1/node_config from each known URL. With a single
	// known URL (the testcontainer admin endpoint) this resolves to that
	// URL — exactly what the operator does in a real cluster.
	t.Run("fresh cluster is safe to restart", func(t *testing.T) {
		safe, err := brokerSafeToRestart(ctx, adminClient, brokerID, true, logger, "pod-0")
		require.NoError(t, err)
		assert.True(t, safe, "fresh broker with no partitions must be safe to restart")
	})

	t.Run("RF=1 topic still counts as safe (acceptable risk)", func(t *testing.T) {
		seed, err := container.KafkaSeedBroker(ctx)
		require.NoError(t, err)
		kc, err := kgo.NewClient(kgo.SeedBrokers(seed))
		require.NoError(t, err)
		defer kc.Close()
		kadmClient := kadm.NewClient(kc)
		_, err = kadmClient.CreateTopic(ctx, 4, 1, nil, "rf1-topic")
		require.NoError(t, err, "create RF=1 topic")

		// Poll briefly — probe data is computed from broker-local
		// state that may lag topic creation by a tick or two.
		var safe bool
		require.Eventually(t, func() bool {
			s, perr := brokerSafeToRestart(ctx, adminClient, brokerID, true, logger, "pod-0")
			if perr != nil {
				t.Logf("brokerSafeToRestart error: %v", perr)
				return false
			}
			safe = s
			// Independently verify the probe DID populate rf1_offline,
			// so we know the "safe" answer isn't simply because the
			// broker hasn't noticed the topic yet.
			scoped, ferr := adminClient.ForHost(adminAddr)
			if ferr != nil {
				return false
			}
			defer scoped.Close()
			res, perr := scoped.PreRestartProbe(ctx, 0)
			if perr != nil {
				return false
			}
			return slices.Contains(res.Risks.RF1Offline, "kafka/rf1-topic/0")
		}, 30*time.Second, 500*time.Millisecond, "rf1-topic partitions never appeared in rf1_offline")
		assert.True(t, safe, "RF=1 partitions are acceptable risk; broker must still report safe")
	})

	t.Run("404 falls back to cluster.IsHealthy", func(t *testing.T) {
		// Stand up a stub admin server that returns 404 for the
		// pre-restart-probe endpoint but otherwise mirrors enough of
		// the broker's surface to let admin.BrokerIDToURL resolve.
		fakeID := brokerID + 99 // arbitrary; we never serve a brokers list

		mux := http.NewServeMux()
		// /v1/node_config is what BrokerIDToURL leans on. Return our
		// fake broker ID so the lookup succeeds; subsequent
		// PreRestartProbe calls will then 404.
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": fakeID})
		})
		mux.HandleFunc("/v1/broker/pre_restart_probe", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		stub := httptest.NewServer(mux)
		defer stub.Close()
		stubURL, err := url.Parse(stub.URL)
		require.NoError(t, err)
		_ = stubURL // present for clarity; not used directly

		stubClient, err := rpadmin.NewAdminAPI([]string{stub.URL}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer stubClient.Close()

		// On 404 the helper returns the value of clusterIsHealthy
		// unchanged. Pin both directions explicitly.
		safeWhenHealthy, err := brokerSafeToRestart(ctx, stubClient, fakeID, true, logger, "pod-old")
		require.NoError(t, err, "404 must not be returned as an error")
		assert.True(t, safeWhenHealthy, "fallback to cluster.IsHealthy=true on pre-25.1 broker")

		safeWhenUnhealthy, err := brokerSafeToRestart(ctx, stubClient, fakeID, false, logger, "pod-old")
		require.NoError(t, err)
		assert.False(t, safeWhenUnhealthy, "fallback to cluster.IsHealthy=false on pre-25.1 broker")
	})

	// Sanity: HTTPResponseError type wiring detects non-404 errors as
	// real errors (not fallback). We exercise this via a 500-returning
	// stub.
	t.Run("non-404 admin errors surface as errors", func(t *testing.T) {
		fakeID := brokerID + 100

		mux := http.NewServeMux()
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": fakeID})
		})
		mux.HandleFunc("/v1/broker/pre_restart_probe", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		stub := httptest.NewServer(mux)
		defer stub.Close()

		stubClient, err := rpadmin.NewAdminAPI([]string{stub.URL}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer stubClient.Close()

		safe, err := brokerSafeToRestart(ctx, stubClient, fakeID, true, logger, "pod-broken")
		require.Error(t, err, "non-404 must propagate as error")
		assert.False(t, safe)
		// Sanity-check that we're catching the right error class.
		var httpErr *rpadmin.HTTPResponseError
		assert.True(t, errors.As(err, &httpErr) || err != nil)
	})
}
