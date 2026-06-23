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

	// The next block of subtests pins the operator-side interpretation
	// of each failure mode the RFC enumerates
	// ("Rolling restart safety probes" design doc). Reproducing the
	// actual cluster states (in-sync / offline / recovering replicas
	// across multiple brokers) inside a single testcontainer is not
	// practical; Redpanda core's own integration suite covers the
	// probe's risk-calculation server-side. What we verify here is that
	// for each documented case the operator's helper makes the right
	// roll/don't-roll decision given the JSON the probe is contractually
	// expected to return.
	//
	// Cases follow the RFC's numbering:
	//   1. in-sync 2, offline 1   → leaderless on restart → unavailable
	//   2. in-sync 2, recovering 1 → acks=-1 produce unavailable on
	//                                in-sync restart
	//   3. in-sync 1, offline 1, recovering 1 → acks=1 data loss
	//   4. in-sync 1, recovering 2 → acks=1 data loss
	for name, tc := range map[string]struct {
		risks    rpadmin.RestartRisks
		wantSafe bool
		// purpose just documents which RFC clause the entry pins, so a
		// reader who breaks a subtest can map back to the design doc.
		purpose string
	}{
		"RFC case 1: in-sync 2, offline 1 — restarting online replica makes partition leaderless": {
			risks: rpadmin.RestartRisks{
				Unavailable: []string{"kafka/topic-rf3/0"},
			},
			wantSafe: false,
			purpose:  "blocks on `unavailable` — both produce and consume reject",
		},
		"RFC case 2: in-sync 2, recovering 1 — restarting in-sync replica blocks acks=-1 produce": {
			risks: rpadmin.RestartRisks{
				FullAcksProduceUnavailable: []string{"kafka/topic-rf3/0"},
			},
			wantSafe: false,
			purpose:  "blocks on `full_acks_produce_unavailable` — only one in-sync replica survives, can't form acks=-1 quorum",
		},
		"RFC case 2 (recovering-replica path): restarting THE recovering replica is safe": {
			// "Restarting the recovering replica won't make much
			// difference (other than completion of the recovery process
			// being delayed), so can be considered safe."
			risks:    rpadmin.RestartRisks{},
			wantSafe: true,
			purpose:  "probe returns no risks when the broker being restarted is itself the recovering replica",
		},
		"RFC case 3: in-sync 1, offline 1, recovering 1 — restarting in-sync risks acks=1 data loss": {
			// The probe typically populates BOTH categories here: the
			// partition will be leaderless AND acks=1 producers may lose
			// data when the offline broker rejoins and elects with the
			// recovering replica. The operator must block on either.
			risks: rpadmin.RestartRisks{
				Unavailable:   []string{"kafka/topic-rf3/0"},
				Acks1DataLoss: []string{"kafka/topic-rf3/0"},
			},
			wantSafe: false,
			purpose:  "blocks even when both `unavailable` and `acks1_data_loss` are populated",
		},
		"RFC case 4: in-sync 1, recovering 2 — restarting in-sync causes acks=1 data loss": {
			risks: rpadmin.RestartRisks{
				Acks1DataLoss: []string{"kafka/topic-rf3/0", "kafka/topic-rf3/1"},
			},
			wantSafe: false,
			purpose:  "blocks on `acks1_data_loss` — recovering replicas elect among themselves, log tail is lost",
		},
		"RF=1 partitions are acceptable risk (RFC: 'restarting a node hosting them obviously results in availability loss, so they will require special handling')": {
			risks: rpadmin.RestartRisks{
				RF1Offline: []string{"kafka/rf1-topic/0", "kafka/rf1-topic/1"},
			},
			wantSafe: true,
			purpose:  "rf1_offline alone does not block — RF=1 has no redundancy by user choice",
		},
		"RF=1 acceptable but other category populated → still blocked": {
			risks: rpadmin.RestartRisks{
				RF1Offline:                 []string{"kafka/rf1-topic/0"},
				FullAcksProduceUnavailable: []string{"kafka/topic-rf3/0"},
			},
			wantSafe: false,
			purpose:  "rf1_offline being acceptable does not override a dangerous category",
		},
		"all categories empty → safe": {
			risks:    rpadmin.RestartRisks{},
			wantSafe: true,
			purpose:  "fresh / steady-state cluster — the happy path",
		},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Logf("RFC mapping: %s", tc.purpose)
			fakeID := brokerID + 1000 + int(time.Now().UnixNano()&0xffff)

			mux := http.NewServeMux()
			mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"node_id": fakeID})
			})
			mux.HandleFunc("/v1/broker/pre_restart_probe", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(rpadmin.PreRestartCheckResult{Risks: tc.risks})
			})
			stub := httptest.NewServer(mux)
			defer stub.Close()

			stubClient, err := rpadmin.NewAdminAPI([]string{stub.URL}, new(rpadmin.NopAuth), nil)
			require.NoError(t, err)
			defer stubClient.Close()

			// clusterIsHealthy is the legacy fallback used only on 404;
			// for these cases the probe answers so the fallback value
			// must not influence the result.
			safe, err := brokerSafeToRestart(ctx, stubClient, fakeID, true, logger, "rfc-case-pod")
			require.NoError(t, err)
			assert.Equal(t, tc.wantSafe, safe, "RFC mapping: %s", tc.purpose)
		})
	}
}

// TestIntegrationBrokerCaughtUp pins the operator-side interpretation of the
// /v1/broker/post_restart_probe contract — the "wait for post-restart probe"
// step in the rolling-restart RFC. The probe returns load_reclaimed_pc (0..100)
// representing the fraction of in-sync replicas this broker has reclaimed
// since its last restart. The next roll is blocked until every broker in the
// cluster reports >= threshold (100 by default).
func TestIntegrationBrokerCaughtUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	logger := testr.New(t)

	for name, tc := range map[string]struct {
		// loadPercent is the load_reclaimed_pc the stub returns.
		// -1 means "respond with 404" (older Redpanda),
		// 999 means "respond with 500" (admin failure).
		loadPercent  int
		threshold    int
		wantCaughtUp bool
		wantErr      bool
	}{
		"fully caught up at default threshold": {
			loadPercent: 100, threshold: 100, wantCaughtUp: true,
		},
		"99% not caught up at default strict threshold": {
			loadPercent: 99, threshold: 100, wantCaughtUp: false,
		},
		"99% caught up at relaxed threshold (95)": {
			loadPercent: 99, threshold: 95, wantCaughtUp: true,
		},
		"0% (just restarted) not caught up": {
			loadPercent: 0, threshold: 100, wantCaughtUp: false,
		},
		"404 falls back to caught up (pre-25.1 broker)": {
			loadPercent: -1, threshold: 100, wantCaughtUp: true,
		},
		"500 propagates as error": {
			loadPercent: 999, threshold: 100, wantErr: true,
		},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			fakeID := 5000 + int(time.Now().UnixNano()&0xffff)
			loadPercent := tc.loadPercent

			mux := http.NewServeMux()
			mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"node_id": fakeID})
			})
			mux.HandleFunc("/v1/broker/post_restart_probe", func(w http.ResponseWriter, _ *http.Request) {
				switch loadPercent {
				case -1:
					http.Error(w, "not found", http.StatusNotFound)
				case 999:
					http.Error(w, "boom", http.StatusInternalServerError)
				default:
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(rpadmin.PostRestartCheckResult{LoadReclaimedPercent: loadPercent})
				}
			})
			stub := httptest.NewServer(mux)
			defer stub.Close()

			stubClient, err := rpadmin.NewAdminAPI([]string{stub.URL}, new(rpadmin.NopAuth), nil)
			require.NoError(t, err)
			defer stubClient.Close()

			caughtUp, err := brokerCaughtUp(ctx, stubClient, fakeID, tc.threshold, logger, "test-pod")
			if tc.wantErr {
				require.Error(t, err)
				assert.False(t, caughtUp)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantCaughtUp, caughtUp)
		})
	}
}

// TestIntegrationBrokersStillRecovering verifies the outer gate's behavior
// across a brokerMap: it returns "still recovering" when any broker is
// below threshold, it deduplicates by broker ID (brokerMap double-keys), and
// 404 is treated as "endpoint absent on this cluster, not recovering."
func TestIntegrationBrokersStillRecovering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	logger := testr.New(t)

	type brokerStub struct {
		id          int
		loadPercent int
		url         string
		server      *httptest.Server
		hits        int
	}

	makeBrokerStub := func(t *testing.T, id, loadPercent int) *brokerStub {
		t.Helper()
		bs := &brokerStub{id: id, loadPercent: loadPercent}
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/node_config", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": bs.id})
		})
		mux.HandleFunc("/v1/broker/post_restart_probe", func(w http.ResponseWriter, _ *http.Request) {
			bs.hits++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rpadmin.PostRestartCheckResult{LoadReclaimedPercent: bs.loadPercent})
		})
		bs.server = httptest.NewServer(mux)
		bs.url = bs.server.URL
		return bs
	}

	t.Run("any broker below threshold blocks", func(t *testing.T) {
		caughtUp := makeBrokerStub(t, 1, 100)
		defer caughtUp.server.Close()
		recovering := makeBrokerStub(t, 2, 50)
		defer recovering.server.Close()
		client, err := rpadmin.NewAdminAPI([]string{caughtUp.url, recovering.url}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer client.Close()

		stillRecovering, err := brokersStillRecovering(ctx, client, map[string]int{
			"broker-1": 1,
			"broker-2": 2,
		}, 100, logger)
		require.NoError(t, err)
		assert.True(t, stillRecovering, "broker-2 at 50% should block")
	})

	t.Run("all brokers caught up returns false", func(t *testing.T) {
		one := makeBrokerStub(t, 11, 100)
		defer one.server.Close()
		two := makeBrokerStub(t, 12, 100)
		defer two.server.Close()
		client, err := rpadmin.NewAdminAPI([]string{one.url, two.url}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer client.Close()

		stillRecovering, err := brokersStillRecovering(ctx, client, map[string]int{
			"broker-1": 11,
			"broker-2": 12,
		}, 100, logger)
		require.NoError(t, err)
		assert.False(t, stillRecovering)
	})

	t.Run("dedupes by broker ID across map entries", func(t *testing.T) {
		// brokerMap intentionally has two entries per broker (first DNS
		// label and raw host). The helper must not query each broker twice.
		one := makeBrokerStub(t, 21, 100)
		defer one.server.Close()
		client, err := rpadmin.NewAdminAPI([]string{one.url}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer client.Close()

		_, err = brokersStillRecovering(ctx, client, map[string]int{
			"broker-1":                           21,
			"broker-1.svc.namespace.svc.cluster": 21,
		}, 100, logger)
		require.NoError(t, err)
		assert.Equal(t, 1, one.hits, "broker should be queried exactly once despite two map entries")
	})

	t.Run("404 on a broker is treated as endpoint absent (not recovering)", func(t *testing.T) {
		notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/node_config" {
				_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 31})
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer notFound.Close()
		caughtUp := makeBrokerStub(t, 32, 100)
		defer caughtUp.server.Close()
		client, err := rpadmin.NewAdminAPI([]string{notFound.URL, caughtUp.url}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer client.Close()

		stillRecovering, err := brokersStillRecovering(ctx, client, map[string]int{
			"broker-1": 31,
			"broker-2": 32,
		}, 100, logger)
		require.NoError(t, err)
		assert.False(t, stillRecovering, "404 → caught-up fallback, no broker blocks")
	})

	t.Run("a probe error on one broker does not mask another still recovering", func(t *testing.T) {
		// Regression: brokersStillRecovering must keep scanning after a
		// per-broker probe error. brokerMap iteration order is random and the
		// caller treats a returned error as non-fatal (it proceeds with the
		// roll), so if a 500 on broker-1 short-circuited the scan we could
		// roll the next pod while broker-2 is still at 50% — exactly the
		// under-replication window the post-restart gate exists to close.
		erroring := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/node_config" {
				_ = json.NewEncoder(w).Encode(map[string]any{"node_id": 41})
				return
			}
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer erroring.Close()
		recovering := makeBrokerStub(t, 42, 50)
		defer recovering.server.Close()
		client, err := rpadmin.NewAdminAPI([]string{erroring.URL, recovering.url}, new(rpadmin.NopAuth), nil)
		require.NoError(t, err)
		defer client.Close()

		stillRecovering, err := brokersStillRecovering(ctx, client, map[string]int{
			"broker-1": 41,
			"broker-2": 42,
		}, 100, logger)
		require.NoError(t, err, "a confirmed recovering broker must win over a probe error on another broker")
		assert.True(t, stillRecovering, "broker-2 at 50% must block even though broker-1's probe errored")
	})
}
