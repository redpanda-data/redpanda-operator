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

	"github.com/redpanda-data/common-go/kube"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// ResourceCheck fetches the StretchCluster resource and populates cc.StretchCluster,
// cc.Conditions, and cc.NodePools. Must run first.
type ResourceCheck struct{}

func (c *ResourceCheck) Name() string { return "resource" }

func (c *ResourceCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	sc := &redpandav1alpha2.StretchCluster{}
	if err := cc.Ctl.Get(ctx, kube.ObjectKey{Namespace: cc.Namespace, Name: cc.Name}, sc); err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("StretchCluster %s/%s not found: %v", cc.Namespace, cc.Name, err))}
	}

	cc.StretchCluster = sc
	cc.Conditions = sc.Status.Conditions
	cc.NodePools = sc.Status.NodePools

	if !sc.DeletionTimestamp.IsZero() {
		return []Result{Fail(c.Name(), fmt.Sprintf("StretchCluster %s/%s has a deletion timestamp — stuck in deletion", cc.Name, cc.Namespace))}
	}

	return []Result{Pass(c.Name(), fmt.Sprintf("StretchCluster %s/%s exists (generation %d)", cc.Name, cc.Namespace, sc.Generation))}
}
