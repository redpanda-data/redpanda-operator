// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

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

import _ "github.com/quasilyte/go-ruleguard/dsl"
