// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package cluster

import (
	"context"
	"fmt"
)

// PoolsCheck inspects the node pool statuses and reports scaling issues,
// unready pods, and out-of-date replicas. Requires ResourceCheck to have run.
type PoolsCheck struct{}

func (c *PoolsCheck) Name() string { return "pools" }

func (c *PoolsCheck) Run(_ context.Context, cc *CheckContext) []Result {
	if cc.StretchCluster == nil {
		return []Result{Skip(c.Name(), "StretchCluster not available")}
	}

	if len(cc.NodePools) == 0 {
		return []Result{Fail(c.Name(), "no node pools reported in status")}
	}

	var results []Result
	for _, pool := range cc.NodePools {
		name := pool.Name
		if name == "" {
			name = "(unnamed)"
		}

		// Check for scaling in progress.
		if pool.Replicas != pool.DesiredReplicas {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("pool %s: scaling in progress (replicas: %d, desired: %d)", name, pool.Replicas, pool.DesiredReplicas)))
			continue
		}

		// Check for out-of-date replicas.
		if pool.OutOfDateReplicas > 0 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("pool %s: %d out-of-date replica(s) pending rolling restart", name, pool.OutOfDateReplicas)))
			continue
		}

		// Check for condemned replicas (being decommissioned).
		if pool.CondemnedReplicas > 0 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("pool %s: %d replica(s) being decommissioned", name, pool.CondemnedReplicas)))
			continue
		}

		// Check for unready replicas.
		ready := pool.UpToDateReplicas
		if ready < pool.DesiredReplicas {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("pool %s: %d/%d replicas ready", name, ready, pool.DesiredReplicas)))
			continue
		}

		results = append(results, Pass(c.Name(),
			fmt.Sprintf("pool %s: %d/%d replicas ready", name, ready, pool.DesiredReplicas)))
	}
	return results
}
