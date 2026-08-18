// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package observability provides metric instrumentation for the operator's
// controllers. It augments the controller-runtime built-in metrics
// (controller_runtime_reconcile_*, workqueue_*) with operator-specific
// signals that flag reconcile-health problems:
//
//   - Whether a controller is reaching steady state vs spinning.
//   - The distribution of requested re-queue intervals.
//   - When each controller last reached steady state.
//   - Redpanda CR resource-state counts (v1 + v2).
//
// (The StretchCluster-level gauges and the maintenance-mode remediation
// counters live in enterprise/operator/observability, next to the
// stretch controllers and shared remediation cores that record them.)
//
// Layout of the package:
//
//   - metrics.go — single source of truth for every metric variable
//     and the init() that registers them all with controller-runtime's
//     Prometheus registry.
//   - wrapper.go — Wrap() middleware that records the per-reconcile
//     metrics around every controller invocation.
//
// Metric naming follows the existing `operator_<subsystem>_<name>`
// convention. All metric labels have closed vocabularies — no per-pod
// or per-object labels that would explode cardinality.
package observability
