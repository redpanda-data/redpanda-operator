# Scenario 5: Operator Resilience — Kill and Recover

**Goal**: Verify that killing the operator pod (simulating a crash) does not
affect Redpanda cluster health, and that the operator recovers automatically.

## Steps

| Step | Action | Skill | Details |
|------|--------|-------|---------|
| 5.1 | Verify initial cluster health | debug-multicluster-operator + debug-redpanda-in-multicluster | Baseline, identify raft leader |
| 5.2 | Create workload | simulate-redpanda-usage-in-multicluster light | Some topics and data |
| 5.3 | Kill operator in leader vCluster | manual | `kubectl delete pod` the operator in the raft leader's vCluster |
| 5.4 | Verify Redpanda unaffected | debug-redpanda-in-multicluster | Brokers should stay running |
| 5.5 | Verify operator recovers | debug-multicluster-operator | New pod starts, raft re-elects |
| 5.6 | Produce/consume during recovery | simulate-redpanda-usage-in-multicluster light | Data plane works without operator |
| 5.7 | Kill operator in non-leader vCluster | manual | Kill a follower operator |
| 5.8 | Verify everything still works | debug-multicluster-operator + debug-redpanda-in-multicluster | Both operator and Redpanda healthy |
| 5.9 | Trigger upgrade after recovery | upgrade-redpanda-in-multicluster v25.2.11 | Verify operator can still drive upgrades |

## Success Criteria

- Killing any single operator pod does not affect Redpanda brokers
- Operator pod auto-restarts via Deployment
- Raft re-election happens (if leader killed)
- Data plane continues serving reads/writes during operator outage
- Operator can drive a rolling upgrade after recovery
