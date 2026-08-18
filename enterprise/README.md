# enterprise

Staging module for the enterprise-only feature set (stretch clusters /
multicluster), structured so it can eventually lift-and-shift to
`github.com/redpanda-data/operator-enterprise` as a directory move plus a
module-path rewrite.

## The dependency rule

This module must not import any other module in this monorepo. Its allowed
dependencies are `github.com/redpanda-data/common-go/*` and third-party
modules only. The dependency direction is strictly one-way: the OSS operator
imports this module, never the reverse.

The boundary is enforced three ways:

1. `task lint:enterprise-boundary` — `GOWORK=off go build ./...` from this
   directory: with no sibling requires in go.mod, any monorepo import fails to
   resolve (workspace mode would silently resolve them, hence GOWORK=off).
2. A depguard rule in the repo's `.golangci.yml` scoped to `enterprise/**`.
3. `lint/boundary_test.go` — parses go.mod (no monorepo or filesystem
   requires/replaces) and every import in the module.

## Layout

Mirrors the operator-enterprise repo so the lift is mechanical:

- `operator/api/redpanda/v1alpha2` — StretchCluster + RedpandaBrokerPool CRD
  types (same `cluster.redpanda.com/v1alpha2` group as the OSS API package,
  own SchemeBuilder registering only these kinds), plus forked copies of the
  shared value structs the stretch spec embeds (see Drift guards).
- `operator/controller` — MulticlusterReconciler + BrokerPoolReconciler and
  the remediation cores (ghost-broker maintenance clearing, stale-disk wipe).
  `seam.go` defines the integration contract (see The seam pattern).
- `operator/lifecycle` — a deliberate one-time de-genericization of the OSS
  lifecycle framework, hard-bound to `*StretchClusterWithPools`, plus the
  stretch resource managers and pool tracking.
- `operator/render` — the chart-free renderer (StatefulSets, ConfigMaps,
  Services, certs, PDBs, ServiceMonitors, MCS).
- `operator/statuses` (+ `operator/statuses.yaml`) — generated status
  conditions for the two kinds.
- `operator/observability` — the StretchCluster metrics + recorder and the
  maintenance-mode counters (recorded only by the remediation cores here).
- `operator/config/crd/bases` — generated CRDs + embed accessors.
- `operator/cmd/rpk-k8s/k8s/multicluster` — the `rpk k8s multicluster` CLI.
- `operator/scheme.go` — the multicluster runtime scheme (module root so the
  lifecycle tests can use it without an import cycle).
- `operator/tplutil` — pod/service template strategic-merge helpers.
- `pkg/multicluster` — the raft-backed manager, leader election (+ gRPC
  transport proto), cross-cluster bootstrap, CA watcher. `manager.go` is a
  structural mirror of the OSS `pkg/multicluster.Manager` interface; neither
  side imports the other, and compile-time assertions in the OSS
  `operator/cmd/multicluster` pin the method sets together.
- `pkg/testutil` — the minimal test-helper mirror this module's tests need.

Regenerate everything with `nix develop -c task enterprise:generate` (part of
the root `task generate` chain).

## The seam pattern

Anything the code here needs from the chart-coupled OSS operator arrives
through injected seams defined in `operator/controller/seam.go`:
`ClientFactory` (admin clients), `ClusterConfigSyncer` (+ `ConfigSyncMode`
mirroring `syncclusterconfig.SyncerMode` 1:1), `FeatureGate` (annotation
feature flags), `ReconcilerWrapper` (generic reconcile metrics/tracing), and
the client error classifiers — all fields of `MulticlusterSetupParams`. The
OSS operator constructs the concrete implementations
(`operator/internal/controller/redpanda/enterprise_adapters.go`,
`OSSMulticlusterSeams`) and passes them to `SetupMulticlusterController` /
`SetupWithMultiClusterManager` from `operator/cmd/multicluster`.

To carve out another enterprise feature, follow the same recipe: define the
interfaces it needs from the OSS side next to its controller here, implement
adapters OSS-side, and keep the types it shares with OSS as deliberate forks
with drift guards.

## Drift guards

Where this module deliberately duplicates something it cannot import, a CI
test pins the copy to its source of truth. When one fails, port the change
across (or record an intentional divergence), then update the guard.

| Duplication | Guard |
|---|---|
| Forked API value structs (~43) | source-level type-decl comparison (incl. kubebuilder markers), `operator/internal/enterprisedrift/source_drift_test.go` |
| `render/hosttuner.go` (7 chart symbols) | `operator/internal/enterprisedrift/drift_test.go` |
| `render/clusterconfig.go` (Fixup wire contract + CEL names) | same, plus a JSON round-trip of the Fixup wire type |
| `controller/superusers.go` | behavioral equivalence, `operator/internal/enterprisedrift` |
| `controller/shared.go` (FinalizerKey, requeue constants) | `operator/internal/controller/redpanda/enterprise_shared_drift_test.go` |
| `controller/roll_helpers.go` (roll-safety decision functions) | source-level function comparison, `operator/internal/enterprisedrift/source_drift_test.go` |
| Stretch metric names vs chart Prometheus rules | `operator/internal/enterprisedrift/metrics_drift_test.go` (matches names against actual rule expressions) |
| Concretized lifecycle (incl. `stretch_shared_helpers.go`'s v2 helper copies) | `operator/lifecycle/forkledger_test.go` (sha256 pins on OSS ancestors) |
| `pkg/multicluster/manager.go` mirror | compile-time assertions in `operator/cmd/multicluster/manager_compat.go` |
| `pkg/testutil` helper mirror | source-level function comparison, `operator/internal/enterprisedrift/source_drift_test.go` |
| `ConfigSyncMode` enum | `operator/internal/controller/redpanda/enterprise_adapters_test.go` |

## Test strategy

Unit and golden tests live in this module and must run standalone
(`cd enterprise && GOWORK=off go test ./...`). The envtest/integration suites
for the stretch controllers stay OSS-hosted
(`operator/internal/controller/redpanda`, `operator/pkg/client`, the vcluster
CLI test in `operator/cmd/rpk-k8s/k8s`) because they depend on the OSS test
infra; they exercise this module through its exported API and move to the
enterprise repo at lift time (which already carries copies of that infra).

## Lift runbook

When this module moves to the operator-enterprise repo:

1. Copy the `enterprise/` directory into the target repo root and rewrite the
   module path: `github.com/redpanda-data/redpanda-operator/enterprise` →
   `github.com/redpanda-data/operator-enterprise` (never sed `*.pb.go` —
   update `buf.gen.yaml`/the proto's `go_package` and run `buf generate`).
2. In this repo: remove `./enterprise` from go.work, delete the directory,
   and replace the filesystem `replace` directives in operator/, acceptance/,
   and gen/ go.mods with a tagged require of the new module.
3. Move the OSS-hosted integration suites (see Test strategy) into the
   enterprise repo and re-home them on its test infra.
4. Delete the cross-repo drift guards that read files across the boundary:
   `operator/lifecycle/forkledger_test.go` skips itself automatically; the
   `operator/internal/enterprisedrift` tests and
   `enterprise_shared_drift_test.go` need equivalents that pin against the
   released module version instead (or move into a periodic cross-repo CI
   job).
5. Port `taskfiles/enterprise.yml` targets into the enterprise repo's
   Taskfile; remove the `enterprise:` include, the `lint:enterprise-boundary`
   task, and the `enterprise/**` depguard rule here.
6. RBAC: the enterprise controllers' kubebuilder markers feed this repo's
   chart RBAC via the `paths=../enterprise/operator/controller` entries in
   `taskfiles/k8s.yml` — after the lift, either vendor the generated rules or
   generate them in the enterprise repo and sync the YAML here.
