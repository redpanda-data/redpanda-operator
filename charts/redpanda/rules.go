//go:build ruleguard

// package rules is a module local ruleguard rule set. Repository wide rules
// should be added to pkg/lint/rules.
//
// Having trouble with rule evaluation? Try running golangci-lint cache clean.
//
// Due to limitations of ruleguard, golangci-lint, and go modules, this file,
// even if empty, must exist in all modules to ensure that the ruleguard module
// is present in the module's go.mod.
//
// See https://github.com/quasilyte/go-ruleguard/ for resources on defining rules.
package rules

import "github.com/quasilyte/go-ruleguard/dsl"

func gotohelm(m dsl.Matcher) {
	m.Match(`for $k, $v := range $m`).
		Where(
			m["m"].Type.Underlying().Is(`map[$k]$v`) &&
				m.File().Imports("github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"),
		).
		Report("range over maps are non-deterministic").
		Suggest(`for $k, $v := range helmette.SortedMap($m)`)
}
