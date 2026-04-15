# Scenario 1: Happy Path — Basic Usage Then Patch Upgrade

**Goal**: Verify that a standard workflow (create topics, produce/consume, then
upgrade to a patch release) works without data loss or downtime.

## Steps

| Step | Action | Skill | Details |
|------|--------|-------|---------|
| 1.1 | Verify initial cluster health | debug-redpanda-in-multicluster | Confirm all 3 brokers healthy, StretchCluster Ready |
| 1.2 | Light usage simulation | simulate-redpanda-usage-in-multicluster light | Create topics, basic produce/consume |
| 1.3 | Patch upgrade v25.2.1 -> v25.2.11 | upgrade-redpanda-in-multicluster v25.2.11 | Sequential patch across clusters |
| 1.4 | Verify data survives upgrade | manual | Consume from pre-existing topics, verify messages intact |
| 1.5 | Medium usage post-upgrade | simulate-redpanda-usage-in-multicluster medium | Consumer groups, config changes |

## Success Criteria

- Zero broker restarts during upgrade
- No leaderless or under-replicated partitions after upgrade settles
- All messages produced pre-upgrade are consumable post-upgrade
- Consumer group offsets preserved across upgrade
