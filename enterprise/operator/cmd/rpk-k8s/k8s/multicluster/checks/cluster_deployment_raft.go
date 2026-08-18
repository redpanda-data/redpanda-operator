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
	"context"
	"fmt"
	"strings"
)

// DeploymentRaftCheck validates that the Deployment's --name and --peer flags
// are consistent with the raft status reported by the running operator.
// Requires cc.DeployArgs and cc.RaftStatus to be set.
type DeploymentRaftCheck struct{}

func (c *DeploymentRaftCheck) Name() string { return "deployment-raft" }

func (c *DeploymentRaftCheck) Run(_ context.Context, cc *CheckContext) []Result {
	if cc.DeployArgs == nil || cc.RaftStatus == nil {
		return nil
	}

	var results []Result

	// --name vs raft name.
	deployName := ExtractFlag(cc.DeployArgs, "--name")
	if deployName != "" && cc.RaftStatus.Name != "" && deployName != cc.RaftStatus.Name {
		results = append(results, Fail(c.Name(),
			fmt.Sprintf("Deployment --name=%s does not match raft status name %q", deployName, cc.RaftStatus.Name)))
	} else if deployName != "" && cc.RaftStatus.Name != "" {
		results = append(results, Pass(c.Name(),
			fmt.Sprintf("Deployment --name=%s matches raft status", deployName)))
	}

	// --peer list vs raft cluster names.
	deployPeers := ExtractFlagAll(cc.DeployArgs, "--peer")
	if len(cc.RaftStatus.ClusterNames) > 0 && len(deployPeers) > 0 {
		raftNames := make(map[string]bool, len(cc.RaftStatus.ClusterNames))
		for _, n := range cc.RaftStatus.ClusterNames {
			raftNames[n] = true
		}
		for _, p := range deployPeers {
			// Peer format: name://address:port
			peerName := strings.SplitN(p, "://", 2)[0]
			if !raftNames[peerName] {
				results = append(results, Fail(c.Name(),
					fmt.Sprintf("Deployment --peer=%s references unknown cluster %q", p, peerName)))
			}
		}
		if len(results) == 0 || allPassing(results) {
			results = append(results, Pass(c.Name(), "Deployment --peer list matches raft cluster names"))
		}
	}

	return results
}

func allPassing(results []Result) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}
