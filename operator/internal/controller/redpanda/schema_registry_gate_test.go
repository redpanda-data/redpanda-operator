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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/sr"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
)

// gateBrokers builds one single-URL schemaregistry.Broker per given base
// URL, with a short HTTP timeout so a blocking /status/ready handler (the
// "store still replaying" signal) fails fast in tests. The probe engine
// itself is tested in pkg/client/schemaregistry; these tests cover the
// roll-gate POLICY layered on top of it.
func gateBrokers(t *testing.T, urls ...string) []schemaregistry.Broker {
	t.Helper()
	brokers := make([]schemaregistry.Broker, 0, len(urls))
	for _, u := range urls {
		client, err := sr.NewClient(
			sr.URLs(u),
			sr.HTTPClient(&http.Client{Timeout: 150 * time.Millisecond}),
		)
		require.NoError(t, err)
		brokers = append(brokers, schemaregistry.Broker{URL: u, Client: client})
	}
	return brokers
}

// stubBrokerClientsFactory implements only SchemaRegistryBrokerClients; the
// embedded nil interface panics on any other method, which the gate must
// never call.
type stubBrokerClientsFactory struct {
	internalclient.ClientFactory
	brokers []schemaregistry.Broker
	err     error
}

func (s stubBrokerClientsFactory) SchemaRegistryBrokerClients(context.Context, any) ([]schemaregistry.Broker, error) {
	return s.brokers, s.err
}

func TestSchemaRegistryReady(t *testing.T) {
	ctx := t.Context()
	logger := testr.New(t)

	t.Run("disabled schema registry skips the gate", func(t *testing.T) {
		ready := schemaRegistryReady(ctx, stubBrokerClientsFactory{err: schemaregistry.ErrDisabled}, nil, logger)
		assert.True(t, ready)
	})

	t.Run("client construction failure fails closed", func(t *testing.T) {
		ready := schemaRegistryReady(ctx, stubBrokerClientsFactory{err: assert.AnError}, nil, logger)
		assert.False(t, ready)
	})

	t.Run("synced brokers satisfy the gate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		ready := schemaRegistryReady(ctx, stubBrokerClientsFactory{brokers: gateBrokers(t, srv.URL)}, nil, logger)
		assert.True(t, ready)
	})

	t.Run("replaying broker defers", func(t *testing.T) {
		// Mirrors core's read_sync() await: the handler blocks until the
		// probe times out.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		}))
		t.Cleanup(srv.Close)
		ready := schemaRegistryReady(ctx, stubBrokerClientsFactory{brokers: gateBrokers(t, srv.URL)}, nil, logger)
		assert.False(t, ready)
	})
}
