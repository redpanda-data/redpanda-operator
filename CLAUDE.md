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

## Build System

- **Task runner**: [go-task](https://taskfile.dev/) via `Taskfile.yml` with includes from `taskfiles/`
- **CI**: Buildkite (`.buildkite/pipeline.yml` → `.buildkite/testsuite.yml`)
- **Nix**: `flake.nix` provides the dev environment. CI runs all commands inside a nix container via `ci/scripts/run-in-nix-docker.sh`
- **Chart generation**: Go source → `gotohelm` → `.tpl` templates. Run `gotohelm --write ./templates .` from chart directory
- **Schema generation**: `gen schema <chart-name>` generates `values.schema.json`
- **Partial generation**: `gen partial` generates `*_partial.gen.go` files

## CI Lint Flow

The CI lint step (`taskfiles/ci.yml`) runs:
1. `task :generate` — regenerates ALL generated files (CRDs, RBAC, templates, schemas, partials, licenses, changelog, buildkite pipelines, then `lint-fix`)
2. `task :lint` — runs `golangci-lint run`, `helm lint --strict`, and `actionlint`
3. `git diff --exit-code` — fails if any generated file doesn't match what's committed

**Key implication**: Any code change that affects generated output requires regenerating those files before committing. Common sources of lint failure:
- Modifying Go chart source without regenerating `.tpl` templates via `gotohelm`
- Adding dependencies without updating `licenses/third_party.md`
- Changing kubebuilder RBAC markers without running `controller-gen`
- Import ordering violations caught by `gci` formatter

## Golden Test Files

Multiple test suites use golden file comparison. To regenerate expected output instead of asserting, pass `-update-golden`:

```bash
nix develop -c go test ./path/to/... -update-golden
```

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

#### 9. vcluster version (`pkg/vcluster/vcluster.go` + `Taskfile.yml`)
vcluster is used by acceptance and integration tests to create isolated K8s environments. The vcluster version must support the host K8s version.
- `pkg/vcluster/vcluster.go`: `vClusterChartVersion` constant
- `Taskfile.yml`: `DEFAULT_TEST_VCLUSTER_VERSION`
- Integration test files: `ghcr.io/loft-sh/vcluster-pro:<version>` image refs in `factory_test.go`, `redpanda_controller_test.go`, `broker_test.go`

Known compatibility: v0.28.0 fails on K8s 1.32+ (vcluster pod never initializes). Use v0.31.2+ for K8s 1.32.

#### 10. cert-manager version in vcluster (`pkg/vcluster/vcluster.go` + `Taskfile.yml`)
cert-manager is deployed inside vclusters for webhook TLS certificates. The version must support the K8s version running inside the vcluster.
- `pkg/vcluster/vcluster.go`: `certManagerChartversion` constant
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

When running tests locally, prefix commands with:
```bash
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore go test ./operator/...
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
   nix develop -c go test github.com/redpanda-data/redpanda-operator/operator/chart -run TestTemplate -update

   # Redpanda chart golden files
   nix develop -c go test github.com/redpanda-data/redpanda-operator/charts/redpanda/... -run TestTemplate -update
   ```
   Note: The flag is `-update`, not `-update-golden`, for chart template tests.

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

### Post-Merge: Tagging and Publishing

After the release PR is merged into the release branch, tags must be cut and pushed **in order**:

1. **Tag the operator release first:**
   ```bash
   git tag operator/v<version> <commit-sha>
   git push origin operator/v<version>
   ```
   Wait for the [Release Workflow](/.github/workflows/release.yml) to complete. Verify the operator Docker image is built and pushed with the correct tag to Docker Hub.

2. **Tag the Redpanda chart release:**
   ```bash
   git tag charts/redpanda/v<version> <commit-sha>
   git push origin charts/redpanda/v<version>
   ```

3. **Publish to helm-charts repo.** Manually trigger the `release_from_operator` workflow in the [redpanda-data/helm-charts](https://github.com/redpanda-data/helm-charts) repo. This workflow syncs the released chart from the operator repo to the public helm-charts repository:
   - Go to Actions → "Release from Operator" → "Run workflow"
   - Provide the chart version that was just tagged

   Workflow source: https://github.com/redpanda-data/helm-charts/blob/main/.github/workflows/release_from_operator.yaml

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
nix develop -c go test ./operator/...

# Run chart template tests
nix develop -c bash -c 'helm dep build charts/redpanda/chart && go test ./charts/redpanda/... -run TestTemplate'

# Regenerate gotohelm templates (from chart dir)
nix develop -c gotohelm --write ./templates . --bundle <bundle-packages>

# Regenerate CRDs and RBAC (from operator dir)
nix develop -c controller-gen object:headerFile="../licenses/boilerplate.go.txt" paths='./...' crd webhook rbac:roleName=manager-role output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac/bases/operator

# Run golangci-lint (v2 format)
nix develop -c bash -c 'golangci-lint run --timeout 10m $(go work edit -json | jq -r '"'"'.Use.[].DiskPath + "/... "'"'"' | tr -d '"'"'\n'"'"')'
nix develop -c golangci-lint fmt <packages>

# Update golden files (prefer -update-golden)
nix develop -c go test ./path/to/... -update-golden
```
