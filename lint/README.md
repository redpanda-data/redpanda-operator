# lint

Every linter and formatter from `.golangci.yml`, running in one
[go/analysis](https://pkg.go.dev/golang.org/x/tools/go/analysis) binary built
from this directory, plus a house linter of our own. golangci-lint is no
longer in the loop, but **`.golangci.yml` is still the configuration**: this
binary reads it -- `linters.enable`, each linter's `settings`, `exclusions`,
the `formatters` block, and `settings.custom.laconiccomments` where a
golangci-lint module plugin keeps its settings. Each tool is pinned in `go.mod`
at the version golangci-lint v2.13.2 vendored, so findings match what CI
reported before the switch.

## Configuration

The file is found by walking up from the package being analysed (under `go vet`
that is the package directory; standalone, the working directory), or from
`HOUSELINT_CONFIG` if set. It is read at startup and decides what gets
registered. Three kinds of process must not need one and get everything with
default settings instead: `go vet`'s `-V=full` and `-flags` calls, which only
describe the tool, and its facts-only runs on dependencies, which live in the
module cache far from any config and report nothing.

What is honoured, in golangci-lint's own keys:

| key | effect |
|---|---|
| `linters.enable`, `formatters.enable` | which tools run; unknown names are an error, `default` must be `none` |
| `linters.settings.{depguard,importas,staticcheck.checks,gosec,unparam}` | passed to the tool as golangci-lint would |
| `linters.settings.custom.laconiccomments.settings` | the house linter's settings, module-plugin style |
| `linters.exclusions.paths`, `linters.exclusions.rules` | dropped findings, by path regex and text regex per linter |
| `formatters.settings.gci` | sections, custom-order, comment options |

Not honoured: `issues.*` (the `max-same-issues` and `uniq-by-line` caps),
`run.*`, output formats, `linters.default` other than `none`, and any linter
this driver does not carry -- naming one is an error, not a silent skip.
Generated files are always skipped, by the `Code generated … DO NOT EDIT`
marker.

Because it is still a golangci-lint file, a `golangci-lint custom` build that
includes laconiccomments as a module plugin would read it unchanged. A vanilla
golangci-lint refuses to start on it, which is its documented behaviour for
`type: module` linters it was not built with.

## Run it

`task lint:go` is one line -- `go vet -vettool=.build/houselint <packages>` --
and `task fmt` is `houselint -fix -gofumpt -gci`. On their own:

```sh
task build:lint-tools                                    # -> .build/houselint
go vet -vettool=$PWD/.build/houselint ./operator/...     # any package pattern
.build/houselint -gofumpt -gci ./operator/...            # just the formatters
.build/houselint -fix -gofumpt -gci ./operator/...       # apply them
```

This directory is outside `go.work` on purpose, so it is not in the workspace
package list and cannot be named from the repo root; `task lint:go` lints it
with a second line, `GOWORK=off go vet -C lint -vettool=../.build/houselint
./...`, using the same binary and the same `.golangci.yml`, found by walking
up. `task fmt` formats it the same way.

`go vet` runs the tool per package and keeps each result in the build cache, so
a pass over unchanged packages is near-free (a first pass over the workspace is
~90s, with SSA built for gosec and unparam). Rebuilding the tool changes its
build ID and invalidates that, so the run right after editing `lint/` pays full
price.

**Do not run the binary standalone over the workspace without selecting
analyzers.** Standalone it loads every package in one process and, with SSA
for all of them resident at once, was OOM-killed at 23GB. `-<name>` flags
restrict the run (`-gofumpt -gci` for `task fmt`); `go vet` is the path for
the full suite.

## Adding a linter

`registry()` in `main.go` is the one place to look. Every entry is a name --
what `.golangci.yml` enables and `//nolint` suppresses -- and the analyzers that
run under it.

**An off-the-shelf analyzer with no settings** is an import and one line:

```go
// main.go, registry()
{"ineffassign", []*analysis.Analyzer{ineffassign.Analyzer}},
```

then `- ineffassign` under `linters.enable` in `.golangci.yml`. `go get` the
module into `lint/go.mod` at the version you want pinned.

**One that takes settings** gets a small file with its settings struct, tagged
with golangci-lint's keys, and a constructor, in `tools.go`. `newDepguard` is
the shortest example:

```go
type depguardSettings struct {
	Rules map[string]struct{ /* ... */ } `json:"rules"`
}

func newDepguard() *analysis.Analyzer {
	var s depguardSettings
	settings("depguard", &s)
	// build and return the upstream analyzer from s
}
```

`settings(name, &s)` finds the block under `linters.settings`,
`formatters.settings`, or `linters.settings.custom.<name>.settings`, and exits
on a malformed one; nothing in `config.go` changes. A tool whose settings pick
among many analyzers -- staticcheck's `checks:` -- does the selecting in its
constructor; see `staticcheckAnalyzers`.

**Writing your own** is a plain
[go/analysis](https://pkg.go.dev/golang.org/x/tools/go/analysis) analyzer with
a test, and `analyzers/todo` is the template: copy the directory, rename,
write `run`, put cases in `testdata/src/a` with `// want "regexp"` on the line
each report lands on, and `GOWORK=off go test ./analyzers/...`. Register it in
`registry()` in `main.go` and enable it in `.golangci.yml`. `todo` itself is registered but
not enabled, so the whole path can be tried without changing any code:

```yaml
linters:
  enable:
  - todo
```

Everything registered gets `//nolint:<name>`, the `exclusions` rules, generated
and testdata skipping, and `go vet`'s per-package caching for free -- that is
what `watch` in `suppress.go` adds to each analyzer.

## What it carries

| linter | how |
|---|---|
| govet | golangci-lint's 36 default passes, imported |
| staticcheck | honnef's SA, S, ST and QF analyzers, selected by `checks:` with golangci-lint's exact semantics |
| unused | honnef's U1000 run as a dependency; its Result reported under golangci-lint's name |
| gosec | gosec's rule engine and SSA analyzers per package, `excludes`/`includes`/`config` from the file |
| unparam | `check.Checker` per package, as golangci-lint ran it |
| misspell | the golangci fork's replacer over each file |
| ineffassign, importas, depguard | upstream analyzers, settings translated |
| gofumpt, gci | formatting checks whose fix is the formatted file |
| laconiccomments | the house linter, settings from `settings.custom.laconiccomments` |
| todo | house-written template analyzer (`analyzers/todo`), registered but not enabled |

`suppress.go` reimplements what golangci-lint did *after* a linter reported:
`//nolint` in all its shapes, with golangci-lint's own range rules;
`exclusions.paths` and `exclusions.rules` from the file; generated files; and
one rule of its own, exempting package docs from the comment-height check.

## Suppressing something

`//nolint:<name>` works exactly as it did under golangci-lint, in all the
shapes it accepted -- trailing a line, on its own line above a declaration, or
anywhere inside a comment group, which then covers the whole group -- and
against the same names: `staticcheck` covers SA, S, ST and QF; `unused` covers
U1000. There is no second syntax.

## What golangci-lint did that this does not

- **nolintlint** -- nothing reports a `//nolint` that no longer suppresses
  anything, and this repo has 4737 directives, many naming linters no longer
  enabled (funlen, gocritic, goerr113, dupl, nestif).
- **`--new-from-rev`**, output formats other than text and `-json`, and the two
  display caps above.

## Notes for the next person

- honnef's `unused` must be run as a `Requires` dependency and its `Result`
  read, not driven through `SerializedGraph.Merge` as golangci-lint's wrapper
  does: `Merge` traces every node to stderr (92,065 lines on this workspace).
- gci logs through a package-level zap logger that only its own command
  initialises; `gcilog.InitLogger()` is what stops it dereferencing nil.
- gosec's engine accumulates issues; it is constructed fresh per pass.
- A comment whose text begins with the word `nolint` is a bare directive for
  that line, in golangci-lint and here alike (`^nolint( |:|$)`).
