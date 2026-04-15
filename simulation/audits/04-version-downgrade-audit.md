# Audit: Scenario 4 — Version Downgrade Under Load

- **Date**: 2026-04-15
- **Operator commit**: f1e214dc (all 4 fixes)
- **Starting Redpanda version**: v25.2.1

---

### Step 4.1 — Verify initial cluster health
- **Cluster health**: Healthy, 3 brokers v25.2.1
- **Issues**: None

### Step 4.2 — Create workload
- **Topics**: orders (6p/3r), user-profiles (3p/3r compact), metrics (3p/3r)
- **Messages**: 30 orders (vc-1), 15 user-profiles (vc-0), 20 metrics (vc-2) = 65 total
- **Consumer group**: order-watchers consumed 15/30 orders
- **Transient issue**: NOT_CONTROLLER error on topic create from vc-2 (succeeded on retry — topic was actually created)
- **Issues**: Minor — transient NOT_CONTROLLER on cross-cluster topic create

### Step 4.3 — Record pre-downgrade state
- **Topics**: 3 (metrics, orders, user-profiles)
- **order-watchers LAG**: 15

### Step 4.4 — Downgrade v25.2.1 -> v25.1.1
- **Rollout**: Sequential, all 3 brokers downgraded to v25.1.1
- **Transient issues**: 8 leaderless partitions (including controller partition) immediately after all brokers reached v25.1.1 — resolved within seconds
- **Issues**: Transient leaderless partitions during raft re-election (expected for minor version downgrade)

### Step 4.5 — Verify cluster stability
- **Health**: Recovered to healthy within seconds
- **Pod restarts**: 0 across all clusters
- **Time to stabilize**: <10 seconds

### Step 4.6 — Verify data integrity
- **All topics present**: Yes (3/3)
- **order-watchers LAG**: 15 (preserved)
- **Remaining orders consumed**: 15/15
- **user-profiles consumed**: 15/15
- **Issues**: None

### Step 4.7 — Usage on downgraded version
- **Produce**: 10 orders on v25.1.1 from vc-1 — OK
- **Consume**: All 40 orders (30 original + 10 new) consumable from vc-2 — OK
- **Issues**: None

### Step 4.8 — Re-upgrade v25.1.1 -> v25.2.1
- **Rollout**: Sequential, all 3 brokers back to v25.2.1
- **Issues**: None

### Step 4.9 — Final health check
- **Cluster health**: Healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

---

### Result: PASS
**Summary**: Minor version downgrade (v25.2.1 -> v25.1.1) and re-upgrade completed successfully. Transient leaderless partitions (~8 partitions for <10 seconds) observed during the downgrade settling — expected behavior for a minor version change as raft re-elects leaders. Zero data loss, consumer group offsets preserved, all operations functional on the downgraded version. Re-upgrade back to v25.2.1 was clean.

### Notable observation
The transient leaderless partitions during downgrade (including the controller partition) suggest that minor version downgrades cause a brief cluster disruption that wouldn't occur in a patch upgrade. This is worth documenting as expected behavior for users.
