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
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

const (
	// healthProbeInterval is how often the background probe checks each
	// cluster's API server.
	healthProbeInterval = 10 * time.Second
	// healthProbeTimeout is the per-cluster timeout for an API server probe.
	healthProbeTimeout = 5 * time.Second
)

// clusterHealthTracker periodically probes each engaged cluster's Kubernetes
// API server and caches the result. Reconcilers call IsReachable to check the
// cached status without making a network call.
type clusterHealthTracker struct {
	mu          sync.RWMutex
	reachable   map[string]bool
	getClusters func() map[string]cluster.Cluster
	logger      logr.Logger
}

func newClusterHealthTracker(logger logr.Logger, getClusters func() map[string]cluster.Cluster) *clusterHealthTracker {
	return &clusterHealthTracker{
		reachable:   make(map[string]bool),
		getClusters: getClusters,
		logger:      logger.WithName("cluster-health"),
	}
}

// IsReachable returns the cached reachability status for the named cluster.
// Unknown clusters (not yet probed or not engaged) are considered reachable
// to avoid false negatives on startup.
func (h *clusterHealthTracker) IsReachable(clusterName string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	reachable, ok := h.reachable[clusterName]
	if !ok {
		// Not probed yet — assume reachable to avoid blocking reconciliation
		// before the first probe completes.
		return true
	}
	return reachable
}

// Start runs the background probe loop. It is registered via
// LeaderManager.RegisterRoutine so it runs only on the raft leader and stops
// when leadership is lost.
func (h *clusterHealthTracker) Start(ctx context.Context) error {
	h.probeAll(ctx)

	ticker := time.NewTicker(healthProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			h.probeAll(ctx)
		}
	}
}

func (h *clusterHealthTracker) probeAll(ctx context.Context) {
	clusters := h.getClusters()

	// Probe all clusters concurrently so a single slow/hanging cluster
	// doesn't delay detection of others.
	type result struct {
		name      string
		reachable bool
	}
	results := make(chan result, len(clusters))

	var wg sync.WaitGroup
	for name, cl := range clusters {
		wg.Add(1)
		go func(name string, cl cluster.Cluster) {
			defer wg.Done()
			results <- result{name: name, reachable: h.probe(ctx, name, cl)}
		}(name, cl)
	}
	wg.Wait()
	close(results)

	h.mu.Lock()
	for r := range results {
		prev, known := h.reachable[r.name]
		h.reachable[r.name] = r.reachable
		if known && prev != r.reachable {
			if r.reachable {
				h.logger.Info("cluster became reachable", "cluster", r.name)
			} else {
				h.logger.Info("cluster became unreachable", "cluster", r.name)
			}
		}
	}

	// Remove stale entries for clusters that are no longer engaged.
	for name := range h.reachable {
		if _, ok := clusters[name]; !ok {
			delete(h.reachable, name)
		}
	}
	h.mu.Unlock()
}

// probe checks whether a single cluster's API server is reachable by fetching
// the "default" namespace via the API reader (direct call, no cache).
func (h *clusterHealthTracker) probe(ctx context.Context, name string, cl cluster.Cluster) bool {
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	var ns corev1.Namespace
	if err := cl.GetAPIReader().Get(probeCtx, client.ObjectKey{Name: "default"}, &ns); err != nil {
		h.logger.V(1).Info("cluster probe failed", "cluster", name, "error", err)
		return false
	}
	return true
}
