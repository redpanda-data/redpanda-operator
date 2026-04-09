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

// LeaderAgreementCheck verifies that all operators agree on the current leader.
type LeaderAgreementCheck struct{}

func (c *LeaderAgreementCheck) Name() string { return "leader-agreement" }

func (c *LeaderAgreementCheck) Run(contexts []*CheckContext) []Result {
	leaderToContexts := map[string][]string{}
	var maxTerm uint64
	for _, cc := range contexts {
		if cc.RaftStatus == nil {
			continue
		}
		if cc.RaftStatus.Leader != "" {
			leaderToContexts[cc.RaftStatus.Leader] = append(leaderToContexts[cc.RaftStatus.Leader], cc.Context)
		}
		if cc.RaftStatus.Term > maxTerm {
			maxTerm = cc.RaftStatus.Term
		}
	}

	if len(leaderToContexts) == 0 {
		return []Result{Fail(c.Name(), "no cluster reports a leader")}
	}
	if len(leaderToContexts) == 1 {
		for leader := range leaderToContexts {
			return []Result{Pass(c.Name(), fmt.Sprintf("leader agreement: %s (term %d)", leader, maxTerm))}
		}
	}

	var results []Result
	for leader, ctxs := range leaderToContexts {
		results = append(results, Fail(c.Name(),
			fmt.Sprintf("leader disagreement: %s says leader is %q", strings.Join(ctxs, ", "), leader)))
	}
	return results
}
