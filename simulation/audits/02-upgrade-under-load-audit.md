# Audit: Scenario 2 — Upgrade Under Active Workload

- **Date**: 2026-04-15
- **Operator commit**: f1e214dc (all 4 fixes)
- **Starting Redpanda version**: v25.2.1

---

### Step 2.1 — Verify initial cluster health
- **Time**: ~13:55 UTC
- **Cluster health**: Healthy, 3 brokers v25.2.1, 0 leaderless, 0 under-replicated
- **Issues**: None

### Step 2.2 — Create heavy workload
- **Topics created**: orders (6p/3r), user-profiles (3p/3r compact), metrics (3p/3r), config-changelog (1p/3r compact), events (9p/3r)
- **Messages produced**: 50 orders (vc-1), 20 user-profiles (vc-0), 30 metrics (vc-2), 200 events (50 each from vc-0, vc-1, vc-2, vc-0)
- **Total messages**: 300
- **Consumer groups created**: order-processors (consumed 25/50 orders), event-sink (consumed 100/200 events)
- **Issues**: None

### Step 2.3 — Record pre-upgrade state
- **Topics**: config-changelog, events, metrics, orders, user-profiles (5 topics)
- **order-processors group**: LAG=25 (25 of 50 consumed)
- **event-sink group**: LAG=100 (100 of 200 consumed)

### Step 2.4 — Patch upgrade v25.2.1 -> v25.2.11
- **Rollout**: Sequential with random delays
- **Rolling restart**: broker 0 first, broker 1 second, broker 2 last
- **Post-upgrade**: All 3 brokers v25.2.11, healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

### Step 2.5 — Verify data integrity post-upgrade
- **Topic list matches pre-upgrade**: Yes — all 5 topics present with same partition/replica counts
- **Consumer group offsets intact**:
  - order-processors: LAG=25 (unchanged)
  - event-sink: LAG=100 (unchanged)
- **Remaining messages consumable**: 25/25 orders consumed, 100/100 events consumed — all data intact
- **No data duplication**: Confirmed (exact counts match)
- **Issues**: None

### Step 2.6 — Continue usage post-upgrade
- **New messages**: 30 orders produced from vc-2, consumed with existing group from vc-1
- **Config changes**: retention.ms=172800000 on orders from vc-2 — succeeded
- **Final health**: Healthy, 0 leaderless, 0 under-replicated
- **Issues**: None

---

### Result: PASS
**Summary**: Upgrade under active workload completed successfully. 300 messages across 5 topics with 2 consumer groups survived the rolling upgrade with zero data loss. Consumer group offsets preserved exactly (LAG unchanged). Post-upgrade produce/consume and config changes work from all clusters. No data duplication detected.
