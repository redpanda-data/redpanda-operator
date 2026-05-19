# Stretch Cluster Operator Upgrade Runbook

This document covers how to upgrade the **multicluster Redpanda operator** on a running stretch cluster: recommended order, how to verify health before/after each step, and how to recover from a failed upgrade mid-rollout.

Scope: operator helm chart and operator container image. Redpanda broker upgrades (rolling the StatefulSet to a new Redpanda image) are covered separately in [`multicluster-operator.md`](./multicluster-operator.md) and follow the standard "one pod at a time" pattern.

## TL;DR

1. Verify the operator raft quorum is healthy on every cluster.
2. `helm upgrade` the operator in cluster 0, then wait for that cluster's operator deployment to roll AND the raft quorum to recover with a strictly advanced raft term.
3. Repeat for cluster 1, then cluster 2.
4. After all clusters are upgraded, verify the Redpanda data plane is still healthy and topic data is intact.

Do not upgrade more than one cluster's operator at the same time.

## Recommended Order

Upgrade clusters one at a time, in **first-to-last index order**: `cluster-1`, then `cluster-2`, then `cluster-3` (or whatever the deployment-side naming is — pick a stable order and stick to it).

* The operator raft quorum needs ⌊n/2⌋ + 1 members alive to keep operating. With three clusters that's two. Rolling one operator at a time keeps the other two alive and able to elect a leader while the upgraded operator rejoins.
* During a `helm upgrade`, the operator Deployment terminates the old pod and starts a new one. While both are absent from raft, the cluster has only two healthy participants — the bare minimum. A second concurrent upgrade in another cluster would drop quorum and stall reconciliation everywhere.
* In production the operator is not on the data path: a few seconds of no-leader means no new reconciliation work, not Kafka and other listeners unavailability. The Redpanda brokers handle traffic regardless of who the operator leader is.

## Health Verification

### Before starting an upgrade

Verify the raft quorum is healthy on every cluster. There are two equivalent ways:

**Option A — `rpk-k8s multicluster check` (if available in your cluster image):**

```bash
for ctx in $CLUSTE$R_1 $CLUSTER_2 $CLUSTER_3; do
  echo "=== $ctx ==="
  kubectl --context "$ctx" -n redpanda exec deploy/<operator-deployment> -- \
    rpk-k8s multicluster check
done
```

The `raft` check calls `TransportService.Status` on the local operator and reports `state=Leader|Follower`, `term=<int>`, and any unhealthy peers. Every cluster should report **state ∈ {Leader, Follower}** and an empty `unhealthy_peers` list.

**Option B — direct gRPC via `grpcurl`:**

```bash
kubectl --context cluster-1 -n redpanda port-forward deploy/<operator-deployment> 9443:9443 &
grpcurl \
  -cacert ca.crt -cert tls.crt -key tls.key \
  -import-path ./pkg/multicluster/leaderelection/proto \
  -proto transport/v1/message.proto \
  localhost:9443 transport.v1.TransportService/Status
```

Where `ca.crt`/`tls.crt`/`tls.key` come from the `redpanda-operator-multicluster-certificates` secret in that cluster's operator namespace.

Either approach should print something like:

```json
{
  "name": "cluster-1",
  "raft_state": "Leader",
  "leader": "cluster-1",
  "term": "7",
  "cluster_names": ["cluster-1", "cluster-2", "cluster-3"],
  "is_healthy": true
}
```

**Hard stop:** if any cluster reports `is_healthy=false`, has non-empty `unhealthy_peers`, or cannot be reached, do not start the upgrade. Resolve the underlying issue first.

### Between cluster upgrades

After `helm upgrade` returns successfully on cluster N, wait for **two** signals before moving on to cluster N+1:

1. **Deployment rolled.** The new operator pod is `Ready` and the old pod is gone:

   ```bash
   kubectl --context cluster-N -n redpanda rollout status deploy/<operator-deployment> --timeout=5m
   ```

2. **Raft quorum recovered with advanced term.** Call `Status` on every cluster (not just the one you just upgraded). All three should report `is_healthy=true`, no `unhealthy_peers`, and a **higher `term`** than what they reported before this upgrade started.

   A higher term is the load-bearing signal: it proves a leader re-election happened, which means the freshly upgraded operator actually joined and forced the quorum to reconverge. If `is_healthy=true` but the term hasn't moved, the new pod might not have joined yet — the existing quorum is still using the old leader, and the new pod is sitting on the sidelines.

### After all clusters are upgraded

1. Re-run the `Status` check on every cluster — quorum healthy, no unhealthy peers.
2. Verify the Redpanda data plane is still up: `rpk cluster health` on any broker pod should return `Healthy: true`.
3. Read back a known sentinel topic (or any topic with data you can verify) to confirm Kafka traffic is unaffected.

## Recovery from a Failed Upgrade

### Failure mode 1: `helm upgrade` returns an error

The chart's pre-upgrade hook (CRD job) failed, or the new operator pod is crash-looping. Helm will report this synchronously.

Steps:

* `kubectl --context cluster-N -n redpanda logs job/<release>-crds` — inspect CRD hook output. Common cause: incompatible CRD changes that need manual cleanup.
* `kubectl --context cluster-N -n redpanda logs deploy/<operator-deployment>` — inspect operator startup logs.
* If the new operator is crashing on startup, roll back **only this cluster**:

  ```bash
  helm --kube-context cluster-N -n redpanda rollback redpanda
  ```

  Do **not** roll back the other clusters yet — they're still on the old version and the rollback brings cluster N back into sync with them.
* Fix the underlying issue (CRD migration, image pull, RBAC change) before re-attempting.

### Failure mode 2: helm succeeded but raft quorum never recovers

`Status` reports `is_healthy=false` or `unhealthy_peers` lists the cluster you just upgraded, indefinitely.

Steps:

* Check the new operator pod's logs — likely either (a) TLS material missing or (b) peer addresses unreachable.
* Verify the `redpanda-operator-multicluster-certificates` secret is intact in that cluster's namespace and matches what the other clusters' operators have for the CA. The chart's `multicluster.peers` value must include this cluster.
* If the upgrade introduced a values-schema change you didn't account for (renamed key, removed default), the new operator may have started with a degraded config. Inspect the rendered Deployment env vars and command line.
* If the issue can't be fixed forward, roll back this cluster's release (see above) — the two unupgraded clusters plus the rolled-back one will hold quorum on the prior version.

### Failure mode 3: cascading failure during a multi-cluster upgrade

You hit failure mode 1 or 2 mid-way through, but you've already upgraded one or more clusters successfully and there's no path forward.

Steps:

1. Stop the rollout — do not start a `helm upgrade` on any remaining cluster.
2. Roll back the failed cluster (`helm rollback`).
3. Verify quorum recovers across the mix of old+new operators. The operator chart and protocol must remain wire-compatible across one minor version, so a transient state of "two clusters on N, one on N-1" should still form a healthy quorum.
4. If quorum forms, fix the root cause and resume the upgrade. If it does not, roll back **every** cluster that you already upgraded. Stretch cluster operators are designed to be roll-back-safe within one minor version.
