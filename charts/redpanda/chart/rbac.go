// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_rbac.go.tpl
package chart

import (
	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// RoleSet resolves this chart's values into the shared RBAC set, which never
// sees chart values. It is the chart's one translation function for everything
// RBAC shaped.
func RoleSet(state *RenderState) redpanda.RoleSet {
	annotations := helmette.Merge(
		map[string]string{},
		state.Values.ServiceAccount.Annotations, // For backwards compatibility
		state.Values.RBAC.Annotations,
		FullAnnotations(state),
	)

	return redpanda.RoleSet{
		Prefix:         Fullname(state),
		Namespace:      state.Release.Namespace,
		Labels:         FullLabels(state),
		Annotations:    annotations,
		ServiceAccount: ServiceAccountName(state),
		Roles: redpanda.Roles{
			Decommission:   state.Values.RBAC.Enabled && state.Values.Statefulset.SideCars.BrokerDecommissioner.Enabled,
			PVCUnbinder:    state.Values.RBAC.Enabled && state.Values.Statefulset.SideCars.PVCUnbinder.Enabled,
			RPKDebugBundle: state.Values.RBAC.Enabled && state.Values.RBAC.RPKDebugBundle,
			Sidecar:        state.Values.RBAC.Enabled,
		},
		ClusterRoles: redpanda.ClusterRoles{
			Decommission:  state.Values.RBAC.Enabled && state.Values.Statefulset.SideCars.BrokerDecommissioner.Enabled,
			PVCUnbinder:   state.Values.RBAC.Enabled && state.Values.Statefulset.SideCars.PVCUnbinder.Enabled,
			RackAwareness: state.Values.RBAC.Enabled && state.Values.RackAwareness.Enabled,
		},
	}
}
