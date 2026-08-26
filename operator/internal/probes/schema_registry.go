// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package probes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"syscall"

	rpkconfig "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
)

// schemaRegistryReadyPath is core's readiness endpoint on the Schema Registry
// listener. Added in redpanda#7080 and present since v23.1, it blocks on
// writer().read_sync() — so a 200 means this broker has finished replaying
// _schemas and its store is consistent, which is exactly the signal the
// operator's roll gate needs. It is served at auth::level::publik and so takes
// no credentials.
const schemaRegistryReadyPath = "/status/ready"

// SchemaRegistryReady reports whether this broker's Schema Registry has caught
// up on _schemas.
//
// configured is false when the broker has no Schema Registry listener at all,
// which callers must treat as "nothing to gate on" rather than "not ready" — a
// cluster running without Schema Registry has to keep rolling.
//
// Why this lives in the sidecar rather than in the operator: the check has to be
// per broker, and the sidecar is already inside the pod. Reaching a specific
// broker's SR listener from outside would mean resolving per-pod addresses and
// reproducing that listener's TLS, while from here it is the local endpoint the
// rpk profile already describes.
func (p *Prober) SchemaRegistryReady(ctx context.Context) (ready bool, configured bool, err error) {
	params := rpkconfig.Params{ConfigFlag: p.configPath}

	cfg, err := params.Load(p.fs)
	if err != nil {
		return false, false, fmt.Errorf("loading rpk config: %w", err)
	}

	profile := cfg.VirtualProfile()
	if profile == nil {
		return false, false, nil
	}
	// NB: do NOT test len(profile.SR.Addresses) == 0 to decide whether Schema
	// Registry exists. rpk's virtual profile always supplies a default SR
	// address (127.0.0.1:8081) whether or not the broker runs the listener, so
	// that check never fires and a cluster without Schema Registry would fall
	// through to a connection error below — which the operator fails closed on,
	// stalling every roll forever. Absence is detected from ECONNREFUSED
	// instead; see the error handling around client.Do.

	client, urls, err := p.factory.SchemaRegistryStatusClientForRPKProfile(profile)
	if err != nil {
		return false, true, fmt.Errorf("building schema registry status client: %w", err)
	}
	if len(urls) == 0 {
		return false, false, nil
	}

	// The profile lists this broker's own listener, so the first URL is the
	// local one. Probing more than one would answer a question about a
	// different broker, which the operator asks that broker's own sidecar.
	target, err := url.JoinPath(urls[0], schemaRegistryReadyPath)
	if err != nil {
		return false, true, fmt.Errorf("building schema registry ready URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false, true, fmt.Errorf("building schema registry ready request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Nothing listening on the Schema Registry port means the broker does
		// not run the listener: there is nothing to gate on, and reporting an
		// error here would fail the roll closed forever on every cluster that
		// runs without Schema Registry.
		//
		// This is deliberately narrow. A replaying SR *is* listening — it
		// answers 503/50005 — so a refused connection cannot be a replay. Other
		// failures (TLS, timeout) stay errors, because an SR that is listening
		// but unanswerable is a state the roll must not walk into.
		if errors.Is(err, syscall.ECONNREFUSED) {
			p.logger.V(1).Info("nothing listening on the schema registry port; treating as nothing to gate on", "url", target)
			return false, false, nil
		}
		return false, true, fmt.Errorf("requesting %s: %w", target, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
		return true, true, nil
	case resp.StatusCode == http.StatusNotFound:
		// Redpanda older than v23.1 has no /status/ready. There is no signal to
		// gate on, so report "not configured" and let the caller proceed rather
		// than stalling every roll on an old cluster.
		p.logger.V(1).Info("schema registry has no /status/ready endpoint; treating as nothing to gate on", "url", target)
		return false, false, nil
	case resp.StatusCode >= 500:
		// 503 while replaying is the expected answer, and the whole point.
		return false, true, nil
	default:
		return false, true, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, target)
	}
}
