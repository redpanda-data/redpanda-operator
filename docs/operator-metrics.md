# Operator Metrics

The Redpanda operator exposes Prometheus metrics on its `/metrics` endpoint.
The endpoint is enabled by `--metrics-bind-address` (default `:8443` in the
chart) and gated by controller-runtime's authentication filter — scrapers
need either a Bearer token recognised by the operator's apiserver or a
client cert.

This document is the canonical inventory of the metrics the operator emits.
It covers four groups:

1. **controller-runtime built-ins** — exported automatically by every
   controller registered through `ctrl.NewControllerManagedBy(mgr)`. Useful
   for reconcile rate, errors, queue depth, and worker pool saturation.
2. **operator reconcile-health metrics** — added by the operator on top of
   the built-ins to surface self-triggered loops, generation drift, and
   non-determinism in spec-rendering.
3. **resource-state metrics** — per-CR gauges describing what the operator
   is managing (counts of Redpanda CRs, desired vs ready broker counts,
   misconfiguration reasons). Mirrors the v1 cluster metrics for the v2
   Redpanda CRD.
4. **multicluster raft metrics** — exposed only by the multicluster
   operator (`operator multicluster ...`). Covers the cross-cluster
   leader-election layer. Documented separately in
   `docs/multicluster-operator.md` (see "raft metrics" section there); a
   summary is included here for completeness.

## How to scrape

The operator chart ships an opt-in ServiceMonitor (`monitoring.enabled=true`)
and an opt-in PrometheusRule (`monitoring.rulesEnabled=true`). Both consume
the standard kube-prometheus-stack and require the
`monitoring.coreos.com/v1` CRDs to be installed.

If you are not running Prometheus Operator, scrape directly with a
`prometheus.yml` job similar to:

```yaml
scrape_configs:
- job_name: redpanda-operator
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names: [redpanda-operator]
  relabel_configs:
  - source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_name]
    regex: operator
    action: keep
  - source_labels: [__meta_kubernetes_pod_container_port_name]
    regex: https
    action: keep
  scheme: https
  bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
  tls_config:
    insecure_skip_verify: true
```

Whichever ServiceAccount is doing the scraping needs `get` on
`nonResourceURLs: ["/metrics"]`. The operator chart binds its own
ServiceAccount to a `metrics-reader` ClusterRole automatically; other
scrapers need their own binding.

## Label cardinality

Every label used by operator-emitted metrics has a closed vocabulary. No
metric exposes a per-pod, per-namespace, or per-object label that grows
unbounded. The labels you will see:

| Label | Cardinality | Source |
|-------|-------------|--------|
| `controller` | Small (one per registered controller, ~5-10) | The wrap-time identifier passed to `observability.Wrap(...)` |
| `kind` | Small (one per kind a controller manages) | The string passed to `RecordObservedGeneration` |
| `result` | Closed (`success` / `error` / `requeue` / `requeue_after`) | controller-runtime built-in |
| `name` | Small (one per workqueue, == one per controller) | controller-runtime built-in |
| `state` | Closed (`leader` / `follower` / `candidate` / `pre_candidate` / `unknown`) | multicluster raft |
| `msg_type` | Closed (~10 raft message types) | multicluster raft |
| `peer` | Small (one per stretch member, ~3) | multicluster raft |
| `error_type` | Closed (`timeout` / `canceled` / `unavailable` / `auth` / `marshal` / `other`) | multicluster raft |
| `result` (raft send) | Closed (`ok` / `error`) | multicluster raft |
| `le` | Closed (histogram buckets) | Standard Prometheus histogram |

## Group 1: controller-runtime built-ins

All of these are emitted automatically. They are the standard
controller-runtime metrics and the primary signal for "is each
controller making progress".

| Metric | Type | Labels | What it tells you |
|--------|------|--------|-------------------|
| `controller_runtime_reconcile_total` | Counter | `controller`, `result` | Reconcile invocations. `result` is one of `success`, `error`, `requeue`, `requeue_after`. |
| `controller_runtime_reconcile_errors_total` | Counter | `controller` | Errored reconciles. Sustained rate = something is broken. |
| `controller_runtime_reconcile_time_seconds` | Histogram | `controller` | Reconcile duration distribution. p99 climbing = controller is slowing down. |
| `controller_runtime_active_workers` | Gauge | `controller` | Currently-busy workers. Pegged at `max_concurrent_reconciles` = saturation. |
| `controller_runtime_max_concurrent_reconciles` | Gauge | `controller` | Configured worker pool ceiling. |
| `workqueue_depth` | Gauge | `name` | Items waiting to be reconciled per controller. |
| `workqueue_adds_total` | Counter | `name` | Enqueues. |
| `workqueue_queue_duration_seconds` | Histogram | `name` | Time items spend in the queue before being picked up. |
| `workqueue_unfinished_work_seconds` | Gauge | `name` | Longest-running item. Climbing past expected reconcile time = stuck item. |
| `workqueue_retries_total` | Counter | `name` | Re-queues. Climbing = controller can't make progress on something. |
| `workqueue_work_duration_seconds` | Histogram | `name` | Time spent processing each item. |

**Suggested alerts**:

- **Errored reconciles**: `sum by (controller) (rate(controller_runtime_reconcile_errors_total[5m])) > 0.1` for 5m → warning.
- **Worker pool saturated**: `controller_runtime_active_workers >= controller_runtime_max_concurrent_reconciles` for 10m → warning.

Both are emitted by the PrometheusRule when `monitoring.rulesEnabled=true`.

## Group 2: operator reconcile-health metrics

These are added by `operator/internal/observability/`. The wrapping
middleware (`observability.Wrap(...)`) emits the result-driven ones for
every controller; per-object metrics are recorded by individual controllers
that opt into the `Record*` helpers.

### Wrapper-emitted (free for every controller)

| Metric | Type | Labels | What it tells you |
|--------|------|--------|-------------------|
| `operator_controller_reconcile_steady_state_total` | Counter | `controller` | Reconciles that returned `(Result{}, nil)` — no work to do, no re-queue requested. Healthy controllers see this dominate once the system is converged. A controller whose `reconcile_total` rate is high while `steady_state_total` rate is flat is spinning. |
| `operator_controller_reconcile_requeue_after_seconds` | Histogram | `controller` | Distribution of `Result.RequeueAfter` durations. A tight cluster of sub-second values is a strong signal of a tight retry loop. |

### Recorder-emitted (opt-in per controller)

| Metric | Type | Labels | Recorded via | What it tells you |
|--------|------|--------|--------------|-------------------|
| `operator_controller_reconcile_observed_generation_drift` | Gauge | `controller`, `kind` | `observability.RecordObservedGeneration(controller, kind, gen, observedGen)` | `metadata.generation` minus `status.observedGeneration` at the end of a reconcile. Sustained non-zero = controller is behind on the resource's spec. |
| `operator_controller_reconcile_spec_hash_changed_without_generation_total` | Counter | `controller`, `kind` | `observability.RecordSpecHashChangedWithoutGeneration(controller, kind)` | Increments when a spec update was a no-op from the API server's perspective but its rendered hash differed from the previous run. Almost always non-determinism in the reconciler. |

The opt-in helpers are intentionally **passive** — they don't fetch
anything; the caller passes the values it already has. This keeps the
observability layer free of redundant API calls and lets each controller
decide whether it cares enough to record.

**Suggested alerts** (all shipped in the chart's PrometheusRule):

- **Runaway reconcile rate**: `operator:reconcile_rate:5m > 5` for 5m → warning. Cross-check `operator_controller_reconcile_steady_state_total` — if it is flat while the rate is high, the controller is spinning.
- **Reconcile stalled**: a controller that was active in the last hour but has reconciled zero times in the past 10 minutes → warning.
- **Observed-generation drift**: `operator_controller_reconcile_observed_generation_drift > 0` for 5m → warning.
- **Non-deterministic spec**: `rate(operator_controller_reconcile_spec_hash_changed_without_generation_total[10m]) > 0` for 10m → warning.

### Reserved (currently silent)

These metric names are reserved for diagnostics that are not yet wired
up. They appear in the inventory so dashboards and alerts that reference
them won't break once they start emitting samples.

| Metric | Description |
|--------|-------------|
| `operator_controller_reconcile_self_triggered_total` | Reconciles whose only effect was a write to the same object that re-enqueued the reconcile. A signal of self-triggering loops; requires a per-reconcile hash comparison the wrapper doesn't yet perform. |
| `operator_controller_reconcile_time_since_last_success_seconds` | Time since the last successful reconcile for a given resource. Pending a decision on label cardinality (per-request would be unbounded; aggregating to `oldest_unfinished_seconds{controller}` is the likely shape). |

## Group 3: resource-state metrics

These gauges describe the CRs the operator is managing (not the operator's
own reconcile behaviour). They are emitted by reconcilers dedicated to
metric-export — separate from the reconcilers that actually mutate
resources — so the gauges are recomputed from a fresh `List` on every
reconcile event rather than relying on per-mutation Inc/Dec calls.

### v2 Redpanda CRs (`--enable-redpanda-controllers=true`, the default)

Emitted by `operator/internal/controller/redpanda/metric_controller.go`'s
`RedpandaMetricsReconciler`. Registered against the local manager; the
Redpanda CRD only lives on the local cluster, so the reconciler is a
single namespace-scoped `List` rather than a per-cluster iteration.

| Metric | Type | Labels | What it tells you |
|--------|------|--------|-------------------|
| `redpandas` | Gauge | — | Number of Redpanda CRs the operator is managing. |
| `redpanda_desired_nodes` | GaugeVec | `namespace`, `name` | Desired broker count per Redpanda (sum across pools). |
| `redpanda_ready_nodes` | GaugeVec | `namespace`, `name` | Ready broker count per Redpanda (sum across pools). |
| `redpanda_misconfigured_clusters` | GaugeVec | `reason` | Count of Redpandas whose `ConfigurationApplied` condition is not `True`, grouped by `reason`. |

The `namespace` label is included on the per-Redpanda gauges because v2
Redpanda CR names can collide across namespaces.

### v1 Cluster CRs (`--enable-vectorized-controllers=true`)

Emitted by `operator/internal/controller/vectorized/metric_controller.go`'s
`ClusterMetricController`. The v1 equivalent of the v2 reconciler above —
predates the convention that `_total` is for counters, so the gauges
carry the `_total` suffix.

| Metric | Type | Labels | Notes |
|--------|------|--------|-------|
| `redpanda_clusters_total` | Gauge | — | Pre-dates the `_total`-is-counters convention. |
| `desired_redpanda_nodes_total` | GaugeVec | `cluster_name` | Per v1 Cluster CR. |
| `actual_redpanda_nodes_total` | GaugeVec | `cluster_name` | Per v1 Cluster CR. |
| `redpanda_misconfigured_clusters_total` | GaugeVec | `reason` | Count of v1 Clusters whose `Configured` condition is not `True`. |

The v1 and v2 metrics never share names, so running both controller modes
side-by-side during a migration is safe.

### StretchCluster member status

Emitted only by the multicluster operator (`operator multicluster ...`).
Recorded by the `MulticlusterReconciler` itself, drawing on data it
already computes: reachability from the multicluster manager's
background probe, broker counts from the per-cluster NodePool fetch,
spec drift from the existing `checkSpecConsistency` routine, and
replication health from the admin API call that
`reconcileDecommission` already makes.

| Metric | Type | Labels | What it tells you |
|--------|------|--------|-------------------|
| `operator_stretchcluster_member_reachable` | GaugeVec | `stretchcluster`, `member` | 0/1 — is each member cluster reachable from this operator. Set to 1 for the local cluster always; set for every reachable peer and to 0 for unreachable peers; unreachable peers also keep the previous `spec_drift` value because we genuinely don't know. |
| `operator_stretchcluster_brokers` | GaugeVec | `stretchcluster`, `member` | Desired broker count per member, summed across NodePools pointing at that member. |
| `operator_stretchcluster_brokers_ready` | GaugeVec | `stretchcluster`, `member` | Ready broker count per member (sum of `NodePool.status.readyReplicas`). `brokers - brokers_ready > 0` means a partial outage on that member. |
| `operator_stretchcluster_replication_health` | GaugeVec | `stretchcluster` | 0/1 — cluster-wide replication health from the admin API. |
| `operator_stretchcluster_spec_drift` | GaugeVec | `stretchcluster`, `member` | 0/1 — does this member's local `StretchCluster.spec` differ from the operator's view. Detects stale manifests applied to a single peer. |

**Suggested alerts** (all shipped in the chart's PrometheusRule):

- **Member unreachable**: `operator_stretchcluster_member_reachable == 0` for 2m → warning.
- **Broker count skew**: `operator_stretchcluster_brokers - operator_stretchcluster_brokers_ready > 0` for 10m → warning.
- **Spec drift**: `operator_stretchcluster_spec_drift > 0` for 5m → warning.
- **Replication unhealthy**: `operator_stretchcluster_replication_health == 0` for 5m → warning.

## Group 4: multicluster raft metrics

Only the multicluster operator emits these. They cover the cross-cluster
leader-election layer (raft running over a gRPC transport between
operator peers).

For the full inventory, see `docs/multicluster-operator.md`. Summary:

| Metric family | Purpose |
|---------------|---------|
| `operator_multicluster_raft_state{state=...}` | One series per state, value 0/1. `state{state="leader"} == 1` identifies the leader. |
| `operator_multicluster_raft_term` | Current raft term. |
| `operator_multicluster_raft_leader_changes_total` | Cluster-wide leader changes. |
| `operator_multicluster_raft_messages_{sent,received}_total{msg_type,peer}` | Raft RPC throughput. |
| `operator_multicluster_raft_send_duration_seconds{peer,result}` | Cross-region RTT distribution. |
| `operator_multicluster_raft_send_errors_total{peer,error_type}` | Send failures categorised as `timeout` / `canceled` / `unavailable` / `auth` / `marshal` / `other`. |
| `operator_multicluster_raft_messages_dropped_total{peer}` | Queue-full drops at `EnqueueSend`. |
| `operator_multicluster_raft_peer_reachable{peer}` | 0/1 — last DoSend / ReportUnreachable outcome. |
| `operator_multicluster_raft_unreachable_reports_total{peer}` | `node.ReportUnreachable` call count. |
| `operator_multicluster_raft_follower_match_lag_entries{peer}` | Leader's last log index minus each follower's match index. |
| `operator_multicluster_raft_send_queue_length{peer}` | Per-peer send queue depth (read on scrape). |
| `operator_multicluster_raft_inflight_rpcs{peer}` | Concurrent DoSend calls per peer. |
| `operator_multicluster_raft_snapshots_sent_total{peer}` | Snapshots sent via the MsgSnap fallback. |
| `operator_multicluster_raft_snapshot_send_errors_total{peer}` | Snapshot send failures. |

**Leader-only metrics** (filter at query time): `follower_match_lag_entries`
is only populated on the leader. Use the standard
`* on(...) group_left() (operator_multicluster_raft_state{state="leader"} == 1)`
join.

## Starter dashboard

The chart ships a starter Grafana dashboard JSON at
`docs/operator-grafana-dashboard.json`. Import it into Grafana with a
Prometheus datasource pointed at whatever scrapes the operator's `/metrics`.
The dashboard has panels for every metric in Groups 1 and 2 plus a
cross-link to the multicluster raft dashboard.
