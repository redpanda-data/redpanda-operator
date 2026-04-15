# Scenario 3: Upgrade Then Rollback

**Goal**: Verify that rolling back to the previous version after an upgrade
restores full functionality without data loss.

## Steps

| Step | Action | Skill | Details |
|------|--------|-------|---------|
| 3.1 | Verify initial cluster health | debug-redpanda-in-multicluster | Baseline check |
| 3.2 | Create workload | simulate-redpanda-usage-in-multicluster medium | Topics, messages, consumer groups |
| 3.3 | Record pre-upgrade state | manual | Topic list, offsets, message counts |
| 3.4 | Upgrade v25.2.1 -> v25.2.11 | upgrade-redpanda-in-multicluster v25.2.11 | Patch upgrade |
| 3.5 | Produce more data on new version | simulate-redpanda-usage-in-multicluster light | Additional data after upgrade |
| 3.6 | Record pre-rollback state | manual | Topic list, offsets, message counts |
| 3.7 | Rollback v25.2.11 -> v25.2.1 | upgrade-redpanda-in-multicluster v25.2.1 | Roll back via same patch mechanism |
| 3.8 | Verify data integrity post-rollback | manual | Both pre-upgrade and post-upgrade data intact |
| 3.9 | Continue usage after rollback | simulate-redpanda-usage-in-multicluster medium | Full workload on original version |

## Success Criteria

- Rollback completes without broker crashes
- All data (from both pre-upgrade and post-upgrade phases) is consumable
- Consumer group offsets preserved through upgrade + rollback cycle
- Topic configurations survive the round trip
- Cluster health returns to fully healthy state
