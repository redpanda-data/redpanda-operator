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

Multiple test suites use golden file comparison. There are TWO different update flags:
- `-update` — used by `pkg/testutil.NewTxTar` (the local testutil library)
- `-update-golden` — used by `github.com/redpanda-data/common-go/goldenfile.TxTar`

Check which library the test uses before choosing the flag. Some tests need both flags.

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

#### 7. Nightly pipeline (`.buildkite/pipeline.yml`)
Set `K3S_IMAGE` env var on the nightly entry point to test the maximum K8s version.

#### 8. envtest version (`flake.nix`)
```nix
{ name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.XX.x)"; }
```

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

## Common Commands

```bash
# Build all
go build ./operator/... && go build ./charts/console/... && go build ./charts/redpanda/...

# Run unit tests (needs envtest)
KUBEBUILDER_ASSETS="$(setup-envtest use -p path)" go test ./operator/...

# Run chart template tests
helm dep build charts/redpanda/chart && go test ./charts/redpanda/... -run TestTemplate

# Regenerate gotohelm templates (from chart dir)
gotohelm --write ./templates . --bundle <bundle-packages>

# Regenerate CRDs and RBAC (from operator dir)
controller-gen object:headerFile="../licenses/boilerplate.go.txt" paths='./...' crd webhook rbac:roleName=manager-role output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac/bases/operator

# Run golangci-lint (v2 format)
golangci-lint run --timeout 10m $(go work edit -json | jq -r '.Use.[].DiskPath + "/... "' | tr -d '\n')
golangci-lint fmt <packages>

# Update golden files
go test ./path/to/... -update        # for testutil-based goldens
go test ./path/to/... -update-golden # for common-go goldenfile-based goldens
```
