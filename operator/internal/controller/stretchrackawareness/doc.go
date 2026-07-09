// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package stretchrackawareness is an intentionally empty package that houses
// the kubebuilder annotations for generating the ClusterRole the operator
// needs to deploy a StretchCluster with rack awareness enabled. The
// multicluster renderer creates a per-pool rack-awareness ClusterRole
// granting nodes get/list/watch (operator/multicluster/rbac.go), and
// Kubernetes RBAC escalation prevention only lets the operator grant verbs
// it already holds — the plain rack-awareness package above carries `get`
// only, which is all the non-stretch flow needs.
package stretchrackawareness

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
