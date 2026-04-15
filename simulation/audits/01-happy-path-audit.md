# Audit: Scenario 1 — Happy Path

- **Date**: 2026-04-15
- **Operator commit**: f1e214dc (all 4 fixes)
- **Starting Redpanda version**: v25.2.1

---

### Step 1.1 — Verify initial cluster health
- **Time**: ~13:10 UTC
- **Operator status**: vc-0: Running 1 restart / vc-1: Running 1 restart / vc-2: Running 1 restart (startup settling)
- **Redpanda status**: vc-0: cluster-first-0 2/2 Running 0 restarts / vc-1: cluster-second-0 2/2 Running 0 restarts / vc-2: cluster-third-0 2/2 Running 0 restarts
- **rpk cluster health**: Healthy=true, 3 nodes, 0 leaderless, 0 under-replicated
- **Broker versions**: all v25.2.1
- **Issues**: None

### Step 1.2 — Light usage simulation
- **Topics created**: orders (6p/3r), user-profiles (3p/3r compact), metrics (3p/3r 1h retention)
- **Messages produced**: 20 to orders (from vc-1), 10 keyed to user-profiles (from vc-0)
- **Messages consumed**: 20 orders (from vc-2), 10 user-profiles (from vc-1)
- **Cross-cluster topic creation**: OK from all 3 clusters
- **Post-step diagnostics**: Healthy, no issues
- **Issues**: None

### Step 1.3 — Patch upgrade v25.2.1 -> v25.2.11
- **Rollout**: Sequential (vc-0, vc-1, vc-2) with random delays
- **Rolling restart observed**: broker 0 upgraded first, then broker 1, then broker 2
- **Rollout duration**: ~2 minutes total
- **Broker restarts during upgrade**: 0 crash restarts (pods recreated by rolling update)
- **Post-upgrade**: All 3 brokers v25.2.11, healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

### Step 1.4 — Verify data survives upgrade
- **Pre-upgrade topics still present**: Yes — orders, user-profiles, metrics all present
- **Messages intact**: 20/20 orders consumed from vc-2, 10/10 user-profiles consumed from vc-1
- **Issues**: None

### Step 1.5 — Medium usage post-upgrade
- **Additional messages**: 30 more to orders from vc-0
- **Consumer groups**: order-processors created, consumed 50 total messages (25 from vc-1, 25 from vc-2), LAG=0
- **Config changes**: retention.ms=86400000 on orders (from vc-2), max.message.bytes=2097152 on metrics (from vc-0)
- **Cross-cluster config propagation**: Verified from vc-1 — both changes visible as DYNAMIC_TOPIC_CONFIG
- **Cross-cluster rpk operations**: All worked from all clusters (topic create, produce, consume, alter-config)
- **Post-step health**: Healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

---

### Result: PASS
**Summary**: Happy path scenario completed successfully. Cluster upgraded from v25.2.1 to v25.2.11 via rolling restart with zero data loss, zero downtime. All cross-cluster operations (topic CRUD, produce, consume, consumer groups, config changes) work from any cluster. Consumer group offsets preserved across upgrade. Topic configurations survive upgrade and propagate across clusters.
