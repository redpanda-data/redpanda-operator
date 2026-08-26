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
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sidecarStub stands in for one pod's sidecar probe server, answering
// /schema-registry/ready with a fixed status. It listens on 127.0.0.1 so the
// gate can address it the way it addresses a real pod: by IP and port.
type sidecarStub struct {
	server *httptest.Server
	ip     string
	port   int
	hits   int
}

func newSidecarStub(t *testing.T, status int) *sidecarStub {
	t.Helper()

	stub := &sidecarStub{}
	mux := http.NewServeMux()
	// Registering only this path means an unexpected path 404s, exactly as an
	// older sidecar's ServeMux would.
	mux.HandleFunc(schemaRegistryReadyPath, func(w http.ResponseWriter, _ *http.Request) {
		stub.hits++
		w.WriteHeader(status)
	})

	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)

	host, portStr, err := net.SplitHostPort(stub.server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	stub.ip, stub.port = host, port
	return stub
}

// oldSidecarStub serves nothing at the Schema Registry path, so requests 404 —
// the shape of a sidecar predating this endpoint.
func newOldSidecarStub(t *testing.T) *sidecarStub {
	t.Helper()

	stub := &sidecarStub{}
	stub.server = httptest.NewServer(http.NewServeMux())
	t.Cleanup(stub.server.Close)

	host, portStr, err := net.SplitHostPort(stub.server.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	stub.ip, stub.port = host, port
	return stub
}

func podWithIP(name, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "redpanda"},
		Status:     corev1.PodStatus{PodIP: ip},
	}
}

// TestSchemaRegistryStillReplaying pins the gate's contract with the sidecar.
// The status codes are the interface between two separately-versioned
// components, so each branch is asserted rather than inferred.
func TestSchemaRegistryStillReplaying(t *testing.T) {
	ctx := context.Background()
	logger := testr.New(t)

	t.Run("all caught up: roll proceeds", func(t *testing.T) {
		stub := newSidecarStub(t, http.StatusOK)

		replaying, err := schemaRegistryStillReplaying(ctx,
			[]*corev1.Pod{podWithIP("broker-0", stub.ip)}, stub.port, logger)

		require.NoError(t, err)
		assert.False(t, replaying)
		assert.Equal(t, 1, stub.hits)
	})

	t.Run("one broker replaying: roll blocks", func(t *testing.T) {
		stub := newSidecarStub(t, http.StatusServiceUnavailable)

		replaying, err := schemaRegistryStillReplaying(ctx,
			[]*corev1.Pod{podWithIP("broker-0", stub.ip)}, stub.port, logger)

		require.NoError(t, err)
		assert.True(t, replaying, "a 503 from the sidecar must block the roll")
	})

	t.Run("no schema registry: roll proceeds", func(t *testing.T) {
		// 404 means "nothing to gate on" — no SR listener, or a Redpanda too
		// old to have /status/ready. A cluster without Schema Registry must
		// never be blocked by a gate that exists to protect Schema Registry.
		stub := newSidecarStub(t, http.StatusNotFound)

		replaying, err := schemaRegistryStillReplaying(ctx,
			[]*corev1.Pod{podWithIP("broker-0", stub.ip)}, stub.port, logger)

		require.NoError(t, err)
		assert.False(t, replaying)
	})

	t.Run("sidecar predating the endpoint: roll proceeds", func(t *testing.T) {
		// Operator upgraded ahead of the sidecars. The unknown path 404s, which
		// lands in the same "nothing to gate on" branch, so a mixed-version
		// fleet degrades to the old behaviour instead of stalling every roll.
		stub := newOldSidecarStub(t)

		replaying, err := schemaRegistryStillReplaying(ctx,
			[]*corev1.Pod{podWithIP("broker-0", stub.ip)}, stub.port, logger)

		require.NoError(t, err)
		assert.False(t, replaying)
	})

	t.Run("unreachable sidecar surfaces an error so the caller fails closed", func(t *testing.T) {
		// Port 1 on loopback: nothing listens, so the dial fails fast. The
		// caller defers the roll on any error — an SR we cannot reach is an SR
		// whose state we do not know.
		replaying, err := schemaRegistryStillReplaying(ctx,
			[]*corev1.Pod{podWithIP("broker-0", "127.0.0.1")}, 1, logger)

		require.Error(t, err)
		assert.False(t, replaying)
		assert.Contains(t, err.Error(), "broker-0")
	})

	t.Run("a confirmed replaying broker outranks another's error", func(t *testing.T) {
		// Map iteration is not involved here, but ordering is: an error on one
		// pod must not stop the scan, because a *confirmed* replaying broker is
		// the more actionable answer and returns cleanly rather than as an error
		// the caller can only treat as "unknown".
		replaying := newSidecarStub(t, http.StatusServiceUnavailable)

		got, err := schemaRegistryStillReplaying(ctx, []*corev1.Pod{
			podWithIP("broker-unreachable", "127.0.0.1"), // wrong port below
			podWithIP("broker-replaying", replaying.ip),
		}, replaying.port, logger)

		// broker-unreachable is probed on the replaying stub's port, which it
		// does not own, so it errors; broker-replaying then answers 503.
		require.NoError(t, err, "a confirmed replaying broker must win over an error")
		assert.True(t, got)
	})

	t.Run("unscheduled pods are skipped, not treated as replaying", func(t *testing.T) {
		stub := newSidecarStub(t, http.StatusOK)

		replaying, err := schemaRegistryStillReplaying(ctx, []*corev1.Pod{
			podWithIP("broker-pending", ""), // no IP yet
			podWithIP("broker-0", stub.ip),
		}, stub.port, logger)

		require.NoError(t, err)
		assert.False(t, replaying, "a pod with no IP has no SR to replay")
		assert.Equal(t, 1, stub.hits, "the IP-less pod must not be probed")
	})

	t.Run("no pods: nothing to wait for", func(t *testing.T) {
		replaying, err := schemaRegistryStillReplaying(ctx, nil, 8093, logger)

		require.NoError(t, err)
		assert.False(t, replaying)
	})
}
