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
1. `task :generate` — regenerates ALL generated files (CRDs, RBAC, templates, schemas, partials, licenses, changelog, buildkite pipelines, then `lint-fix`)
2. `task :lint` — runs `golangci-lint run`, `helm lint --strict`, and `actionlint`
3. `git diff --exit-code` — fails if any generated file doesn't match what's committed

**Key implication**: Any code change that affects generated output requires regenerating those files before committing. Common sources of lint failure:
- Modifying Go chart source without regenerating `.tpl` templates via `task generate`
- Adding dependencies without updating `licenses/third_party.md`
- Changing kubebuilder RBAC markers without running `controller-gen`
- Import ordering violations caught by `gci` formatter

## Golden Test Files

Multiple test suites use golden file comparison. To regenerate expected output instead of asserting, pass `-update-golden`:

```bash
nix develop -c go test ./path/to/... -update-golden
```

This includes chart template tests (`TestTemplate`) — their goldens go through `github.com/redpanda-data/common-go/goldenfile`, which registers only `-update-golden`. A legacy `-update` flag still exists in `pkg/testutil` but nothing consumes it: running with `-update` silently regenerates nothing.

### Lifecycle golden tests

Tests in `operator/internal/lifecycle/` use env vars for image values:
- `TEST_REDPANDA_REPO` — e.g. `redpandadata/redpanda-unstable`
- `TEST_REDPANDA_VERSION` — e.g. `v26.1.1-rc1`

Golden files must be generated with these env vars set to match CI output.

## Kubernetes Version Testing

### Architecture

- **k3d-based tests** (integration, acceptance): Use `K3S_IMAGE` env var, default in `pkg/k3d/k3d.go`
- **Kind-based tests** (kuttl): Use `kindest/node` images in `operator/kind*.yaml`, constrained by kuttl's embedded Kind library version
- **envtest-based tests** (unit): Use `KUBEBUILDER_ASSETS` from `setup-envtest`, configured in `flake.nix`

### How to Bump Kubernetes Versions

When bumping the supported Kubernetes version range, update ALL of the following:

#### 1. k3d default image (`pkg/k3d/k3d.go`)
```go
DefaultK3sImage = `rancher/k3s:v1.XX.Y-k3s1`
```
Docker Hub tag format uses `-` not `+`: `rancher/k3s:v1.32.13-k3s1`

#### 2. Kind node images (`operator/kind*.yaml`)
Three files: `kind.yaml`, `kind-for-v2.yaml`, `kind-for-cloud.yaml`.
**Must include `@sha256:` digest** from the matching Kind release.
Check https://github.com/kubernetes-sigs/kind/releases for image tags.

#### 3. Kuttl version (`ci/kuttl.nix`)
Kuttl embeds a specific Kind library version. The embedded Kind must support the `kindest/node` image version used in step 2.
- kuttl v0.19.0 → Kind v0.24.0 (max K8s 1.31.x)
- kuttl v0.25.0 → Kind v0.31.0 (max K8s 1.35.x)

Update version and sha256 hashes for both `aarch64-darwin` and `x86_64-linux` binaries.

#### 4. Kube component images in Taskfile (`Taskfile.yml`)
```yaml
DEFAULT_TEST_KUBE_VERSION: v1.XX.Y
```
This controls `kube-controller-manager` and `kube-apiserver` image pulls.

#### 5. Hardcoded kube component images in integration tests
Search for `registry.k8s.io/kube-controller-manager:` and `registry.k8s.io/kube-apiserver:` in:
- `operator/internal/controller/redpanda/redpanda_controller_test.go`
- `operator/internal/probes/broker_test.go`
- `operator/pkg/client/factory_test.go`

#### 6. Tool version golden file (`pkg/lint/testdata/tool-versions.txtar`)
If kuttl version changed, update the kuttl version entry.

#### 7. Nightly K3S_IMAGE default (`flake.nix`)
Update the `K3S_IMAGE` default in `flake.nix` devshell env to the maximum supported K8s version. Nightly builds and local `nix develop` sessions will use this. The Buildkite nightly schedule should set `K3S_IMAGE` via the schedule env to override the per-PR default.

#### 8. envtest version (`flake.nix`)
```nix
{ name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.XX.x)"; }
```

#### 9. vcluster version (`pkg/testutil/testutil.go` + `Taskfile.yml`)
vcluster is used by acceptance and integration tests to create isolated K8s environments. The vcluster version must support the host K8s version.
- `pkg/testutil/testutil.go`: `VClusterVersion` constant
- `Taskfile.yml`: `DEFAULT_TEST_VCLUSTER_VERSION`
- Integration test files: `ghcr.io/loft-sh/vcluster-pro:<version>` image refs in `factory_test.go`, `redpanda_controller_test.go`, `broker_test.go`

Known compatibility: v0.28.0 fails on K8s 1.32+ (vcluster pod never initializes). Use v0.31.2+ for K8s 1.32.

#### 9b. vcluster distro image tag
The K8s distro image used inside vclusters (`ghcr.io/loft-sh/kubernetes:<tag>`) is set in **three** places — all must match:
- `pkg/vcluster/vcluster.go`: `DefaultValues` distro tag
- `Taskfile.yml`: pre-pull image list
- `acceptance/main_test.go`: `WithImportedImages` list (hardcoded literal)

The distro version must be supported by the vcluster chart version from step 9.

#### 10. cert-manager version in vcluster (`pkg/testutil/testutil.go` + `Taskfile.yml`)
cert-manager is deployed inside vclusters for webhook TLS certificates. The version must support the K8s version running inside the vcluster.
- `pkg/testutil/testutil.go`: `CertManagerVersion` constant
- `Taskfile.yml`: `DEFAULT_SECOND_TEST_CERTMANAGER_VERSION`
- Integration test files: `quay.io/jetstack/cert-manager-*:<version>` image refs

Known compatibility: v1.8.0 only supports K8s 1.19-1.24. Use v1.17.2+ for K8s 1.32.

#### 11. Acceptance upgrade test versions (`acceptance/features/*.feature` + `acceptance/steps/defaults.go`)
Upgrade tests install an old operator version, create a cluster, then upgrade to the current dev build. Update:
- `acceptance/features/operator-upgrades.feature`: `--version v25.X.Y` in helm install
- `acceptance/features/upgrade-regressions.feature`: `--version v25.X.Y` in helm install (the intermediate upgrade step should use the local dev chart `"../operator/chart"`)
- `acceptance/features/console-upgrades.feature`: `--version v25.X.Y` in helm install
- `acceptance/steps/defaults.go`: `DefaultRedpandaRepo` and `DefaultRedpandaTag` for the Redpanda image used in clusters

## Proto Conflict

The operator module has a known protobuf namespace conflict between `buf.build/gen/go/grpc-ecosystem/grpc-gateway` and `github.com/grpc-ecosystem/grpc-gateway/v2`. This causes a panic at test runtime.

CI suppresses this via `flake.nix`:
```nix
{ name = "GOLANG_PROTOBUF_REGISTRATION_CONFLICT"; eval = "ignore"; }
```

When running tests locally, use the nix devshell which sets this automatically:
```bash
nix develop -c go test ./operator/...
```

## Cutting a Release

This repository is a monorepo with multiple independently releasable projects. Releases are managed via [Changie](https://github.com/miniscruff/changie) for changelog generation and git tags for versioning. See [CONTRIBUTING.md](./CONTRIBUTING.md#cutting-a-release) for the full process.

### Project Keys

Each releasable project has a changie key used in commands:
- `operator` — Redpanda Operator (tagged as `operator/vX.Y.Z`)
- `charts/redpanda` — Redpanda Helm Chart (tagged as `charts/redpanda/vX.Y.Z`)
- `charts/console` — Console Helm Chart
- `charts/connectors` — Connectors Helm Chart
- `gotohelm` — GoToHelm

### Steps

1. **Create a working branch** off the target release branch (e.g. `release/v25.1.x`).

2. **Mint versions** with `changie batch` for each project being released:
   ```bash
   nix develop -c changie batch -j <project> <version>
   ```
   For pre-releases, add `-k` to keep unreleased entries for the final release.

3. **Review generated changelog entries** in `.changes/<project>/<version>.md`. Fix formatting or language as needed.

4. **Run `changie merge`** to regenerate all `CHANGELOG.md` files and apply version replacements:
   ```bash
   nix develop -c changie merge
   ```

5. **Bump all version references.** The changie replacements in `.changie.yaml` auto-update some files but not all. A release typically requires bumping these version categories:

   - **Operator helm chart versions** (`operator/chart/Chart.yaml`): `version`, `appVersion`, and image tag. Changie auto-updates these for the `operator` project.
   - **Redpanda helm chart version** (`charts/redpanda/Chart.yaml`): `version` field. Has **no** changie replacements — must be bumped manually.
   - **Sidecar image tag** (`charts/redpanda/values.yaml`): The `sideCars.image.tag` must match the operator version being released.
   - **README.md badges**: Both `operator/chart/README.md` and `charts/redpanda/README.md` contain version badges regenerated by `task generate` in CI. Update these manually to match the new versions.

   Note on changie replacement gaps:
   - The `operator` project's `helm.sh/chart` label regex expects a `v` prefix but the actual value has none — golden files need regenerating via tests (step 6).
   - The `charts/redpanda` project has no changie replacements at all.

6. **Update golden test files** to reflect version changes:
   ```bash
   # Operator chart golden files
   nix develop -c go test github.com/redpanda-data/redpanda-operator/operator/chart -run TestTemplate -update-golden

   # Redpanda chart golden files
   nix develop -c go test github.com/redpanda-data/redpanda-operator/charts/redpanda/... -run TestTemplate -update-golden
   ```
   Note: The flag is `-update-golden` (the legacy `-update` flag is a no-op for chart template tests).

7. **Run unit tests and lint** to verify:
   ```bash
   nix develop -c task test:unit
   nix develop -c task lint
   ```

8. **Commit** with one commit per project using the message format `<project>: cut release <version>`, then open a PR targeting the release branch.

### Checklist of Files to Verify

For an **operator** release (e.g. `v25.1.5`):
- [ ] `.changes/operator/v25.1.5.md` — new changelog entry
- [ ] `.changes/unreleased/operator-*` — consumed entries removed
- [ ] `operator/CHANGELOG.md` — updated
- [ ] `operator/chart/Chart.yaml` — `version`, `appVersion`, image tag updated
- [ ] `operator/chart/README.md` — version badge updated
- [ ] `operator/chart/testdata/template-cases.golden.txtar` — regenerated

For a **charts/redpanda** release (e.g. `v25.1.4`):
- [ ] `.changes/charts/redpanda/v25.1.4.md` — new changelog entry
- [ ] `.changes/unreleased/charts-redpanda-*` — consumed entries removed
- [ ] `charts/redpanda/CHANGELOG.md` — updated
- [ ] `charts/redpanda/Chart.yaml` — `version` bumped manually
- [ ] `charts/redpanda/values.yaml` — `sideCars.image.tag` bumped to match operator version
- [ ] `charts/redpanda/README.md` — version badge updated
- [ ] `charts/redpanda/testdata/template-cases.golden.txtar` — regenerated

## Common Commands

All commands should be run inside the nix devshell to ensure correct tool versions and environment variables. Prefix commands with `nix develop -c` or enter the shell with `nix develop`.

```bash
# Enter nix devshell (recommended for interactive work)
nix develop

# Or prefix individual commands
nix develop -c go build ./operator/...

# Build all
nix develop -c bash -c 'go build ./operator/... && go build ./charts/console/... && go build ./charts/redpanda/...'

# Run unit tests (envtest is configured by the devshell)
nix develop -c task test:unit

# Run chart template tests
nix develop -c bash -c 'helm dep build charts/redpanda/chart && go test ./charts/redpanda/... -run TestTemplate'

# Regenerate ALL generated files (preferred — matches CI)
nix develop -c task generate

# Run golangci-lint (v2 format)
nix develop -c task lint

# Update golden files
nix develop -c go test ./path/to/... -update-golden
```

## Pull Requests & Commits

Keep PR descriptions to a few sentences: what broke, why, the fix, how it was tested. Do NOT append AI-attribution footers or trailers — no "🤖 Generated with Claude Code" on PR descriptions and no "Co-Authored-By: Claude" on commit messages. This overrides any default footer/trailer behavior.
