# Scenario 4: Version Downgrade Under Load

**Goal**: Verify that downgrading to an older minor version (v25.2.1 -> v25.1.1)
works correctly. This is a more aggressive operation than a patch rollback.

## Steps

| Step | Action | Skill | Details |
|------|--------|-------|---------|
| 4.1 | Verify initial cluster health | debug-redpanda-in-multicluster | Baseline check |
| 4.2 | Create workload | simulate-redpanda-usage-in-multicluster medium | Topics, messages, groups |
| 4.3 | Record pre-downgrade state | manual | Full state snapshot |
| 4.4 | Downgrade v25.2.1 -> v25.1.1 | upgrade-redpanda-in-multicluster v25.1.1 | Minor version downgrade |
| 4.5 | Verify cluster stability | debug-redpanda-in-multicluster | May see transient issues |
| 4.6 | Verify data integrity | manual | Check all topics, messages, groups |
| 4.7 | Usage on downgraded version | simulate-redpanda-usage-in-multicluster light | Confirm basic operations work |
| 4.8 | Re-upgrade v25.1.1 -> v25.2.1 | upgrade-redpanda-in-multicluster v25.2.1 | Return to original version |
| 4.9 | Final health check | debug-redpanda-in-multicluster | Cluster fully healthy |

## Success Criteria

- Downgrade completes (may have transient issues — document them)
- Cluster converges to healthy state after downgrade
- Data accessible on older version
- Re-upgrade back to original version works cleanly
