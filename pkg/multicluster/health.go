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
	// cluster's API server. Tuned to keep partition detection inside a
	// ~4s ceiling: worst-case detect time is interval + probe timeout.
	healthProbeInterval = 2 * time.Second
	// healthProbeTimeout is the per-cluster timeout for an API server
	// probe. Healthy apiservers respond in well under 100ms; 2s leaves
	// generous headroom for transient network slowness without letting a
	// truly-partitioned peer linger.
	healthProbeTimeout = 2 * time.Second
)

// clusterHealthTracker periodically probes each engaged cluster's Kubernetes
// API server and caches the result. Reconcilers call IsReachable to check the
// cached status without making a network call.
type clusterHealthTracker struct {
	mu          sync.RWMutex
	reachable   map[string]bool
	getClusters func() map[string]cluster.Cluster
	logger      logr.Logger
	// onReachable, if set, is invoked once per unreachable→reachable transition
	// detected by the probe loop. It runs in the probeAll goroutine, so callers
	// must keep work cheap (typically a channel send / broadcaster notify).
	onReachable func(clusterName string)
}

func newClusterHealthTracker(logger logr.Logger, getClusters func() map[string]cluster.Cluster) *clusterHealthTracker {
	return &clusterHealthTracker{
		reachable:   make(map[string]bool),
		getClusters: getClusters,
		logger:      logger.WithName("cluster-health"),
	}
}

// SetOnReachable installs the callback fired when a cluster transitions from
// unreachable to reachable. Used to wake the leaderRunnable's engage loop so
// it retries engagement on healed peers without polling.
func (h *clusterHealthTracker) SetOnReachable(fn func(clusterName string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onReachable = fn
}

// IsReachable returns the cached reachability status for the named cluster.
// Unknown clusters (not yet probed) are reported as UNREACHABLE: this is the
// conservative choice during the brief startup window between leader election
// and the first probeAll completing. The alternative (default-reachable) lets
// the first reconcile race past the probe and dial a partitioned peer, which
// then hangs at the kernel TCP-retry budget (~30–90s) and blows the partition
// SLA. The unreachable→reachable transition callback re-engages each cluster
// the moment its first probe succeeds, so default-unreachable only delays
// healthy clusters by one probe interval (~5s).
//
// Local cluster (empty string) is always treated as reachable — the operator
// can always talk to its own apiserver, and the probe loop intentionally
// doesn't include it.
func (h *clusterHealthTracker) IsReachable(clusterName string) bool {
	if clusterName == "" {
		return true
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	reachable, ok := h.reachable[clusterName]
	if !ok {
		// Not probed yet — assume unreachable so callers skip rather than
		// dial a peer whose state we don't know. Log the miss with the
		// current key set so a partition-time hang can be traced back to a
		// name mismatch between the caller and the probe loop (the latter
		// keys off whatever getClusters() returned at probe time).
		knownKeys := make([]string, 0, len(h.reachable))
		for k := range h.reachable {
			knownKeys = append(knownKeys, k)
		}
		h.logger.V(1).Info("reachability lookup miss; defaulting to unreachable",
			"queryClusterName", clusterName,
			"knownKeys", knownKeys)
		return false
	}
	h.logger.V(2).Info("reachability lookup hit",
		"queryClusterName", clusterName,
		"reachable", reachable)
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

	// Each probe goroutine writes its own result to h.reachable as soon as
	// it finishes — we don't aggregate results and then update once at the
	// end. That matters because a single slow probe (unreachable peer at
	// the kernel TCP-retry budget, ~30s) would otherwise gate the map
	// update for every other cluster in the same round. Healthy peers
	// should be queryable as reachable within their own probe latency, not
	// the slowest peer's.
	var wg sync.WaitGroup
	for name, cl := range clusters {
		wg.Add(1)
		go func(name string, cl cluster.Cluster) {
			defer wg.Done()
			reachable := h.probe(ctx, name, cl)
			h.recordResult(name, reachable)
		}(name, cl)
	}
	wg.Wait()

	// Once every probe has reported, prune entries for clusters that are no
	// longer engaged. This runs after wg.Wait so we don't race a delete
	// against a still-in-flight probe for the same name.
	h.mu.Lock()
	for name := range h.reachable {
		if _, ok := clusters[name]; !ok {
			delete(h.reachable, name)
		}
	}
	h.mu.Unlock()
}

// recordResult writes one probe's outcome to the reachability map and fires
// the onReachable callback on any new positive observation. Called once per
// probe goroutine so fast probes don't wait on slow ones.
//
// The callback fires in two cases: an explicit unreachable→reachable
// transition (recovery), and the very first time a previously-unknown
// cluster is observed reachable. The latter matters because with the
// "unknown defaults to unreachable" IsReachable policy, the engage path
// initially skips every cluster — without firing onReachable on the
// first positive probe the engage cycle never wakes for clusters that
// were healthy all along.
func (h *clusterHealthTracker) recordResult(name string, reachable bool) {
	h.mu.Lock()
	prev, known := h.reachable[name]
	h.reachable[name] = reachable
	transitionedUp := reachable && (!known || !prev)
	if known && prev != reachable {
		if reachable {
			h.logger.Info("cluster became reachable", "cluster", name)
		} else {
			h.logger.Info("cluster became unreachable", "cluster", name)
		}
	} else if !known && reachable {
		h.logger.Info("cluster first observed reachable", "cluster", name)
	}
	onReachable := h.onReachable
	h.mu.Unlock()

	if transitionedUp && onReachable != nil {
		onReachable(name)
	}
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
