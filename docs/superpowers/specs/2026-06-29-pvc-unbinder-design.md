# PVC-Unbinder for decommissioned-broker bad_rejoin (K8S-843)

Date: 2026-06-29
Status: Approved (design)
Jira: https://redpandadata.atlassian.net/browse/K8S-843

## Problem

In a StretchCluster, when a region is lost (e.g. all nodes cordoned), Redpanda's
partition autobalancer eventually **auto-decommissions** the unreachable brokers
once `partition_autobalancing_node_autodecommission_timeout_sec` elapses. The
operator did **not** issue this decommission — the cluster did it autonomously.

The decommissioned broker's PVC survives. When the pod later reschedules onto the
**same** PVC (old data dir), the broker boots with its **decommissioned `node_id`
and old node UUID**. The controller rejects the rejoin (the node_id is retired),
and the pod crashloops forever — the "bad_rejoin" failure mode. Nothing in the
operator wipes the stale disk, so the broker never recovers and the
StretchCluster cannot return to full strength.

Andrew's proposed mechanism (Jira comment):

> Add a mechanism in the RP/StretchCluster controller that does two requests:
> 1. seeing what a running, but not healthy broker UUID is (against that broker)
> 2. seeing if the broker with this UUID has ever been decommissioned (against the full cluster)
> If there is a collision, unbind the PVC and delete the pod for rescheduling with
> a clean disk / new UUID.

## Goal

Add a guarded reconcile step that detects a persistently not-ready broker whose
on-disk identity collides with the cluster's authoritative broker set, and
recovers it by deleting its data PVC(s) and pod so it reschedules with a clean
disk (fresh UUID, freshly-assigned node_id).

Non-goals: changing decommission logic itself; handling single-cluster
RedpandaReconciler (this spec targets the multicluster StretchCluster reconciler
only); persisting decommission history.

## Where

A new reconciler method `reconcilePVCUnbinder` on `*MulticlusterReconciler`,
inserted into the `clusterReconcilers` slice in `Reconcile`
(`operator/internal/controller/redpanda/multicluster_controller.go`, ~line 300),
**after** `reconcileDecommission` (cluster health/brokerMap are freshest there)
and **before** `reconcileLicense`.

It is self-contained: it recomputes the cluster state it needs rather than
threading `reconcileDecommission`'s locals through `state`.

## Detection — UUID/identity collision (Andrew's two requests)

1. **Against the full cluster** — using `state.admin` (healthy leader view):
   `GetBrokerUuids(ctx)` → authoritative `map[nodeID]uuid` of current members,
   plus `Brokers(ctx)` for membership/address cross-reference.

2. **Against the sick broker** — per not-ready pod, build a single-endpoint admin
   client pointed at that pod's own admin API
   (`ClientFactory.RedpandaAdminClientForMulticluster([]string{podAdminEndpoint}, user, pass)`),
   short timeout, and read the broker's self-reported `(node_id, uuid)` via its
   own `GetBrokerUuids`/`Brokers` response.

3. **Collision** holds when the sick broker's self `node_id` is either:
   - **absent** from the cluster-authoritative map (the node_id was retired /
     decommissioned and is gone), or
   - present but mapped to a **different uuid** (a replacement broker took that
     node_id; this disk's identity was superseded).

## Guards (guarded + requeue threshold)

A disk is destroyed only when **all** hold:

- **Not-Ready ≥ `PVCUnbindNotReadyThreshold`**: the pod's `Ready` condition has
  been `False` for at least the threshold (read from
  `pod.Status.Conditions[type=Ready].LastTransitionTime`; no extra persistence).
  Default 5m, configurable via a new `MulticlusterReconciler` field / operator
  flag. This filters out normal startup and brand-new empty-disk brokers, which
  join quickly and never collide.
- **Cluster healthy / quorum intact**: `health.IsHealthy` and `len(downNodes)==0`.
  Never wipe during a live partition, where the "authoritative" view may itself
  be transiently wrong.
- **Collision positively confirmed** (above) — not merely "not in cluster".
- **One unbind per reconcile pass**, then requeue — same conservatism as the
  roll loop.

If the sick broker's admin API is unreachable (truly crashlooping, admin port
down), we cannot read its self-identity → **defer** (requeue) rather than guess.
The threshold guard means we keep retrying; this errs on the side of not
destroying data.

## Action

- New `LifecycleClient.DeletePVCsForPod(ctx, pod)`: deletes the broker's data
  PVC(s) on the pod's cluster, matched by StatefulSet `volumeClaimTemplate` name
  + pod ordinal/labels (mirrors how `DeletePod` resolves the pod's cluster).
- Then existing `DeletePod(ctx, pod)`.
- The StatefulSet recreates an empty PVC from its template; the broker boots
  fresh, obtains a new UUID, and the cluster assigns a new node_id → rejoin
  succeeds.
- Emit a loud `Info`/event log and (optionally) an observability counter
  `stretchcluster_pvc_unbind_total{member,cluster}`.

## Tests (TDD)

Unit tests against a fake admin API + fake/lifecycle client:

- collision (node_id absent) + not-ready past threshold + healthy → PVC+pod deleted.
- collision (node_id→different uuid) → PVC+pod deleted.
- no collision (identity matches cluster) → no-op.
- collision but not-ready < threshold → no-op (requeue).
- collision but cluster unhealthy / downNodes present → no-op (requeue).
- sick-broker admin unreachable → defer (requeue), no deletion.
- only one unbind per pass when multiple candidates exist.

Plus `DeletePVCsForPod` lifecycle test (correct PVC matched + deleted, correct
cluster targeted).

## Build / ship

- `nix develop -c task build:image`
- `docker tag localhost/redpanda-operator:dev public.ecr.aws/x0d1q6b5/operator:pvc-unbinder-feature`
- `docker push public.ecr.aws/x0d1q6b5/operator:pvc-unbinder-feature`
  (use the linux/amd64 build path from operator/CLAUDE.md if the local arch
  doesn't match the cloud nodes).

---

# Appendix A — 3-cloud cross-cloud validation infra (net-new)

The `redpanda-operator-stretch-beta` repo ships only **single-cloud** 3-region
terraform (all-AWS, all-GCP, or all-Azure). True "3 clusters, 3 clouds" requires
net-new cross-cloud L3 so broker pod IPs route between clouds.

- **Topology:** AWS/EKS `us-east-1` (2 brokers, "east") · GCP/GKE `us-west1`
  (2 brokers, "west") · Azure/AKS `westeurope` (1 broker, "eu"). 2/2/1, RF=5.
- **Networking:** full-mesh site-to-site IPSec VPN (AWS VPN Gateway ↔ GCP HA VPN
  ↔ Azure VPN Gateway). Non-overlapping pod/service/node CIDRs across the three.
  Pod CIDRs made routable across the VPN (GKE VPC-native alias IPs advertised via
  Cloud Router BGP; EKS VPC-CNI pod IPs in subnet ranges; AKS Azure CNI). Broker
  RPC :33145 / Kafka :9093 traverse the VPN directly (`crossClusterMode: flat`);
  operator raft :9443 via per-cluster internal LB (as in the repo).
- **Reuse** the repo's per-cloud K8s terraform; add a `cross-cloud-vpn/` layer for
  the mesh. Install `local-path` provisioner per cluster
  (`local-path-provisioner v0.0.36`). Operator image = `pvc-unbinder-feature`.
  GCP project `rp-byoc-rafal-2b5f`; AWS profile `cloud-sandbox`; Azure
  `sandbox-cloud`. License from `operator/redpanda.license`.

# Appendix B — Scenario + conditional failover

1. 2/2/1 brokers, RF=5 topic; produce + consume.
2. Set `partition_autobalancing_node_autodecommission_timeout_sec: 900`.
3. `kubectl cordon` the **east** (AWS) region's nodes. Do **not** run the
   failover capacity injection.
4. Wait (~15 min) for the autobalancer to auto-decommission the lost brokers;
   observe whether the new PVC-unbinder lets the cluster self-heal.
5. **Only if** Redpanda cannot decommission: stand up the failover cluster,
   reconfigure all 3 operators + install the failover operator, create the
   StretchCluster, confirm `Quiesced=True` and `Stable=True`, then create the
   `RedpandaBrokerPool`.
