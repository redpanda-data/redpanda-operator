// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/require"
)

// Regression for the StretchCluster admin-client timeout bug: before the fix,
// AdminClientForStretch silently dropped rpadmin.Opt values, so the operator's
// configured ClientTimeout never took effect on the Stretch path. Requests
// would hang for the full rpadmin default (10s) instead of the configured
// shorter value.
func TestAdminClientForStretch_AppliesClientTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	host := strings.TrimPrefix(srv.URL, "http://")

	const configuredTimeout = 200 * time.Millisecond
	// MaxRetries(1) isolates the per-attempt timeout: rpadmin's retry client
	// otherwise multiplies ClientTimeout by the retry count plus its own
	// backoff, muddying the signal. Without the fix in AdminClientForStretch
	// the opts below would be ignored entirely and the call would hang for
	// rpadmin's default 10s ClientTimeout.
	adminClient, err := AdminClientForStretch(nil, []string{host}, "", "", nil,
		rpadmin.ClientTimeout(configuredTimeout),
		rpadmin.MaxRetries(1),
	)
	require.NoError(t, err)

	start := time.Now()
	_, err = adminClient.GetFeatures(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err, "request against the hanging server must fail")
	// A pre-fix build would stall here for ~10s (the rpadmin default). The
	// 3s ceiling is generous enough for CI jitter but catches the regression.
	require.LessOrEqual(t, elapsed, 3*time.Second,
		"AdminClientForStretch did not apply the configured ClientTimeout (%s); request took %s",
		configuredTimeout, elapsed)
}
