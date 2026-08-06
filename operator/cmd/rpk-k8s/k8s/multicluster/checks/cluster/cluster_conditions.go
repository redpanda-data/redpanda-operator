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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionsCheck inspects the StretchCluster status conditions and reports
// any that are not True. Requires ResourceCheck to have run.
type ConditionsCheck struct{}

func (c *ConditionsCheck) Name() string { return "conditions" }

func (c *ConditionsCheck) Run(_ context.Context, cc *CheckContext) []Result {
	if cc.StretchCluster == nil {
		return []Result{Skip(c.Name(), "StretchCluster not available")}
	}

	if len(cc.Conditions) == 0 {
		return []Result{Fail(c.Name(), "no status conditions found — cluster may not have been reconciled yet")}
	}

	var results []Result
	for _, cond := range cc.Conditions {
		if cond.Status == metav1.ConditionTrue {
			results = append(results, Pass(c.Name(), fmt.Sprintf("%s: %s", cond.Type, cond.Reason)))
		} else {
			msg := fmt.Sprintf("%s: %s", cond.Type, cond.Reason)
			if cond.Message != "" {
				msg += " — " + cond.Message
			}
			results = append(results, Fail(c.Name(), msg))
		}
	}
	return results
}
