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
	"fmt"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

// StatusAgreementCheck verifies that the StretchCluster .status.conditions
// are identical across all clusters. The reconciler writes status to every
// reachable cluster, so disagreement indicates a cluster was unreachable
// when the last status write occurred.
type StatusAgreementCheck struct{}

func (c *StatusAgreementCheck) Name() string { return "status-agreement" }

func (c *StatusAgreementCheck) Run(contexts []*CheckContext) []Result {
	var available []*CheckContext
	for _, cc := range contexts {
		if cc.StretchCluster != nil {
			available = append(available, cc)
		}
	}

	if len(available) < 2 {
		return []Result{Skip(c.Name(), "fewer than 2 clusters have the StretchCluster — cannot compare status")}
	}

	reference := available[0]
	var stale []string
	for _, cc := range available[1:] {
		if !apiequality.Semantic.DeepEqual(reference.Conditions, cc.Conditions) {
			stale = append(stale, cc.Context)
		}
	}

	if len(stale) > 0 {
		return []Result{Fail(c.Name(),
			fmt.Sprintf("status conditions differ from %s on: %s — these clusters may have been unreachable during the last status write",
				reference.Context, strings.Join(stale, ", ")))}
	}

	return []Result{Pass(c.Name(),
		fmt.Sprintf("status conditions agree across all %d clusters", len(available)))}
}
