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
	"sort"
	"strings"
)

// PeerAgreementCheck verifies that all operators see the same set of cluster names.
type PeerAgreementCheck struct{}

func (c *PeerAgreementCheck) Name() string { return "peer-agreement" }

func (c *PeerAgreementCheck) Run(contexts []*CheckContext) []Result {
	type peerSet struct {
		key     string
		context string
	}
	var sets []peerSet
	for _, cc := range contexts {
		if cc.RaftStatus == nil {
			continue
		}
		names := make([]string, len(cc.RaftStatus.ClusterNames))
		copy(names, cc.RaftStatus.ClusterNames)
		sort.Strings(names)
		sets = append(sets, peerSet{
			key:     strings.Join(names, ","),
			context: cc.Context,
		})
	}

	if len(sets) == 0 {
		return []Result{Fail(c.Name(), "no clusters reachable for peer list comparison")}
	}

	allSame := true
	for i := 1; i < len(sets); i++ {
		if sets[i].key != sets[0].key {
			allSame = false
			break
		}
	}

	if allSame {
		return []Result{Pass(c.Name(), "peer lists agree across all clusters")}
	}

	var results []Result
	for _, ps := range sets {
		results = append(results, Fail(c.Name(),
			fmt.Sprintf("peer list mismatch: %s sees [%s]", ps.context, ps.key)))
	}
	return results
}
