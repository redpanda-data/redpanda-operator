# Multicluster operator — failure-injection repro scripts

Standalone scripts that reproduce specific resilience bugs in the multicluster
operator by injecting network faults with [Pumba](https://github.com/alexei-led/pumba)
and reading back observable effects from operator logs / reconcile durations.

Each script is an opinionated, one-shot demo: it picks a target, applies a
specific fault, waits, measures, prints a PASS/FAIL verdict, and cleans up on
exit (including Ctrl-C). No state is left behind in a healthy run.

## What each script reproduces

| Script | Finding | Injection |
|---|---|---|
| `finding-01-dosend-stall.sh`      | raft `DoSend` synchronous in Ready loop → leader churn under asymmetric silent drop | 100% packet loss on leader→peer tcp/9443 |
| `finding-02-03-admin-unbounded.sh` | admin-client `ClientTimeout` dropped on StretchCluster path + reconcile has no `context.WithTimeout` | `netem delay 30s` on leader egress to admin tcp/9644 |
| `finding-04-serial-fanout.sh`     | `syncStatus`/`setupLicense`/Phase-1 scans iterate clusters serially with no reachability check | `netem delay` on leader egress to one peer's K8s API |
| `finding-05-probe-flap.sh`        | health probe has no hysteresis at marginal RTT → `SpecSynced` flaps | `netem delay 4.5s ± 500ms` on leader egress to one peer's K8s API |

## Requirements

- `kubectl`, with a context pointing at the cluster (or clusters) under test
- A Linux node capable of running privileged pods with `hostPID` and a bind-mounted `/run` / `/var/lib` — this is fine on EKS/GKE/AKS standard node pools, k3s, k3d, and kind, but not on GKE Autopilot, EKS Fargate, or Bottlerocket (which lack the tc-netem kernel module)
- Outbound internet from the target node to `ghcr.io` to pull `pumba:1.0.6` and `pumba-alpine-nettools:latest`

The scripts deploy the Pumba Job and a privileged `netshoot` debug pod on the
node that hosts the target operator, and clean both up on exit.

## Running

### Default: local vcluster-on-k3d dev env

Stand up the repo's dev environment with:

```bash
task dev:setup-multicluster-dev-env
```

Then run any of the scripts with no flags — they auto-detect the three
vcluster namespaces and the current raft leader:

```bash
./finding-05-probe-flap.sh
```

### Real multi-cluster (multi-cloud or multi-region)

Pass comma-separated kube contexts, one per cluster:

```bash
./finding-01-dosend-stall.sh \
  --contexts=eks-us-east-1,gke-us-central1,aks-eastus2 \
  --namespace=redpanda \
  --containerd-sock=/run/containerd/containerd.sock
```

The containerd socket defaults to k3s's `/run/k3s/containerd/containerd.sock`;
standard EKS/GKE/AKS node pools use `/run/containerd/containerd.sock`.

### All available flags

Every script prints its full flag list with `--help`, including the common
flags (`--contexts`, `--namespace`, `--operator-label`, `--operator-service`,
`--containerd-sock`, `--pumba-image`, `--tc-image`).

## How results are interpreted

Each script prints a `RESULT:` line:

- `CONFIRMED` — the bug was observed end-to-end (elections triggered, timeouts
  fired, flaps observed, etc.)
- `NOT REPRODUCED` — the observation window elapsed without the expected
  signal. Either the fix is applied, the latency/duration is off, or the
  filter missed. Each script suggests the first thing to bump.

The scripts exit 0 on CONFIRMED and non-zero on NOT REPRODUCED, so they can
drive CI when desired. In pure demo mode, the exit code can be ignored.

## Limitations

These are script-driven repros, not integration tests — they rely on Pumba,
tc netem, and network-layer timing. They are meant to catch the *class* of
bug rather than the *exact* line-of-code regression. Line-level regression
coverage for each finding should live in unit tests alongside the fix.
