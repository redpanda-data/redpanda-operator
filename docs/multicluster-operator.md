# Multicluster Operator Architecture

This document describes how the multicluster operator works: how it elects a leader across Kubernetes clusters, how it discovers and connects to peer clusters, how it reconciles `StretchCluster` resources, and how it behaves under various failure conditions.

## Overview

A `StretchCluster` is a Redpanda cluster whose brokers are spread across multiple Kubernetes clusters. Managing it requires exactly one operator to be "in charge" at any point in time — the one that holds the raft leader position. That operator reconciles the `StretchCluster` spec, manages broker lifecycle, and propagates configuration changes across all participating clusters.

The multicluster operator achieves this with two layers of leader election stacked on top of each other.

---

## Leader Election

### Layer 1: Within-cluster (K8s lease)

Each Kubernetes cluster runs one or more operator replicas. These replicas compete for a standard Kubernetes `Lease` resource. Only the pod that holds the lease participates in raft. If that pod dies, another replica in the same cluster acquires the lease and takes its place in the raft group. With a single replica the lease election is uncontested but still occurs.

### Layer 2: Cross-cluster (raft)

One raft group spans all participating clusters. Each cluster contributes exactly one participant — the pod holding the local lease, or the single pod if there is no local election. The raft group elects a single leader from among these participants. Only the raft leader starts the reconciliation controllers and begins processing `StretchCluster` resources.

The raft group requires a quorum of `⌊n/2⌋ + 1` nodes to elect a leader and continue operating. For a three-cluster deployment, two clusters must be reachable. For five clusters, three must be reachable.

All communication within the raft group uses gRPC with mutual TLS. Each node advertises a stable address (typically a `ClusterIP` service or via a public load-balanced IP) to its peers.

---

## Cluster Discovery

Before the raft leader can manage a remote cluster it needs a kubeconfig for it. Each operator provisions the necessary credentials in its own cluster on demand — no pre-provisioned kubeconfigs are required:

1. Each operator exposes a `Kubeconfig` gRPC endpoint. When called, it creates a `ServiceAccount` and a long-lived token in its own cluster and returns the resulting kubeconfig to the caller. The token uses the legacy `kubernetes.io/service-account-token` Secret type, which does not expire.

2. When the raft leader is elected it calls each peer's `Kubeconfig` gRPC endpoint, receives the kubeconfig, and uses it to connect to that cluster.

### Kubeconfig caching

A fresh leader election after a crash requires access to every peer's kubeconfig. If a peer's gRPC endpoint is also down at that moment (e.g. its operator pod is still restarting), the new leader cannot connect.

To avoid this dependency, every operator member — not just the leader — fetches kubeconfigs from all of its peers at startup and stores them as Kubernetes `Secret` resources in its own local cluster. The secrets are named `<kubeconfigName>-<peerName>` in the configured namespace.

When a node becomes raft leader, it reads from its local Secret cache first and only falls back to a live gRPC call if the cached Secret is absent. If the gRPC call succeeds, the result is written back to the cache for next time. Fetch failures are retried in the background every five seconds; the operator continues functioning for all other clusters in the meantime.

This means the most common failover scenario — one leader crashes, another takes over — requires no live gRPC connectivity to any peer. The new leader reads from its own cache and proceeds.

---

## How the Leader Manages Clusters

When a node wins raft leadership, for each peer cluster it:

1. Reads the kubeconfig from the local cache (or fetches via gRPC if not cached).
2. Establishes a connection to the remote cluster using that kubeconfig.
3. Makes the connected cluster available to all running controllers.
4. Notifies all running controllers that a new cluster is available. Each controller starts watching resources on that cluster and begins receiving reconcile requests from it.

Each of these steps is attempted independently and concurrently per cluster. If one cluster is unreachable, others proceed unaffected. Failed connection attempts are retried automatically after ten seconds.

All controllers are notified simultaneously when a new cluster becomes available. If a cluster comes online while a controller is already processing a previous notification, the controller processes the new cluster immediately after finishing — no notifications are missed.

### Controller startup sequence

Each controller goes through two phases before processing events:

1. **Cache warmup** — the controller's local view of each cluster's Kubernetes resources is populated from the API server. The controller waits until this view is fully consistent before processing any events, ensuring it never acts on stale or incomplete state.
2. **Reconciliation** — the main controller loop begins processing reconcile requests.

Both phases run alongside clusters coming online. A cluster that becomes available after cache warmup has already started will have its resources added to the running cache and will begin delivering reconcile events to the controller.

---

## StretchCluster Reconciliation

The `StretchCluster` reconciler runs on the raft leader. At a high level each reconcile pass does:

1. **Spec consistency check** — fetches the `StretchCluster` from every reachable peer cluster and verifies that `.spec` is identical everywhere. If specs differ on a reachable cluster, reconciliation is blocked with a `DriftDetected` status until the user realigns them. If some clusters are unreachable the check is skipped for those clusters and a `ClusterUnreachable` status is set, but reconciliation continues on the live clusters.

2. **Resource provisioning** — reconciles broker `StatefulSets`, `Services`, and related resources on each cluster according to the spec.

3. **Admin API operations** — once brokers are running, uses the Redpanda admin HTTP API to manage scale-down (decommission), configuration changes, and license application.

4. **Status propagation** — writes the observed status back to the `StretchCluster` resource on every cluster.

---

## Failure Modes

### A peer cluster's operator is not deployed yet

If a peer's gRPC endpoint does not exist when the raft leader starts up, kubeconfig fetches for that peer are retried in the background every five seconds. The raft group can still form a quorum from the clusters that are ready, elect a leader, and begin reconciling. The missing cluster's brokers simply do not exist in the spec yet; they will be engaged once their operator comes online and the kubeconfig is fetched and cached.

### The raft leader crashes

A new leader is elected from the remaining quorum members. The new leader reads each peer's kubeconfig from its local Secret cache and engages all clusters without needing any live gRPC call to the former leader. If the former leader's kubeconfig was never cached (i.e. the leader crashed before any member cached it), the new leader falls back to a live gRPC call to the former leader's cluster. If the former leader's operator pod has also not recovered by then, that cluster's engagement is deferred and retried.

Once the new leader has connected to all available clusters it resumes normal reconciliation.

### Kubernetes worker nodes on a peer cluster go down

If Kubernetes worker nodes on a peer cluster go down but the cluster's control plane (API server, etcd) remains available:

- The raft gRPC connection to that cluster's operator continues to function as long as the operator pod survives or is rescheduled onto a healthy node.
- The `StretchCluster` spec consistency check can still reach the peer's API server and continues to include that cluster in its checks. Reconciliation proceeds normally.
- Broker pods scheduled on the affected nodes move to `Unknown` or `Terminating` state. Redpanda will detect the broker loss and re-elect partition leaders accordingly.
- If the PVC unbinder is enabled, it detects broker pods that are stuck in `Pending` due to volume node-affinity constraints (the PVCs remain bound to the failed node's volumes). After a configurable interval it unbinds those PVCs, allowing Kubernetes to reschedule the pod onto a healthy node. Because the rescheduled pod comes up as a fresh broker with a new broker ID, the original broker entry is left behind as a ghost in Redpanda's metadata. Redpanda 26.1 natively detects and ejects ghost brokers after a set interval, cleaning up the stale entry without operator involvement.

### A peer cluster's Kubernetes API server becomes unreachable

If the Kubernetes API server for a peer cluster is unreachable (for example, due to a misconfigured firewall rule):

- The `StretchCluster` spec consistency check cannot contact that cluster. It records a `ClusterUnreachable` condition and continues reconciling on all reachable clusters. Reconciliation is **not** blocked.
- The operator's connection to the unreachable cluster is broken. Controllers watching resources on that cluster cannot refresh their local view of its state. Because cluster connections are independent, controllers watching other clusters proceed unaffected.
- All work scoped to the unreachable cluster is tied to the current leader's tenure. If the leader pod restarts or loses leadership, any in-progress work for that cluster is cancelled cleanly — it does not continue in the background.
- **If only the API server is unreachable but the underlying infrastructure is intact**, broker pods continue running and are still reachable via the Redpanda admin API. Redpanda itself remains healthy; partition leadership is unaffected. The operator cannot reconcile Kubernetes resources on that cluster (no StatefulSet updates, no status writes) but does not need to take any recovery action — the brokers are functioning normally. *Note that this will mean that no upgrades or operator-based remediation can occur on brokers in the degraded cluster.*
- **If all infrastructure is down** (network partition, cloud zone failure, etc.), broker pods are also unreachable. Redpanda detects the broker loss and re-elects partition leaders from the remaining clusters. The operator does not attempt to decommission the lost brokers — see the worker node failure case above for how recovery proceeds once connectivity is restored.
- When the API server recovers, the consistency check will resume including it. If specs have drifted while it was down, the condition will change to `DriftDetected` and reconciliation will pause until the user resolves it.

### Spec drift between clusters

If a user modifies the `StretchCluster` spec on one cluster without updating the others, the consistency check detects the divergence and sets `DriftDetected`. All reconciliation is paused across all reachable clusters until the specs are realigned. This is intentional — proceeding with a diverged spec would apply different configurations to different clusters, which could produce an inconsistent Redpanda cluster topology.

---

## Status Conditions

The `SpecSynced` condition on a `StretchCluster` reflects the outcome of the cross-cluster spec check:

| Reason              | Meaning                                                                                       |
|---------------------|-----------------------------------------------------------------------------------------------|
| `Synced`            | All clusters were reachable and their specs are identical. Normal operation.                  |
| `ClusterUnreachable`| One or more clusters could not be contacted. Check was partial; reconciliation continues on reachable clusters. |
| `DriftDetected`     | A reachable cluster has a different spec. Reconciliation is blocked until specs are aligned.  |
| `Error`             | A transient error occurred during the consistency check. Will be retried.                     |
| `TerminalError`     | A non-retryable error occurred.                                                               |

---

## TLS

gRPC between operator nodes is always protected by mutual TLS. A shared CA is used to issue per-node certificates. The CA file, certificate, and private key paths are required flags — the operator will not start without them.

### Initial bootstrapping

Before the operator is deployed, the `rpk k8s multicluster bootstrap` command performs a one-time setup across all participating clusters. It connects to each cluster using the contexts in your kubeconfig and does the following:

1. **Generates a shared root CA.** A single self-signed CA certificate (ECDSA P-256, 10-year lifetime) is created. The same CA is distributed to every cluster — this is what allows each node to verify the identity of its peers.

2. **Issues per-cluster leaf certificates.** For each cluster, a leaf certificate is signed by the shared CA and valid for one year. Each certificate carries both server and client authentication key usages, enabling mutual TLS in both directions. The DNS names on the certificate include the operator's service address within that cluster; custom FQDNs can be specified with `--dns-override` when the operator is reachable via a load-balanced or external address.

3. **Writes TLS Secrets to each cluster.** A Secret is created in each cluster containing three entries: the shared CA certificate, the cluster's own leaf certificate, and its private key. The operator reads these files at startup and uses them for all gRPC connections.

4. **Optionally creates the namespace.** If the target namespace does not exist, it is created automatically (controlled by `--create-namespace`, which defaults to true).

The key flags are:

| Flag | Default | Description |
|------|---------|-------------|
| `--context` | (required) | One or more kubeconfig context names identifying the clusters to bootstrap |
| `--namespace` | `redpanda` | Namespace where TLS Secrets are created |
| `--service-name` | `redpanda-multicluster` | Prefix used when naming the generated Secrets |
| `--dns-override` | (none) | Custom DNS name for a cluster's certificate SANs, in `context=address` format |
| `--organization` | `Redpanda` | Organization field in generated certificate subjects |

### Certificate rotation

To rotate certificates, re-run `rpk k8s multicluster bootstrap`. It generates a new CA and new per-cluster leaf certificates and overwrites the TLS Secrets in each cluster. The operator watches its certificate files on disk and reloads them automatically when the Secret's volume remounts — no restart is required.
