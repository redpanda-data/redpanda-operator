# Audit: Scenario 3 — Upgrade Then Rollback

- **Date**: 2026-04-15
- **Operator commit**: f1e214dc (all 4 fixes)
- **Starting Redpanda version**: v25.2.1

---

### Step 3.1 — Verify initial cluster health
- **Cluster health**: Healthy, 3 brokers v25.2.1
- **Issues**: None

### Step 3.2 — Create workload
- **Topics**: orders (6p/3r), user-profiles (3p/3r compact), metrics (3p/3r)
- **Messages**: 40 orders (vc-1), 15 user-profiles (vc-0), 20 metrics (vc-2) = 75 total
- **Consumer group**: order-processors consumed 20/40 orders
- **Config change**: orders retention.ms=86400000
- **Issues**: None

### Step 3.3 — Record pre-upgrade state
- **Topics**: 3 (metrics, orders, user-profiles)
- **order-processors LAG**: 20
- **orders retention.ms**: 86400000

### Step 3.4 — Upgrade v25.2.1 -> v25.2.11
- **Rollout**: Sequential, all 3 brokers upgraded to v25.2.11
- **Issues**: None

### Step 3.5 — Produce more data on new version
- **Additional messages**: 20 orders (post-upgrade, from vc-0), 10 user-profiles (from vc-2)
- **Issues**: None

### Step 3.6 — Record pre-rollback state
- **order-processors LAG**: 40 (20 unconsumed pre-upgrade + 20 new post-upgrade)
- **orders retention.ms**: 86400000

### Step 3.7 — Rollback v25.2.11 -> v25.2.1
- **Rollout**: Sequential, all 3 brokers rolled back to v25.2.1
- **Broker crashes during rollback**: 0
- **Issues**: None

### Step 3.8 — Verify data integrity post-rollback
- **All topics present**: Yes (3/3)
- **Pre-upgrade data intact**: Yes (40 orders with "widget" prefix consumable)
- **Post-upgrade data intact**: Yes (20 orders with "post-upgrade" prefix consumable)
- **Consumer group offsets intact**: Yes (LAG=40, unchanged through upgrade+rollback cycle)
- **Topic configs preserved**: Yes (retention.ms=86400000 survived round trip)
- **User-profiles**: All 25 messages (15 pre + 10 post) consumable
- **Cluster health**: Healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

### Step 3.9 — Continue usage after rollback
- **Post-rollback produce**: 20 orders from vc-2 — OK
- **Post-rollback consume**: 20 consumed with existing group from vc-0 — OK
- **Post-rollback config change**: metrics retention.ms=7200000 from vc-1 — OK
- **Final health**: Healthy
- **Issues**: None

---

### Result: PASS
**Summary**: Full upgrade + rollback cycle completed successfully. Data produced on both the original version (v25.2.1) and the upgraded version (v25.2.11) survived the rollback to v25.2.1 with zero loss. Consumer group offsets preserved exactly through the entire upgrade → produce → rollback cycle (LAG=40 unchanged). Topic configurations survived the round trip. All cross-cluster operations work after rollback.
