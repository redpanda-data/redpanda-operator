// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"fmt"
	"strings"
)

// UniqueNamesCheck verifies that every operator reports a distinct node name.
type UniqueNamesCheck struct{}

func (c *UniqueNamesCheck) Name() string { return "unique-names" }

func (c *UniqueNamesCheck) Run(contexts []*CheckContext) []Result {
	nameToContexts := map[string][]string{}
	for _, cc := range contexts {
		if cc.RaftStatus != nil && cc.RaftStatus.Name != "" {
			nameToContexts[cc.RaftStatus.Name] = append(nameToContexts[cc.RaftStatus.Name], cc.Context)
		}
	}

	if len(nameToContexts) == 0 {
		return []Result{Fail(c.Name(), "no clusters reported a node name")}
	}

	var results []Result
	hasDuplicates := false
	for name, ctxs := range nameToContexts {
		if len(ctxs) > 1 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("duplicate node name %q reported by contexts: %s", name, strings.Join(ctxs, ", "))))
			hasDuplicates = true
		}
	}
	if !hasDuplicates {
		results = append(results, Pass(c.Name(), "all node names are unique"))
	}
	return results
}
