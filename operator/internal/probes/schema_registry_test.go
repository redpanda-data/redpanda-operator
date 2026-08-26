// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package probes_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/operator/internal/probes"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

// proberForSR builds a Prober whose rpk profile points Schema Registry at
// srURL, using an in-memory filesystem the way the sidecar reads its real
// config off disk.
func proberForSR(t *testing.T, srURL string) *probes.Prober {
	t.Helper()

	fs := afero.NewMemMapFs()
	yaml := "rpk:\n"
	if srURL != "" {
		yaml += fmt.Sprintf("    schema_registry:\n        addresses:\n            - %q\n", srURL)
	} else {
		// A broker with no Schema Registry listener at all.
		yaml += "    admin_api:\n        addresses:\n            - \"127.0.0.1:9644\"\n"
	}
	require.NoError(t, afero.WriteFile(fs, "/redpanda.yaml", []byte(yaml), 0o644))

	factory := internalclient.NewRPKOnlyFactory().WithFS(fs)
	return probes.NewProber(factory, "/redpanda.yaml",
		probes.WithFS(fs), probes.WithLogger(testr.New(t)))
}

// TestSchemaRegistryReady covers the sidecar half of the roll gate: translating
// core's GET /status/ready into the (ready, configured) pair the operator's
// gate depends on. The distinction between "not ready" and "nothing to gate on"
// is the whole contract — conflating them would either stall rolls on clusters
// without Schema Registry, or roll straight through a replaying one.
func TestSchemaRegistryReady(t *testing.T) {
	ctx := context.Background()

	t.Run("caught up", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/status/ready", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		ready, configured, err := proberForSR(t, srv.URL).SchemaRegistryReady(ctx)
		require.NoError(t, err)
		assert.True(t, configured)
		assert.True(t, ready)
	})

	t.Run("still replaying", func(t *testing.T) {
		// What core returns while _schemas is catching up. This is the answer
		// that has to block the roll.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		ready, configured, err := proberForSR(t, srv.URL).SchemaRegistryReady(ctx)
		require.NoError(t, err)
		assert.True(t, configured, "a replaying SR is still an SR worth gating on")
		assert.False(t, ready)
	})

	t.Run("redpanda too old for /status/ready", func(t *testing.T) {
		// Pre-v23.1: the endpoint doesn't exist. There is no signal to gate on,
		// so this must report "not configured" rather than "not ready" — else
		// every roll on an old cluster would stall forever.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		ready, configured, err := proberForSR(t, srv.URL).SchemaRegistryReady(ctx)
		require.NoError(t, err)
		assert.False(t, configured)
		assert.False(t, ready)
	})

	t.Run("no schema registry listener", func(t *testing.T) {
		// rpk's virtual profile hands out a default SR address whether or not
		// the broker runs the listener, so absence shows up as a refused
		// connection rather than an empty address list. It must read as
		// "nothing to gate on": treating it as an error would fail the roll
		// closed forever on every cluster without Schema Registry.
		ready, configured, err := proberForSR(t, "").SchemaRegistryReady(ctx)
		require.NoError(t, err)
		assert.False(t, configured, "a broker without SR has nothing to gate on")
		assert.False(t, ready)
	})

	t.Run("timeout is an error, not a verdict", func(t *testing.T) {
		// An SR that is listening but never answers must fail closed: the
		// operator cannot tell a hung SR from a replaying one, and rolling into
		// either is what the gate exists to prevent. Distinct from a refused
		// connection, which means no listener at all.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		ready, configured, err := proberForSR(t, srv.URL).SchemaRegistryReady(ctx)
		require.Error(t, err)
		assert.True(t, configured)
		assert.False(t, ready)
	})
}
