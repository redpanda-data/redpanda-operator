// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package cluster implements a pluggable check framework for validating the
// health of a StretchCluster and its Redpanda brokers across multiple
// Kubernetes clusters. This is distinct from the parent checks package which
// validates the operator infrastructure (pods, raft, TLS).
//
// Checks come in two flavors:
//   - ClusterCheck: runs once per Kubernetes cluster, given a shared
//     CheckContext that accumulates state as checks execute. Checks run in
//     registration order so later checks can depend on state from earlier ones.
//   - CrossClusterCheck: runs once after all per-cluster checks complete,
//     given all CheckContexts. Used for validations that compare state across
//     clusters (e.g., spec consistency, secret agreement).
package cluster

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// CheckContext holds shared state for a single cluster, accumulated as
// checks execute. Earlier checks populate fields that later checks read.
type CheckContext struct {
	// Inputs — set before checks run.
	Context   string // Kubernetes context name
	Namespace string // Namespace for StretchCluster resources
	Name      string // StretchCluster resource name
	Ctl       *kube.Ctl

	// Accumulated state — populated by checks for downstream consumers.
	StretchCluster *redpandav1alpha2.StretchCluster
	Conditions     []metav1.Condition
	NodePools      []redpandav1alpha2.EmbeddedNodePoolStatus
	BootstrapSecret *corev1.Secret
	CASecrets       []*corev1.Secret

	// Broker-level state — populated by BrokerHealthCheck.
	AdminConnected bool
	Health         *rpadmin.ClusterHealthOverview
	Brokers        []rpadmin.Broker
	ConfigStatus   []rpadmin.ConfigStatus
}

// Result is the outcome of a single validation.
type Result struct {
	// Name identifies the check that produced this result.
	Name string
	// Status is one of "pass", "fail", or "skip".
	Status string
	// Message describes what was checked or what went wrong.
	Message string
}

// Pass returns a passing Result.
func Pass(name, message string) Result {
	return Result{Name: name, Status: "pass", Message: message}
}

// Fail returns a failing Result.
func Fail(name, message string) Result {
	return Result{Name: name, Status: "fail", Message: message}
}

// Skip returns a skipped Result.
func Skip(name, message string) Result {
	return Result{Name: name, Status: "skip", Message: message}
}

// ClusterCheck validates one aspect of a StretchCluster on a single
// Kubernetes cluster.
type ClusterCheck interface {
	Name() string
	Run(ctx context.Context, cc *CheckContext) []Result
}

// CrossClusterCheck validates consistency across all clusters.
type CrossClusterCheck interface {
	Name() string
	Run(contexts []*CheckContext) []Result
}

// RunClusterChecks executes all per-cluster checks in order.
func RunClusterChecks(ctx context.Context, cc *CheckContext, checks []ClusterCheck) []Result {
	var results []Result
	for _, check := range checks {
		results = append(results, check.Run(ctx, cc)...)
	}
	return results
}

// RunCrossClusterChecks executes all cross-cluster checks.
func RunCrossClusterChecks(contexts []*CheckContext, checks []CrossClusterCheck) []Result {
	var results []Result
	for _, check := range checks {
		results = append(results, check.Run(contexts)...)
	}
	return results
}
