# Scenario 2: Upgrade Under Active Workload

**Goal**: Verify that upgrading while the cluster has active topics and consumer
groups does not cause data loss or group offset corruption.

## Steps

| Step | Action | Skill | Details |
|------|--------|-------|---------|
| 2.1 | Verify initial cluster health | debug-redpanda-in-multicluster | Baseline check |
| 2.2 | Create heavy workload | simulate-redpanda-usage-in-multicluster heavy | Many topics, messages, consumer groups |
| 2.3 | Record pre-upgrade state | manual | Note: topic list, message counts, group offsets |
| 2.4 | Patch upgrade v25.2.1 -> v25.2.11 | upgrade-redpanda-in-multicluster v25.2.11 | Upgrade while data exists |
| 2.5 | Verify data integrity post-upgrade | manual | Compare topic list, consume remaining, check group offsets |
| 2.6 | Continue usage post-upgrade | simulate-redpanda-usage-in-multicluster medium | Produce/consume on existing topics |

## Success Criteria

- Topic list identical before and after upgrade
- All pre-upgrade messages consumable
- Consumer group offsets unchanged (no offset reset)
- No data duplication
- Compacted topic (`user-profiles`) keys intact
