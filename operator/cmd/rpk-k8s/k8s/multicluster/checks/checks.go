// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package checks implements a pluggable check framework for validating
// multicluster operator deployments. Each check is a self-contained unit
// that inspects one aspect of the deployment and returns results.
//
// Checks come in two flavors:
//   - ClusterCheck: runs once per cluster, given a shared CheckContext that
//     accumulates state as checks execute. Checks run in registration order
//     so later checks can depend on state populated by earlier ones.
//   - CrossClusterCheck: runs once after all per-cluster checks complete,
//     given all CheckContexts. Used for validations that compare state
//     across clusters (e.g., CA consistency, leader agreement).
package checks

import (
	"context"
	"crypto/x509"

	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

// CheckContext holds shared state for a single cluster, accumulated as
// checks execute. Earlier checks populate fields that later checks read.
type CheckContext struct {
	// Inputs — set before checks run.
	Context     string
	Namespace   string
	ServiceName string
	Ctl         *kube.Ctl

	// Accumulated state — populated by checks for downstream consumers.
	Pod         *corev1.Pod
	Deployment  *appsv1.Deployment
	DeployArgs  []string
	TLSSecret   *corev1.Secret
	CACert      *x509.Certificate
	TLSCert     *x509.Certificate
	TLSKeyMatch bool
	RaftStatus  *transportv1.StatusResponse
}

// Result is the outcome of a single validation.
type Result struct {
	// Name identifies the check that produced this result.
	Name string
	// OK is true when the check passed.
	OK bool
	// Message describes what was checked or what went wrong.
	Message string
}

// Pass returns a passing Result.
func Pass(name, message string) Result {
	return Result{Name: name, OK: true, Message: message}
}

// Fail returns a failing Result.
func Fail(name, message string) Result {
	return Result{Name: name, OK: false, Message: message}
}

// ClusterCheck validates one aspect of a single cluster's deployment.
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
