# StretchCluster Simulation Testing

Simulation tests that exercise the StretchCluster deployment the way a real user would — creating topics, producing/consuming messages, upgrading Redpanda versions, rolling back, and killing operator pods — to verify stability, correctness, and resilience.

## Why Simulation Testing

Unit and integration tests verify code correctness, but can miss issues that only surface under realistic multi-cluster operation:

- Cross-cluster DNS resolution and advertised address correctness
- Rolling upgrade coordination across 3 independent Kubernetes clusters
- Data integrity (messages, consumer group offsets, topic configs) through version changes
- Operator crash recovery and raft re-election
- Interactions between concurrent reconciliation and user operations

This framework found 4 real bugs in the first run that existing test suites missed.

## Prerequisites

### Environment

The simulation runs on a local k3s cluster with 3 vClusters (`vc-0`, `vc-1`, `vc-2`), each running a redpanda-operator and cert-manager. Together they form a StretchCluster managing a Redpanda cluster distributed across all 3 Kubernetes clusters.

Set up the environment:

```bash
nix develop -c task dev:setup-multicluster-dev-env
```

### Pre-loaded Redpanda Images

These images are pre-imported into the k3d cluster and available for testing:

- `redpandadata/redpanda:v25.1.1`
- `redpandadata/redpanda:v25.2.1` (default starting version)
- `redpandadata/redpanda:v25.2.11`

### Environment Layout

| vCluster | NodePool name | Broker pod | Namespace |
|----------|--------------|------------|-----------|
| vc-0 | first | cluster-first-0 | default |
| vc-1 | second | cluster-second-0 | default |
| vc-2 | third | cluster-third-0 | default |

StretchCluster name: `cluster`

### Skills (Claude Code)

Four skills support this framework. They are defined in `~/.claude/skills/` and invocable as slash commands:

| Skill | Purpose |
|-------|---------|
| `/debug-multicluster-operator` | Check operator pod health, restarts, logs, raft leader across vClusters |
| `/debug-redpanda-in-multicluster` | Check broker pod health, rpk cluster health, StretchCluster/NodePool CR status |
| `/upgrade-redpanda-in-multicluster <tag>` | Patch NodePool image tags sequentially with random delays, monitor rollout |
| `/simulate-redpanda-usage-in-multicluster <level>` | Create topics, produce/consume messages, manage consumer groups across clusters |

## Executing Commands Inside vClusters

All kubectl/rpk commands targeting a vCluster must be prefixed with:

```bash
vcluster connect -n <vc> <vc> -- <command>
```

Example:

```bash
vcluster connect -n vc-0 vc-0 -- kubectl exec cluster-first-0 -c redpanda -- rpk cluster health
```

## Structure

```
simulation/
  README.md                  # This file
  scenarios/                 # Scenario definitions (what to do)
    01-happy-path.md
    02-upgrade-under-load.md
    03-upgrade-then-rollback.md
    04-version-downgrade.md
    05-operator-resilience.md
  audits/                    # Audit logs (what happened)
    01-happy-path-audit.md
    02-upgrade-under-load-audit.md
    03-upgrade-then-rollback-audit.md
    04-version-downgrade-audit.md
    05-operator-resilience-audit.md
```

## Execution Protocol

### Running a Scenario

1. Read the scenario file (e.g., `scenarios/01-happy-path.md`) for the step-by-step plan
2. Set up a fresh environment (`nix develop -c task dev:setup-multicluster-dev-env`)
3. Execute each step in order
4. After **every** step, run diagnostics and record findings in the audit file

### After Every Step

1. Run `/debug-multicluster-operator all` — check operator pods, restarts, logs
2. Run `/debug-redpanda-in-multicluster all` — check broker pods, rpk health, CR status
3. Update the corresponding audit file with findings

For efficiency, the diagnostic checks can be abbreviated when previous steps showed no issues — focus on `rpk cluster health` and pod restart counts as quick checks, and only do deep investigation when something looks wrong.

### When an Error is Found

1. Run full diagnostics (both debug skills)
2. Check operator logs for errors: `vcluster connect -n <vc> <vc> -- kubectl logs deploy/redpanda-operator --tail=50`
3. Record the error with full evidence in the audit file
4. **Pause and check with the user** before continuing — the error may indicate a bug that needs fixing before the scenario can proceed

### Recording Results

Each audit file has a structured template. Fill in:

- **Timestamps** for each step
- **Concrete numbers**: messages produced/consumed, topic counts, consumer group LAGs
- **Pod status**: restarts, ready state
- **CR conditions**: StretchCluster and NodePool condition summaries
- **Issues**: any errors, with the supporting evidence (command output, log excerpts)
- **Result**: PASS or FAIL with a summary

## Scenarios

| # | Name | Focus | Key Operations |
|---|------|-------|----------------|
| 1 | Happy Path | Basic workflow | Create topics → produce/consume → patch upgrade → verify data |
| 2 | Upgrade Under Load | Data during upgrade | Heavy workload → upgrade → verify offsets + data integrity |
| 3 | Upgrade Then Rollback | Version round-trip | Upgrade → produce on new version → rollback → verify all data |
| 4 | Version Downgrade | Minor version change | Downgrade v25.2→v25.1 → verify → re-upgrade |
| 5 | Operator Resilience | Crash recovery | Kill leader operator → kill follower → verify data plane unaffected → upgrade |

### Adding New Scenarios

1. Create `scenarios/NN-name.md` with a step table and success criteria
2. Create `audits/NN-name-audit.md` with the structured template (copy from an existing audit)
3. Update this README's scenario table

Good scenarios to add:
- StretchCluster spec changes (cluster config, TLS rotation)
- Scale up/down (add/remove NodePools)
- Network partition simulation (disconnect a vCluster)
- Concurrent operations (upgrade while producing)

## Results (2026-04-15)

| Scenario | Result | Notable Findings |
|----------|--------|-----------------|
| 1. Happy Path | PASS | Clean |
| 2. Upgrade Under Load | PASS | Zero data loss, offsets preserved exactly |
| 3. Upgrade Then Rollback | PASS | Data from both versions survived rollback |
| 4. Version Downgrade | PASS | Transient leaderless partitions (~seconds) during minor downgrade |
| 5. Operator Resilience | PASS | Killing operator has zero impact on data plane |

### Bugs Found During Initial Run

4 bugs were discovered and fixed before the scenarios could pass:

1. **Cross-cluster advertised Kafka/HTTP addresses** (`scripts.go`) — advertised addresses used StatefulSet pod FQDN which doesn't resolve across clusters
2. **CA secret lookup for issuerRef TLS certs** (`stretch_cluster.go`) — admin client couldn't find CA when using external cert-manager issuers
3. **NodePool image upgrades not rolling StatefulSets** (`pool.go`) — changing NodePool `spec.image.tag` didn't trigger StatefulSet update because only StretchCluster generation was checked
4. **rpk/client broker addresses** (`render_state.go`) — base config broker lists used same broken FQDN pattern as advertised addresses
