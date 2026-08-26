// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package schemaregistry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"
)

// testBrokers builds one single-URL Broker per given base URL, with a short
// HTTP timeout so a blocking /status/ready handler (the "store still
// replaying" signal) fails fast in tests.
func testBrokers(t *testing.T, urls ...string) []Broker {
	t.Helper()
	brokers := make([]Broker, 0, len(urls))
	for _, u := range urls {
		client, err := sr.NewClient(
			sr.URLs(u),
			sr.HTTPClient(&http.Client{Timeout: 150 * time.Millisecond}),
		)
		require.NoError(t, err)
		brokers = append(brokers, Broker{URL: u, Client: client})
	}
	return brokers
}

// readyServer answers 200 on /status/ready, mirroring a broker whose SR
// store has caught up on _schemas.
func readyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// replayingServer blocks on /status/ready until the request is canceled,
// mirroring core's read_sync() await while the SR store replays _schemas.
func replayingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSynced(t *testing.T) {
	ctx := t.Context()
	logger := testr.New(t)

	t.Run("all brokers caught up", func(t *testing.T) {
		a, b := readyServer(t), readyServer(t)
		synced, err := Synced(ctx, testBrokers(t, a.URL, b.URL), logger)
		require.NoError(t, err)
		assert.True(t, synced)
	})

	t.Run("one broker still replaying is not synced", func(t *testing.T) {
		// The healthy broker first: the probe must not stop at a passing
		// broker — every broker gets its own verdict.
		healthy, replaying := readyServer(t), replayingServer(t)
		synced, err := Synced(ctx, testBrokers(t, healthy.URL, replaying.URL), logger)
		require.Error(t, err)
		assert.False(t, synced)
	})

	t.Run("restarting broker (connection refused) is not synced", func(t *testing.T) {
		healthy := readyServer(t)
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		downURL := down.URL
		down.Close()

		synced, err := Synced(ctx, testBrokers(t, healthy.URL, downURL), logger)
		require.Error(t, err)
		assert.False(t, synced)
	})

	t.Run("non-200 is not synced", func(t *testing.T) {
		unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(unavailable.Close)

		synced, err := Synced(ctx, testBrokers(t, unavailable.URL), logger)
		require.Error(t, err)
		assert.False(t, synced)
	})
}

func TestPerBrokerClients(t *testing.T) {
	aggregate, err := sr.NewClient(sr.URLs("http://a:8081", "http://b:8081"))
	require.NoError(t, err)

	brokers, err := PerBrokerClients(aggregate)
	require.NoError(t, err)
	require.Len(t, brokers, 2)

	for i, expected := range []string{"http://a:8081", "http://b:8081"} {
		assert.Equal(t, expected, brokers[i].URL)
		// Each scoped client must hold exactly its own URL — otherwise
		// sr.Client's internal multi-URL failover would turn a per-broker
		// verdict into an any-broker verdict.
		scopedURLs, _ := brokers[i].Client.OptValue(sr.URLs).([]string)
		assert.Equal(t, []string{expected}, scopedURLs)
	}
}
