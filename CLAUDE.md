# Claude Code Guide for redpanda-operator

## Repository Structure

This is a Go monorepo using `go.work` with multiple modules:

- `operator/` — The Redpanda Kubernetes operator (v1 and v2 controllers)
- `charts/redpanda/` — Helm chart for Redpanda (Go source → gotohelm → templates)
- `charts/console/` — Helm chart for Redpanda Console
- `charts/connectors/` — Helm chart for Redpanda Connectors
- `gotohelm/` — Custom Go-to-Helm template transpiler
- `pkg/` — Shared packages (k3d, multicluster, testutil, etc.)
- `acceptance/` — Acceptance test suite (harpoon framework)
- `gen/` — Code generation tools (partial, schema, pipeline)
- `harpoon/` — BDD test framework for acceptance tests

## Code Style & File Organization

**Order declarations most-relevant-first.** Entry points and exported API first, then the helpers they call, in call order. A helper above its caller makes the reader hold an unexplained function in their head. Same rule inside tests (`TestXxx` before its helpers).

```go
// BAD                        // GOOD
func helper() { ... }         func Exported() { helper() }
func Exported() { helper() }  func helper() { ... }
```

A type leads its own declarations: type, constructors, then methods.

```go
type Foo struct { ... }

func NewFoo() Foo { ... }

func (f *Foo) Do() { ... }
```

Rote method sets are the exception. When several types satisfy the same interface with one-line bodies, declare the types together and group the implementations, rather than interleaving type/method pairs.

```go
type Foo struct { ... }
type Bar struct { ... }
type Baz struct { ... }

func (Foo) ImplementsQuux() {}
func (Bar) ImplementsQuux() {}
func (Baz) ImplementsQuux() {}
```

Keep pure reordering of existing files in its own commit so it doesn't hide behavioral changes.

**Tests go in `<file>_test.go`.** Tests for `foo.go` go in `foo_test.go` — **do not proliferate test files.** No `foo_edge_cases_test.go`, `foo_regression_test.go`, or per-scenario files; append to the existing one. A long test file beats five with overlapping names. If a test file gets unwieldy, split the *source* file along a real seam and let the test file follow.

**Prefer table-driven tests** over many bespoke `TestXxx` funcs covering the same function. New cases should be a row in the table, not a new function. Reach for a standalone test only when the setup genuinely doesn't fit the table's shape.

**Declare zero values with `var`.** `var x string`, not `x := ""`. `var xs []T`, not `xs := []T{}` — the nil slice is the intended value: it appends, ranges, and `len`s identically, and marshals to `null` rather than `[]`.

**Keep variable scopes tight.** Declare at first use, not at the top of the function. A binding that sits unused through 20 lines of unrelated work is 20 lines the reader has to keep it alive for.

```go
// BAD                          // GOOD
x := arg + "-value"             other := work()
other := work()                 x := arg + "-value"
return x + other                return x + other
```

**Don't reuse `err`.** Declare it with the value it belongs to; don't pre-declare a result and assign into it just to share an `err`.

```go
// BAD                          // GOOD
var x string                    x, err := failableFunc()
x, err = failableFunc()
```

**Write comments for experts.** Assume the reader knows Go and this codebase. Comment the *why*: invariants, constraints, and why the obvious approach doesn't work. Everything else is superfluous — comments cost reading time on every future read and rot silently, so an unearned line is worse than no line.

Do not write:
- Restatements of the code — `// increment the counter` above `count++`.
- Diff or conversation archaeology — `// changed to fix the flake`, `// as discussed`, `// we used to call Foo here`. Version control holds this; the reader doesn't need it.
- Godoc that just re-spells the identifier — `// Broker is a broker.`
- Commented-out code. Delete it.

```go
// BAD - narrates the change; tells a future editor nothing.
// Bumped to 30s from 10s to fix CI flakes.
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

// GOOD - names the constraint that chose the number, so it can be rechecked.
// Brokers can take ~20s to report ready after a config change.
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
```

The one piece of history worth keeping is a link: for an upstream bug or API quirk, cite the issue rather than describing the symptom.

## Reconciliation: Idempotency & Quiescence

**Read this before changing controller watch triggers or requeue/rate-limit intervals.**

A healthy controller **quiesces**: once a resource matches its desired state, `Reconcile` returns `(Result{}, nil)` (or the controller's periodic requeue) and stops writing. If a resource reconciles forever — or on a tight interval on an otherwise-stable cluster — that is a **non-determinism / idempotency bug**: something is written on every pass. It is **not**, first and foremost, a watch-trigger or requeue-interval problem.

**Root-cause it; do not bandage it.** Before changing any watch trigger or interval, find the exact write that happens every loop. Common culprits:
- A status field recomputed each pass with `time.Now()`, unstable map/slice ordering, or re-applied defaulting.
- A **rate-limited status condition** heartbeating: `setStatusCondition` (`operator/internal/statuses/zz_generated_status.go`) bumps `LastTransitionTime` once `time.Since(LastTransitionTime) > rateLimit`, which dirties the status and retriggers reconciliation. The `rateLimit` values live in `operator/statuses.yaml` (e.g. `LicenseValid`, `ConfigurationApplied`). Too tight a rate == perpetual churn at roughly that interval; windows offset across conditions cluster into bursts.
- **Cross-resource churn**: e.g. the Redpanda CR status being rewritten every loop retriggers watching controllers (Topic, owned resources). When a *downstream* resource won't quiesce, suspect the *upstream* status writer — fix it there, not in the downstream watch.

**Do NOT** "fix" infinite reconciliation by tuning `Watches(...)`, event predicates (`GenerationChangedPredicate`, etc.), `EnqueueRequestsFromMapFunc`, `RequeueAfter`, or `rateLimit` to reduce trigger frequency *as the primary fix*. That masks the underlying bug and the churn reappears later (or on another resource). Adjusting triggers is legitimate only as an additional refinement *after* the per-loop write has been found and eliminated.

**Verify quiescence with a unit test, not an integration harness.** The cheap proof is that the pure "what mutations does convergence require?" function returns nothing on converged input — e.g. `generateConf` in `topic_controller.go` returns an empty set map (see `TestGenerateConf`), or `UpdateConditions` returns `changed=false`. The churn is often NOT a Kubernetes write — it can be an outbound admin/Kafka RPC (e.g. `IncrementalAlterConfigs`) or a time-gated status heartbeat — so assert on the decision function's output, not on whether the CR object changed. For rate-limited conditions, age the condition's `LastTransitionTime` (or advance a clock) and assert it re-dirties no faster than its configured `rateLimit`. References:
- `operator/internal/observability/wrapper.go` — the steady-state signal (`(Result{}, nil)`, or a requeue matching the controller's periodic interval, == quiesced); the same signal powers the `OperatorReconcileRunaway` Prometheus alert.
- `operator/internal/statuses/rate_limit_test.go` — the cadence-test pattern (it constructs conditions with an aged `LastTransitionTime`). Its blind spot: `TestUpdateConditions_IdempotentOnHealthyCluster` calls `UpdateConditions` twice in quick succession, so the rate window never elapses and it can't see rate-limited churn.

**When reviewing** a PR that changes watch triggers, predicates, `RequeueAfter`, or `rateLimit`: require an explicit statement of the per-loop write that was identified and fixed. A trigger/interval change with no named root cause is a red flag — request the root-cause analysis before approving.

## Build System

- **Task runner**: [go-task](https://taskfile.dev/) via `Taskfile.yml` with includes from `taskfiles/`
- **CI**: Buildkite (`.buildkite/pipeline.yml` → `.buildkite/testsuite.yml`)
- **Nix**: `flake.nix` provides the dev environment. CI runs all commands inside a nix container via `ci/scripts/run-in-nix-docker.sh`
- **Code generation**: Go source is transpiled to Helm templates via `gotohelm`, JSON schemas are produced by `gen schema`, and Go partials by `gen partial`. **Do not invoke these tools directly.** Instead, use `nix develop -c task generate` which runs all generators in the correct order and matches CI. For CRD/RBAC regeneration specifically, use `nix develop -c task k8s:generate`.

## CI Lint Flow

The CI lint step (`taskfiles/ci.yml`) runs:
1. `task :generate` — regenerates ALL generated files (CRDs, RBAC, templates, schemas, partials, licenses, changelog, buildkite pipelines, then `fmt:fix`)
2. `task :lint` — runs `golangci-lint run`, `helm lint --strict`, and `actionlint`
3. `git diff --exit-code` — fails if any generated file doesn't match what's committed

**Key implication**: Any code change that affects generated output requires regenerating those files before committing. Common sources of lint failure:
- Modifying Go chart source without regenerating `.tpl` templates via `task generate`
- Adding dependencies without updating `licenses/third_party.md`
- Changing kubebuilder RBAC markers without running `controller-gen`
- Import ordering violations caught by `gci` formatter

`generate` ends with `fmt:fix` (`golangci-lint fmt`), which runs the formatters
only. `lint-fix` is an alias of it, so it no longer auto-fixes analysis-linter
findings in hand-written code — step 2 reports those instead. For the old
behavior, `task lint -- --fix`.

### Generation task caching

Generators in `taskfiles/k8s.yml` and `taskfiles/charts.yml` declare
`sources:`/`generates:`, so `task generate` skips whatever is current: ~4s for
a no-op and ~12s after editing one controller, against ~50s from scratch.
**Give any new generator a fingerprint**, and `exclude:` its own output from
`sources:` or it dirties itself every run.

This only helps locally. CI starts with an empty `.task/` and ends with
`git diff --exit-code`, so it regenerates everything and catches a glob that
missed a real input. `rm -rf .task` forces a full local rebuild.

## Golden Test Files

Regenerate goldens with `-update-golden`: `go test ./path/to/... -update-golden`. The legacy `-update` flag in `pkg/testutil` is a no-op — chart template tests (`TestTemplate`) go through `common-go/goldenfile`, which registers only `-update-golden`, so `-update` silently regenerates nothing.

Goldens under `operator/internal/lifecycle/` bake in image values from `TEST_REDPANDA_REPO` (e.g. `redpandadata/redpanda-unstable`) and `TEST_REDPANDA_VERSION` (e.g. `v26.1.1-rc1`); both must be set to match CI output.

## Kubernetes Version Testing

How each suite picks its K8s version:
- **k3d** (integration, acceptance) — `K3S_IMAGE` env var, default in `pkg/k3d/k3d.go`
- **Kind** (kuttl) — `kindest/node` images in `operator/kind*.yaml`, capped by kuttl's embedded Kind library
- **envtest** (unit) — `KUBEBUILDER_ASSETS` from `setup-envtest`, set in `flake.nix`

### How to Bump Kubernetes Versions

These are pinned independently; a missed one surfaces as an unrelated-looking test failure. Update **all** of them:

| What | Where |
|---|---|
| k3d default image | `pkg/k3d/k3d.go` `DefaultK3sImage`. Docker Hub tags use `-`, not `+`: `rancher/k3s:v1.32.13-k3s1` |
| k3d nightly default | `flake.nix` devshell `K3S_IMAGE` — the max supported version. The Buildkite nightly schedule overrides it per-run |
| Kind node images | `operator/kind.yaml`, `kind-for-v2.yaml`, `kind-for-cloud.yaml`. **Must include the `@sha256:` digest** from the matching [Kind release](https://github.com/kubernetes-sigs/kind/releases) |
| kuttl | `ci/kuttl.nix` — version plus sha256 for both `aarch64-darwin` and `x86_64-linux`. Kuttl embeds a Kind library that caps the usable `kindest/node` version (v0.19.0 → Kind v0.24.0, max K8s 1.31.x; v0.25.0 → Kind v0.31.0, max K8s 1.35.x). Also bump the kuttl entry in `pkg/lint/testdata/tool-versions.txtar` |
| envtest | `flake.nix`: `{ name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.XX.x)"; }` |
| Kube component images | `Taskfile.yml` `DEFAULT_TEST_KUBE_VERSION`, plus hardcoded `registry.k8s.io/kube-{apiserver,controller-manager}` refs in the three test files below |
| vcluster | `pkg/testutil/testutil.go` `VClusterVersion`, `Taskfile.yml` `DEFAULT_TEST_VCLUSTER_VERSION`, and `ghcr.io/loft-sh/vcluster-pro` refs in the three test files below |
| vcluster distro image | `ghcr.io/loft-sh/kubernetes` tag in **three** places that must match: `pkg/vcluster/vcluster.go` `DefaultValues`, `Taskfile.yml` pre-pull list, `acceptance/main_test.go` `WithImportedImages`. Must be supported by the vcluster chart version above |
| cert-manager (inside vclusters, for webhook TLS) | `pkg/testutil/testutil.go` `CertManagerVersion`, `Taskfile.yml` `DEFAULT_SECOND_TEST_CERTMANAGER_VERSION`, `quay.io/jetstack/cert-manager-*` refs in the three test files below |
| Acceptance upgrade baselines | `--version vXX.Y.Z` in `acceptance/features/operator-upgrades.feature`, `console-upgrades.feature`, and `upgrade-regressions.feature` (whose intermediate step uses the local `../operator/chart`), plus `DefaultRedpandaRepo`/`DefaultRedpandaTag` in `acceptance/steps/defaults.go` |

The three test files carrying hardcoded image refs: `operator/internal/controller/redpanda/redpanda_controller_test.go`, `operator/internal/probes/broker_test.go`, `operator/pkg/client/factory_test.go`.

## Proto Conflict

The operator module has a protobuf namespace conflict between `buf.build/gen/go/grpc-ecosystem/grpc-gateway` and `github.com/grpc-ecosystem/grpc-gateway/v2` that panics at test runtime. `flake.nix` suppresses it with `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore`, so run tests through the devshell.

## Cutting a Release

[CONTRIBUTING.md](./CONTRIBUTING.md#cutting-a-release) is authoritative for the mechanics — `changie batch`/`merge`, tagging, pushing, the release workflow, helm-charts sync, and `NEXT_VERSION`. Work on a branch off the target release branch (e.g. `release/v25.1.x`). Changie project keys, which double as tag prefixes (`<key>/vX.Y.Z`): `operator`, `charts/redpanda`, `charts/console`, `charts/connectors`, `gotohelm`.

Changie's replacements handle `operator/chart/Chart.yaml` (`version`, `appVersion`, image tag). What they do **not** handle, and you must bump by hand:
- `charts/redpanda/Chart.yaml` `version` — the `charts/redpanda` project has no changie replacements at all.
- `charts/redpanda/values.yaml` `sideCars.image.tag` — must match the operator version being released.
- Version badges in `operator/chart/README.md` and `charts/redpanda/README.md`.
- Chart goldens. The `operator` project's `helm.sh/chart` replacement regex expects a `v` prefix the real value lacks, so that label never updates on its own:
  ```bash
  go test ./operator/chart ./charts/redpanda/... -run TestTemplate -update-golden
  ```

`task test:unit` and `task lint` report anything still missed. Commit once per project as `<project>: cut release <version>`.

## Common Commands

Run everything inside the nix devshell — `nix develop`, or prefix with `nix develop -c`. It pins tool versions and sets `KUBEBUILDER_ASSETS` and `GOLANG_PROTOBUF_REGISTRATION_CONFLICT`.

```bash
task test:unit                     # unit tests (envtest configured by the devshell)
task lint                          # golangci-lint, helm lint --strict, actionlint
task generate                      # regenerate ALL generated files (preferred - matches CI)
task k8s:generate                  # CRDs and RBAC only
go test ./path/... -update-golden  # refresh golden files
helm dep build charts/redpanda/chart && go test ./charts/redpanda/... -run TestTemplate
```
