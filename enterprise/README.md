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

Anything the code here needs from the chart-coupled OSS operator arrives
through injected seams (see `operator/controller/seam.go` once the controllers
land here): the OSS operator constructs the concrete implementations and
passes them in at controller setup time.

The boundary is enforced three ways:

1. `GOWORK=off go build ./...` from this directory — with no sibling requires
   in go.mod, any monorepo import fails to resolve (workspace mode would
   silently resolve them, hence GOWORK=off).
2. A depguard rule in the repo's `.golangci.yml` scoped to `enterprise/**`.
3. `lint/boundary_test.go` — parses go.mod and every import in the module.
