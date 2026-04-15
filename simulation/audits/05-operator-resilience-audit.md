# Audit: Scenario 5 — Operator Resilience

- **Date**: 2026-04-15
- **Operator commit**: f1e214dc (all 4 fixes)
- **Starting Redpanda version**: v25.2.1

---

### Step 5.1 — Verify initial cluster health
- **Cluster health**: Healthy, 3 brokers v25.2.1
- **Raft leader**: vc-1 (active reconciler)
- **Issues**: None

### Step 5.2 — Create workload
- **Topics**: orders (6p/3r), events (3p/3r)
- **Messages**: 30 orders (vc-1), 20 events (vc-2) = 50 total
- **Issues**: None

### Step 5.3 — Kill operator in leader vCluster (vc-1)
- **Command**: `kubectl delete pod redpanda-operator-9b9968756-cjg2c`
- **Issues**: None

### Step 5.4 — Verify Redpanda unaffected
- **Redpanda health**: Healthy, all 3 brokers alive
- **Leaderless partitions**: 0
- **Under-replicated**: 0
- **Data plane impact**: NONE — killing the operator has zero effect on Redpanda brokers
- **Issues**: None

### Step 5.5 — Verify operator recovers
- **New operator pod**: Running within ~15 seconds (Deployment auto-restart)
- **New raft leader**: vc-2 took over reconciliation
- **Time to recover**: <30 seconds
- **Issues**: None

### Step 5.6 — Produce/consume during recovery
- **Produce**: 10 orders from vc-0 during operator outage — OK
- **Consume**: All 40 orders (30 + 10) from vc-2 — OK
- **Data plane operational during outage**: YES
- **Issues**: None

### Step 5.7 — Kill operator in non-leader vCluster (vc-0)
- **Command**: `kubectl delete pod redpanda-operator-68788666b8-pgtcq`
- **Issues**: None

### Step 5.8 — Verify everything still works
- **Redpanda health**: Healthy, unaffected
- **New operator pod in vc-0**: Running within ~15 seconds
- **Raft leader**: vc-2 still active (killing a follower doesn't trigger re-election)
- **Issues**: None

### Step 5.9 — Trigger upgrade after recovery
- **Upgrade**: v25.2.1 -> v25.2.11, sequential patching
- **Rollout**: All 3 brokers upgraded successfully
- **Transient leaderless**: 10 partitions briefly leaderless after last broker restarted (~seconds, resolved)
- **Final health**: Healthy, 0 leaderless, 0 under-replicated
- **Issues**: Transient leaderless during post-upgrade settling (same as other scenarios)

---

### Result: PASS
**Summary**: Operator resilience verified. Killing the raft leader operator causes automatic re-election to a different vCluster within seconds. Killing a follower operator has no effect on leadership. In both cases:
- Redpanda data plane is completely unaffected (zero impact on brokers)
- Data can be produced and consumed during operator outage
- New operator pod auto-starts via Deployment
- Raft re-elects a new leader when the leader is killed
- The recovered operator can successfully drive a rolling upgrade

Key finding: the operator is a pure control plane component — its failure/restart has zero impact on Redpanda cluster availability.
