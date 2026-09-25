# Scale-Down, Decommissioning, and PVC Recovery

This document describes how the operator removes brokers from a Redpanda cluster — both deliberately (a user lowers the replica count) and reactively (a Kubernetes node dies and takes a broker with it) — and how the storage those brokers leave behind is cleaned up or recovered. It covers four cooperating mechanisms:

1. **Graceful scale-down** — the controller-driven flow when `spec.replicas` decreases, for both the v2 `Redpanda` CR (`cluster.redpanda.com/v1alpha2`) and the v1 `Cluster` CR (`redpanda.vectorized.io/v1alpha1`).
2. **The StatefulSetDecommissioner** — a safety-net controller that removes *dead* brokers ("ghost brokers") from the Redpanda membership when their pod or node is gone, and optionally deletes the orphaned PersistentVolumeClaims a scale-down leaves behind.
3. **The PVCUnbinder** — a controller that frees pods stuck in `Pending` because their local-disk PersistentVolume is pinned to a node that no longer exists.
4. **PVC lifecycle policy** — the `--auto-delete-pvcs` flag, StatefulSet `persistentVolumeClaimRetentionPolicy`, and the invariant that a PersistentVolume is always switched to `Retain` before its claim is deleted.

For the multicluster (StretchCluster) variant of this flow, see [multicluster-reconciliation.md](./multicluster-reconciliation.md) (phase 9, "Decommissioning and rolling restarts").

---

## Background: what "decommissioning" means

Removing a broker from a Redpanda cluster is not the same as deleting its pod. A broker holds partition replicas and participates in Raft groups; it must be **decommissioned** through the Admin API (`DecommissionBroker`) so that Redpanda drains its partitions onto the remaining brokers and removes it from cluster membership. Until the decommission reports finished, the broker's data is still needed.

This ordering constraint shapes everything below:

- On **graceful scale-down**, the operator decommissions the broker *first* and shrinks the StatefulSet (deleting the pod) only after the drain completes.
- On **node loss**, the pod and possibly the disk are already gone. Nothing can be drained; the dead broker ID must still be decommissioned so Redpanda stops counting it toward quorum, and a replacement broker (a new ID on an empty disk) must be able to start.
- All paths decommission **one broker at a time**. Draining is expensive and concurrent decommissions of a shrinking cluster risk losing partition availability.

A decommission can hang forever by design: if the remaining brokers cannot host the drained replicas (e.g. scaling 3→2 while topics have replication factor 3), Redpanda never finishes the drain. The v1 flow supports **recommissioning** (raising `spec.replicas` back while the broker is still draining) as the escape hatch; the v2 flow simply keeps requeueing and reports the cluster as not `Quiesced`/`Stable` until the drain completes or the spec changes.

---

## Component map

| Component | What it handles | Where it lives | How it's enabled |
|---|---|---|---|
| v2 `RedpandaReconciler.reconcileDecommission` | Graceful scale-down of `Redpanda` CRs and `NodePool` CRs | `operator/internal/controller/redpanda/redpanda_controller.go` | Always on for v2 clusters |
| v1 `StatefulSetResource.handleScaling` | Graceful scale-down of vectorized `Cluster` CRs | `operator/pkg/resources/statefulset_scale.go` | Always on for v1 clusters |
| `StatefulSetDecommissioner` | Ghost/dead broker removal; orphaned-PVC cleanup | `operator/internal/controller/decommissioning/statefulset_decomissioner.go` | `--additional-controllers=decommission` (operator-wide, v2), `--enable-ghost-broker-decommissioner` (operator-wide, v1), or the per-cluster sidecar (`--run-decommissioner`) |
| `PVCUnbinder` | Pods stuck `Pending` on a local PV pinned to a dead node | `operator/internal/controller/pvcunbinder/pvcunbinder.go` | `--unbind-pvcs-after=<duration>` (operator-wide) or the sidecar (`--run-pvc-unbinder`) |
| Broker controller disk-lost path | v2 Broker-CR clusters: pod + PVC teardown after provable disk loss | `operator/internal/controller/redpanda/broker_controller.go` | `--enable-broker` (timeout shared with `--unbind-pvcs-after`) |

Two historical components are worth knowing about because their names still appear in older material:

- The **nodeWatcher** controller (`RedpandaNodePVCReconciler`), which deleted PVCs in reaction to Node-delete events, was removed. It could miss a Node deletion that happened while the operator itself was down, and it deleted any matching PVC. The PVCUnbinder is its successor: it reacts to the observable symptom (the unschedulable pod) instead of the event, and touches only non-remountable local/hostPath volumes.
- The **legacy decommission controller** (`operator/internal/controller/olddecommission/`) predates the `StatefulSetDecommissioner`. It is only reachable via the deprecated `--additional-controllers=legacy-decommission`, which takes precedence over `decommission` so the two never run together (and `all` deliberately excludes it).

---

## Graceful scale-down: v2 (`Redpanda` CR)

### The reconcile chain

`RedpandaReconciler.Reconcile` runs a fixed sequence of sub-reconcilers; any step that errors or requests a requeue aborts the rest of the pass:

```
reconcileParameterValidation → reconcileResources → reconcilePools →
initAdminClient → reconcileMaintenanceMode → reconcileStaleDiskWipe →
reconcileDecommission → reconcileLicense → reconcileClusterConfig
```

Desired state comes from rendering the Redpanda Helm chart in-process: `lifecycle.Client.FetchExistingAndDesiredPools` renders the chart's StatefulSets (`charts/redpanda` Go source via `V2NodePoolRenderer`) and compares them with what exists, tracked in a `lifecycle.PoolTracker`. The operator forces `updateStrategy: OnDelete` on every rendered StatefulSet regardless of chart values — Kubernetes never rolls or deletes broker pods on its own; the operator owns pod deletion order.

The split of responsibilities between two steps is the key design point:

- **`reconcilePools` only ever creates, scales *up*, or updates** StatefulSets (`ToCreate`, `ToScaleUp`, `RequiresUpdate`). It also refuses to mutate anything while any pool is mid-scale (`CheckScale`: a pool whose `spec.replicas`, `status.replicas`, and live pod count disagree).
- **`reconcileDecommission` owns everything that removes brokers**: scale-down, deletion of drained pools, and — because it must be sequenced after decommissions — the one-pod-at-a-time rolling restart of out-of-date pods.

Two steps deliberately run *before* decommissioning:

- **`reconcileMaintenanceMode`** clears maintenance mode left over from restarts (bounded by `--clear-maintenance-mode-after`, default 30m). A broker stuck in maintenance mode can never finish decommissioning — the partition balancer refuses to move data off it.
- **`reconcileStaleDiskWipe`** (opt-in via `--wipe-stale-disk-after`) handles the `bad_rejoin` case where a previously-decommissioned broker's old disk tries to rejoin (K8S-843).

### The scale-down loop

When a pool's desired replicas are lower than its existing replicas, `PoolTracker.ToScaleDown` produces a `ScaleDownSet`: the StatefulSet with `spec.replicas` already decremented **by exactly one**, plus `LastPod`, the highest-ordinal pod — the one that will disappear when the StatefulSet shrinks. A guard refuses to interpret "no desired counterpart for this pool" as intent to drain it unless the NodePool list was actually observed during this pass, so a stale cache cannot condemn a pool.

For each pass, `reconcileDecommission`:

1. Fetches the Admin API health overview and builds a map from pod name (the first DNS label of each broker's internal RPC address, with a pod-IP fallback) to broker ID.
2. Takes the **first** pool needing scale-down and calls `scaleDown` — then returns early and requeues. One StatefulSet, one broker per pass.
3. `scaleDown` resolves `LastPod` to its broker ID. If the mapping is ambiguous it defers (logs and requeues) rather than guessing. If the pod maps to *no* registered broker, the broker is already gone and the StatefulSet is patched down directly.
4. Otherwise `decommissionBroker` drives the Admin API: `DecommissionBrokerStatus` to observe progress; if the broker "is not decommissioning" yet, `DecommissionBroker` starts it; while the status is not `Finished`, requeue.
5. Only once the decommission is `Finished` does the operator patch the StatefulSet with the decremented replica count (server-side apply with forced ownership). The StatefulSet controller then deletes the highest-ordinal pod.

A pool whose `NodePool` CR was deleted is drained the same way, one broker at a time, and its StatefulSet object is deleted only once it reaches zero replicas (`ToDelete` explicitly guards against deleting a StatefulSet whose pods still need decommissioning).

### Status and observability

- `status.nodePools[]` carries per-pool `Replicas`, `DesiredReplicas`, `ReadyReplicas`, and **`CondemnedReplicas`** — `status.Replicas - spec.Replicas` clamped at zero, i.e. how many brokers still await decommissioning.
- Because scale-down passes always end in a requeue, the `Quiesced` condition (and therefore the `Stable` rollup) stays `False` until the scale completes. `statuses.yaml` documents `Quiesced/StillReconciling` as covering exactly this.
- `reconcileDecommission` maintains `ResourcesSynced` and `Healthy` (from the health overview) in a deferred status update on every pass.
- The Redpanda controller itself emits no decommission events; events come from the StatefulSetDecommissioner (below) when it is the actor.

### PVCs on v2 scale-down

The Redpanda controller never deletes PVCs. What happens to the orphaned `datadir` claim is decided by:

- The chart's `statefulset.persistentVolumeClaimRetentionPolicy` (default `Retain`/`Retain`), exposed on the CRD — if set to `Delete` on scale-down, Kubernetes removes the claim.
- Otherwise the operator-wide `StatefulSetDecommissioner`, when running with `--auto-delete-pvcs`, deletes claims left unbound by scale-down (see below). Without that flag the claims persist for manual cleanup.

**Edge cases:**
- A decommission that cannot complete (insufficient remaining capacity for the drained replicas) requeues forever; the cluster stays `Stable=False`. Raising `spec.replicas` again removes the pool from `ToScaleDown`, but — unlike v1 — the Redpanda controller never calls `RecommissionBroker`: a drain already started through the Admin API keeps running. Cancelling it takes a manual recommission (`rpk redpanda admin brokers recommission`), or, on the Broker-CR path, removing the Broker's decommission intent (the Broker controller does recommission). If the drain instead runs to completion while the pod still exists, the broker lands in the decommissioned-broker `bad_rejoin` crashloop that `reconcileStaleDiskWipe` exists to recover.
- Because `reconcilePools` refuses to act while any pool fails `CheckScale`, an interrupted scale operation converges before new spec changes are applied.

---

## Graceful scale-down: v1 (vectorized `Cluster` CR)

The v1 flow predates the v2 design and drives the same invariant (decommission before pod removal) through a status-field state machine in `operator/pkg/resources/statefulset_scale.go`.

### The `currentReplicas` dance

The StatefulSet's replica count is never rendered from `spec.replicas` directly. It is rendered from **`status.nodePools[*].currentReplicas`** — "the number of Pods the controller currently wants to run" — and only `handleScaling` moves that number:

- **Scale up**: `currentReplicas` jumps straight to `spec.replicas`; Redpanda handles new brokers joining on its own. Scale-up is deliberately *not* gated on cluster health (only decommission is), so an unhealthy cluster can still be grown.
- **Scale down**: one broker at a time, **across all NodePools** — only one decommission may be in flight cluster-wide. The controller:
  1. Picks the highest ordinal of the pool (`currentReplicas - 1`).
  2. Resolves the pod's broker ID from the `operator.redpanda.com/node-id` pod annotation, and double-checks the broker is actually registered in Redpanda via `Brokers()` — a guard against stale reads of `currentReplicas` and against downscaling a pod whose broker never finished registering (upscale must fully complete before downscale is considered).
  3. Records intent by setting **`status.decommissioningNode`** (despite the field's doc comment saying "ordinal", it stores the broker ID; use `GetDecommissionBrokerID`/`SetDecommissionBrokerID`).
- **In-flight decommission** (`status.decommissioningNode != nil`), handled before anything else: `handleDecommission` polls `Brokers()`; while the broker reports `draining` it requeues (interval `--decommission-wait-interval`, default 8s, with 20% jitter); once the broker disappears from the broker list, it clears `decommissioningNode`, decrements `currentReplicas`, and `verifyRunningCount` requeues until the StatefulSet and live pod count actually shrink. Multi-step downscales repeat the whole cycle per broker.

### Recommissioning

If the user raises `spec.replicas` back while a decommission is in flight, `handleRecommission` calls `RecommissionBroker` — but only while Redpanda still reports the broker as `draining`. Once the pod is gone or the broker is fully decommissioned, recommission is a `RecommissionFatalError` and the controller falls through and lets the decommission complete (the subsequent scale-up then adds a fresh broker). Success is detected when the broker's membership returns to `Active`.

### Maintenance-mode interactions

A decommissioned node's shutdown hooks may activate maintenance mode, wedging the cluster (redpanda#4999). `disableMaintenanceModeOnDecommissionedNodes` runs whenever `decommissioningNode` is set and the pod is already gone, and the rolling-update path both strips maintenance mode from in-decommission nodes and tolerates a 404 when enabling it ("broker is most likely decommissioned").

### PVCs, safety, and status in v1

- **PVC deletion is delegated to Kubernetes.** With `--auto-delete-pvcs`, the StatefulSet is rendered with `persistentVolumeClaimRetentionPolicy: {whenDeleted: Delete, whenScaled: Delete}`; without it, the legacy behavior (retain everything) applies. The Cluster/pod finalizers that once deleted PVCs and decommissioned brokers on node loss were removed as unreliable — a missed edge could destroy Raft quorum — and any leftover finalizers are actively stripped each reconcile.
- The validating webhook rejects `replicas <= 0` and blocks *any* decrease unless downscaling is allowed (`--allow-downscaling`, default true at the operator level).
- There is no quorum arithmetic in the controller; Redpanda itself is the backstop. A drain that cannot complete hangs visibly in `status.decommissioningNode`, and the documented recovery is recommissioning by raising `spec.replicas`.
- The **`OperatorQuiescent`** condition reports `False` with reason `DecommissioningInProgress` (message naming the node ID) while a decommission runs, and `NodePoolNotSynced` while replica counts disagree. The v1 decommission path emits no Kubernetes events.

---

## The StatefulSetDecommissioner: unplanned broker removal

Graceful flows assume the broker is alive to be drained. When a Kubernetes node dies, is preempted (spot instances), or a pod is force-deleted, the cluster is left with a **ghost broker**: a registered member that is down and will never return, dragging cluster health and quorum accounting. The `StatefulSetDecommissioner` (`operator/internal/controller/decommissioning/statefulset_decomissioner.go` — note the historical one-`m` filename) removes such brokers, and optionally cleans up the PVCs that scale-downs leave behind.

### Triggers

It is a StatefulSet controller: `For(StatefulSet)`, `Owns(Pod)`, plus a watch on PersistentVolumeClaims mapped back to their StatefulSet by name (`datadir-<sts>-<ordinal>`, via regex — PVCs have no true owner reference). A label selector (mode-dependent, below) filters both the watches and the PVC mapping. Because a node can vanish without generating any pod/STS event the operator observes, the v1 mode also runs on a periodic `syncPeriod` resync.

### Decision pipeline

Each pass, for one StatefulSet:

1. **Health first.** `GetHealthOverview` from the Admin API. If the number of registered brokers is not larger than the desired replica count, or if no brokers are down, there is nothing to do — only brokers Redpanda itself reports as **down** are ever candidates. The desired count is *cluster-wide*: with NodePools a cluster spans several StatefulSets, so the fetcher sums the replicas of every pool.
2. **Map brokers to pods.** Each broker ID resolves to a pod name via the first DNS label of its `InternalRPCAddress`. Unmappable brokers are logged and skipped.
3. **Classify each downed broker** (skipping any already decommissioning, per `DecommissionBrokerStatus`):
   - **Too-high ordinal** (scale-down leftovers): the pod's ordinal — parsed against this exact StatefulSet's name, so `redpanda-poolb-0` never counts against `redpanda` — is `>= spec.replicas` of *this pool*. Enabled by default; disabled in v1 mode, where the Cluster controller owns ordinal-based scale-down.
   - **Ghost broker** (ordinal collision): if two or more broker IDs map to the same pod name, an old dead broker and its replacement are both registered. The downed one is decommissioned only if at least one *healthy* broker shares the pod name — with no healthy twin there is no way to tell which registration is current, so nothing is touched. A downed broker that maps *uniquely* to its pod is left alone entirely; it may just be restarting.
4. **Vote before acting.** Candidates pass through a delayed-vote cache: an entry must be re-confirmed on passes at least `interval` apart, `count` times (defaults 30s × 2 ≈ one minute of sustained evidence) before the destructive callback runs. Every pass first expunges entries that no longer qualify, so a broker that recovers mid-window is dropped. This absorbs both transient down states during rolling restarts and stale-informer races.
5. **One at a time.** If any broker is currently decommissioning, the pass requeues without starting another. When a decommission is issued (`DecommissionBroker`, after emitting a `DecommissioningBroker` event on the StatefulSet), the pass ends immediately; progress is re-polled on requeue (10s with 10% jitter) until `Finished`.

### Orphaned-PVC cleanup

When constructed with PVC cleanup enabled, each pass first looks for **unbound datadir claims**: claims matching the StatefulSet's volume-claim-template labels that no pod's volumes reference. Pods are matched with the StatefulSet's immutable **`spec.selector`** — never `spec.template.labels`. That distinction is load-bearing: template labels include `helm.sh/chart`, which changes on every upgrade, and under the chart's `OnDelete` strategy not-yet-rolled pods keep the old labels. Matching on template labels made live brokers' pods invisible mid-upgrade, their claims look unbound, and (after the vote window) the decommissioner deleted the PVCs of running brokers — they came back on empty disks and rejoined as new brokers. Fixed in PR #1866; pinned by `TestFindUnboundVolumeClaims`.

Deletion itself is debounced through the same vote cache and, critically, **patches the backing PersistentVolume to `reclaimPolicy: Retain` before deleting the claim**, so the data outlives the claim. A `DecommissioningUnboundPersistentVolumeClaims` event is emitted on the StatefulSet.

### Deployment modes

The same controller runs in three configurations:

| Mode | Enabled by | Selector / scope | PVC cleanup | Notes |
|---|---|---|---|---|
| Operator-wide, v2 | `--additional-controllers=decommission` (or `all`; alias `decommissionV2`) | Any STS with an `app.kubernetes.io/instance` label that resolves to a `Redpanda` CR. Deliberately not keyed on `app.kubernetes.io/name`, which `nameOverride` would break | Only with `--auto-delete-pvcs` (PR #1866; previously always on) | Desired replicas summed across all of the CR's pool StatefulSets. The filter deliberately has **no** readiness gate — requiring a healthy cluster would deadlock the very remediation this controller performs. Admin client from the chart-rendered DNS |
| Operator-wide, v1 "ghost broker" | `--enable-ghost-broker-decommissioner` | v1 Cluster ownership via ownerReferences; skips `redpanda.vectorized.io/managed=false` | Never | Ordinal rule disabled (the v1 controller owns scale-down and cannot always cope with external decommissions). Acts only on quiesced clusters (replicas synced, not restarting, generation observed, no decommission in flight) with ≥3 desired replicas. Periodic resync `--ghost-broker-decommissioner-sync-period` (default 5m) |
| Per-cluster sidecar | Chart value `statefulset.sideCars.brokerDecommissioner.enabled` → `--run-decommissioner` | That cluster's release labels; namespace-scoped cache; leader-elected among the cluster's own pods | On (constructor default) | For chart-managed clusters with no operator. Admin client from the pod-local `redpanda.yaml` rpk profile. Vote knobs: `--decommission-vote-interval` (= chart `decommissionAfter`, default 60s), `--decommission-vote-count` (chart hardcodes 2), `--decommission-requeue-timeout` (10s) |

Separately, the v1 Cluster controller has an older in-process variant behind the hidden `--unsafe-decommission-failed-brokers` flag ("ghostbuster"): it directly decommissions registered-but-dead brokers absent from Kubernetes, only when all pods are Running and counts match. It predates the shared controller and is irreversible; prefer `--enable-ghost-broker-decommissioner`.

The decommissioner writes no CR status and exposes no custom metrics — its observable surface is the two StatefulSet events, trace logs of each pass's classification (healthy / ignored / to-decommission / decommissioning), and the standard controller-runtime metrics under `statefulset_decommissioner`.

---

## Pods stuck in Pending: the PVCUnbinder

### The problem

With local storage (the `local` or `hostPath` volume shapes), a PersistentVolume carries node affinity to the node holding the disk. When that node is deleted — spot reclaim, cluster autoscaler, hardware failure — the StatefulSet controller recreates the pod, but the pod's PVC is still bound to the old PV, and the scheduler reports `Unschedulable` with a volume-node-affinity conflict. The pod stays `Pending` forever: PVCs are immutable, so nothing short of deleting the claim can free it.

The `PVCUnbinder` watches **pods, not nodes** — a Node-deletion event can be missed when the operator itself ran on the dead node; the stuck pod is the durable, observable symptom.

### Trigger conditions

A pod qualifies for remediation when all of the following hold:

- It is `Pending` and controller-owned by a StatefulSet (event-filter predicate).
- It matches the optional `--unbinder-label-selector`.
- It has `PodScheduled=False` with reason `Unschedulable`, whose message matches a deliberately weak regex (`0/N nodes are available` for N ≥ 1, or `volume node affinity` — schedulers stopped naming volume affinity in the message somewhere between K8s 1.21 and 1.28). `0/0 nodes` is excluded: all nodes gone is a cluster-wide event, not a per-pod one.
- The `Unschedulable` condition is older than the configured timeout (`--unbind-pvcs-after`); younger pods are requeued for the remaining delta.
- The pod's StatefulSet-named PVCs (claim name suffixed with the pod name) resolve to PVs that have node affinity **and** are `local`/`hostPath`. Remountable network volumes are never touched.

### Safety gates

Before anything destructive, five gates run against uncached reads; any failure defers with a 30s requeue, a `PVCUnbinderDeferred` event, and a `pvc_unbinder_gate_deferred_total` metric increment:

- **Gate 0 — in-flight**: a previous unbind in the same cluster hasn't settled. Progress is recorded as durable annotations on the PVs (`operator.redpanda.com/pvc-unbinder-in-flight` / `-claim`), so a crash mid-unbind leaves evidence and the same pod may resume its own half-finished unbind, but a second pod may not start one.
- **Gate 1 — pause**: the owning Redpanda / StretchCluster / v1 Cluster CR carries the `operator.redpanda.com/pause-pvc-unbinder` annotation.
- **Gate 2 — multi-node event**: stuck broker pods pinned to more than one distinct node indicate a cluster-wide disruption (upgrade, zone outage); unbinding during one risks compounding the damage. (This gate fails *open* if the operator lacks cluster-wide pod list permission.)
- **Gate 3 — PVC rebinding in progress**: any same-cluster PVC with an empty `spec.volumeName` defers, unless it is provably deadlocked (the "stuck claim exemption": a chain of proofs that the claim is mis-pinned to a node that is gone or permanently unschedulable; disable with `--disable-pvc-rebinding-gate-exemption`).
- **Gate 4 — freed PV**: a previously freed, still-`Available` PV whose node is alive blocks further unbinds — the guard against the INC-2818 cross-broker disk swap.

If a freed PV never rebinds, the gates hold the cluster **forever, on purpose**: an alertable halt is better than a silent disk swap.

### Remediation sequence

1. A single patch per PV, *before any delete*: set `reclaimPolicy: Retain` and write the in-flight annotations (optimistic-locked). A crash at any later point leaves durable evidence and retained data.
2. Delete the bound PVCs, guarded by UID + resourceVersion preconditions.
3. Optionally clear the PV's `claimRef` so it can rebind (`--allow-pv-rebinding`, deprecated, default off). Otherwise the PV is left `Released`/`Retain` for manual inspection.
4. Delete the pod (again precondition-guarded, and only if at least one PVC was actually deleted). The StatefulSet controller recreates pod and PVC, which bind to a fresh disk on a schedulable node.

The recreated broker starts on an empty disk and joins as a **new broker ID**. The unbinder never talks to the Redpanda Admin API; removing the dead *old* broker ID is the decommissioner's job. The two are complementary and both are needed to fully recover from node loss — the acceptance feature `decommissioning.feature` states this explicitly, and `helm-chart.feature`'s "Tolerating Node Failure" scenario demonstrates the full cycle (pod Pending → fresh disk → old broker decommissioned → replacement active).

### Deployment

- **Operator-wide**: `--unbind-pvcs-after=<duration>` (default 0 = disabled) enables it in every operator mode, one controller instance for all clusters; per-cluster isolation comes from the gates' cluster key (the `app.kubernetes.io/instance` label) and the pause annotation. It is *not* part of `--additional-controllers`.
- **Multicluster**: the same controller wrapped per member cluster (`pvcunbinder.MulticlusterController`).
- **Sidecar**: chart value `statefulset.sideCars.pvcUnbinder.enabled` → `--run-pvc-unbinder --pvc-unbinder-timeout=<unbindAfter>` (default 60s), namespace- and cluster-scoped.

Relatedly, on the v2 Broker-CR path (`--enable-broker`), the Broker controller has its own disk-lost flow: after a pod has been volume-affinity-unschedulable for `MarkDiskLostAfter` (default 5m; wired from `--unbind-pvcs-after` when set) *and* the PV is provably pinned to a node that no longer exists, it marks the Broker `DiskLost` and dismantles the pod and PVCs itself, with decommissioning driven through the Broker CR.

---

## PVC cleanup: one table

Every mechanism that deletes a Redpanda PVC first forces the backing PV to `Retain`, so claim deletion never destroys data — recovery from a wrong deletion is always possible at the PV level.

| Mechanism | Deletes | When | Gated by |
|---|---|---|---|
| StatefulSet `persistentVolumeClaimRetentionPolicy` (Kubernetes itself) | Claim of the removed ordinal | On scale-down / STS deletion | v1: `--auto-delete-pvcs` renders `Delete`/`Delete`; v2: chart value `statefulset.persistentVolumeClaimRetentionPolicy` (default `Retain`) |
| `StatefulSetDecommissioner` unbound-claim cleanup | Claims no live pod references (PV → Retain first) | After the vote window (~1 min) | Operator-wide v2: `--auto-delete-pvcs`. Sidecar: always on. v1 ghost mode: never |
| `PVCUnbinder` | Claims of a provably stuck Pending pod (PV → Retain first) | After `--unbind-pvcs-after` + five gates | `--unbind-pvcs-after` > 0 / sidecar flag |
| Broker controller `dismantleDiskLost` | Claims of a Broker whose disk is provably lost | After `MarkDiskLostAfter` | `--enable-broker` |

---

## Flag quick reference (`redpanda-operator run`)

| Flag | Default | Effect |
|---|---|---|
| `--additional-controllers=decommission` | off | Operator-wide `StatefulSetDecommissioner` for v2 clusters (`all` includes it; `legacy-decommission` overrides it) |
| `--auto-delete-pvcs` | false | v1: STS retention policy `Delete`; also lets the v2 decommission controller delete leftover claims |
| `--unbind-pvcs-after` | 0 (off) | Enables the PVCUnbinder; also the Broker controller's disk-lost timeout |
| `--unbinder-label-selector` | — | Restricts which pods the unbinder considers |
| `--allow-pv-rebinding` | false (deprecated) | Unbinder clears PV `claimRef` after freeing |
| `--disable-pvc-rebinding-gate-exemption` | false | Turns off Gate 3's stuck-claim exemption |
| `--decommission-wait-interval` | 8s | v1 decommission poll interval (also legacy-decommission) |
| `--enable-ghost-broker-decommissioner` | false | v1 ghost-broker mode of the `StatefulSetDecommissioner` |
| `--ghost-broker-decommissioner-sync-period` | 5m | Periodic resync for the above |
| `--unsafe-decommission-failed-brokers` | false (hidden) | v1 in-controller ghostbuster |
| `--clear-maintenance-mode-after` | 30m | v2: unwedge maintenance mode so decommissions can finish |
| `--wipe-stale-disk-after` | 0 (off) | v2: `bad_rejoin` stale-disk recovery before decommission |

The operator Helm chart passes all of these through `additionalCmdFlags`; RBAC for the optional controllers is gated by `rbac.createAdditionalControllerCRs`. The Redpanda chart's sidecar equivalents live under `statefulset.sideCars.{brokerDecommissioner,pvcUnbinder}` (both disabled by default).

---

## Where the behavior is pinned by tests

- **v2 scale-down**: `operator/internal/controller/redpanda/redpanda_controller_test.go` — `TestScaling` (5→3→5 waiting on `ClusterStable`), `TestNodePoolsBlueGreen` (drain one pool to zero while growing another). Acceptance: `acceptance/features/scale-down.feature`, `scale-up.feature`.
- **v1 scale-down**: kuttl suites `operator/tests/e2e/decommission/` (down, up, down again), `e2e/lost-redpanda-decommission/`, and `e2e-with-flags/decommission/` (asserts the `--auto-delete-pvcs` retention policy).
- **Decommissioner**: `operator/internal/controller/decommissioning/` — `statefulset_decommissioner_test.go` (integration: scale-down PVC cleanup, then simulated node failure → ghost decommission), `statefulset_decommissioner_internal_test.go` (`TestFindUnboundVolumeClaims`, the PR #1866 rolling-restart regression pin), `pod_ordinal_test.go` (NodePool prefix attribution), `delayed_cache_test.go`. Wiring: `operator/cmd/run/redpanda_decommission_test.go`.
- **PVCUnbinder**: `operator/internal/controller/pvcunbinder/pvcunbinder_test.go` (`TestPVCUnbinderShouldRemediate` trigger matrix; `TestIntegrationPVCUnbinder` kills a real k3d node and asserts no pod stays Pending and fresh PVs appear), plus the extensive gate matrices in `pvcunbinder_internal_test.go`.
- **Node-failure end-to-end**: `acceptance/features/decommissioning.feature` and `helm-chart.feature` ("Tolerating Node Failure") — both require the decommissioner and the unbinder together. The acceptance operator install runs with `--additional-controllers=decommission --unbind-pvcs-after=5s --enable-broker`.
