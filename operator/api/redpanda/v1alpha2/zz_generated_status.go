// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

// GENERATED from ./statuses.yaml, DO NOT EDIT DIRECTLY

// This condition indicates whether a cluster is ready to serve any traffic.
// This can happen, for example if a cluster is partially degraded but still can
// process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterReadyCondition string

// This condition indicates whether a cluster is healthy as defined by the
// Redpanda Admin API's cluster health endpoint.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterHealthyCondition string

// This condition indicates whether a cluster has a valid license.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterLicenseValidCondition string

// This condition indicates whether the Kubernetes resources for a cluster have
// been synchronized.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterResourcesSyncedCondition string

// This condition indicates whether cluster configuration parameters have
// currently been applied to a cluster for the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterConfigurationAppliedCondition string

// This condition is used as to indicate that the cluster is no longer
// reconciling due to it being in a finalized state for the current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterQuiescedCondition string

// This condition is used as a roll-up status for any sort of automation such as
// terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterStableCondition string

// This condition indicates whether a node pool is bound to a known Redpanda
// cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolBoundCondition string

// This condition indicates whether a node pool has been deployed for a known
// Redpanda cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolDeployedCondition string

// This condition is used as to indicate that the node pool is no longer
// reconciling due to it being in a finalized state for the current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolQuiescedCondition string

// This condition is used as a roll-up status for any sort of automation such as
// terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolStableCondition string

const (
	// This reason is used with the "Ready" condition when it evaluates to True
	// because a cluster can service traffic.
	ClusterReadyReasonReady ClusterReadyCondition = "Ready"
	// This reason is used with the "Ready" condition when it evaluates to False
	// because a cluster is not ready to service traffic.
	ClusterReadyReasonNotReady ClusterReadyCondition = "NotReady"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a retryable error occurring prior to applying the
	// desired cluster state. If it is set on any non-final condition, then the
	// condition "Quiesced" will be False with a reason of "SillReconciling".
	ClusterReadyReasonError ClusterReadyCondition = "Error"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a known terminal error occurring prior to applying
	// the desired cluster state. Because the cluster should no longer be reconciled
	// when a terminal error occurs, the "Quiesced" status should be set to True.
	ClusterReadyReasonTerminalError ClusterReadyCondition = "TerminalError"

	// This reason is used with the "Healthy" condition when it evaluates to True
	// because a cluster's health endpoint says the cluster is healthy.
	ClusterHealthyReasonHealthy ClusterHealthyCondition = "Healthy"
	// This reason is used with the "Healthy" condition when it evaluates to False
	// because a cluster's health endpoint says the cluster is not healthy.
	ClusterHealthyReasonNotHealthy ClusterHealthyCondition = "NotHealthy"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a retryable error occurring prior to applying the
	// desired cluster state. If it is set on any non-final condition, then the
	// condition "Quiesced" will be False with a reason of "SillReconciling".
	ClusterHealthyReasonError ClusterHealthyCondition = "Error"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a known terminal error occurring prior to applying
	// the desired cluster state. Because the cluster should no longer be reconciled
	// when a terminal error occurs, the "Quiesced" status should be set to True.
	ClusterHealthyReasonTerminalError ClusterHealthyCondition = "TerminalError"

	// This reason is used with the "LicenseValid" condition when it evaluates to
	// True because a cluster has a valid license.
	ClusterLicenseValidReasonValid ClusterLicenseValidCondition = "Valid"
	// This reason is used with the "LicenseValid" condition when it evaluates to
	// False because a cluster has an expired license.
	ClusterLicenseValidReasonExpired ClusterLicenseValidCondition = "Expired"
	// This reason is used with the "LicenseValid" condition when it evaluates to
	// False because a cluster has no license.
	ClusterLicenseValidReasonNotPresent ClusterLicenseValidCondition = "NotPresent"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a retryable error occurring prior to applying the
	// desired cluster state. If it is set on any non-final condition, then the
	// condition "Quiesced" will be False with a reason of "SillReconciling".
	ClusterLicenseValidReasonError ClusterLicenseValidCondition = "Error"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a known terminal error occurring prior to applying
	// the desired cluster state. Because the cluster should no longer be reconciled
	// when a terminal error occurs, the "Quiesced" status should be set to True.
	ClusterLicenseValidReasonTerminalError ClusterLicenseValidCondition = "TerminalError"

	// This reason is used with the "ResourcesSynced" condition when it evaluates to
	// True because a cluster has had all of its Kubernetes resources synced.
	ClusterResourcesSyncedReasonSynced ClusterResourcesSyncedCondition = "Synced"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a retryable error occurring prior to applying the
	// desired cluster state. If it is set on any non-final condition, then the
	// condition "Quiesced" will be False with a reason of "SillReconciling".
	ClusterResourcesSyncedReasonError ClusterResourcesSyncedCondition = "Error"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a known terminal error occurring prior to applying
	// the desired cluster state. Because the cluster should no longer be reconciled
	// when a terminal error occurs, the "Quiesced" status should be set to True.
	ClusterResourcesSyncedReasonTerminalError ClusterResourcesSyncedCondition = "TerminalError"

	// This reason is used with the "ConfigurationApplied" condition when it
	// evaluates to True because a cluster has had its cluster configuration
	// parameters applied.
	ClusterConfigurationAppliedReasonApplied ClusterConfigurationAppliedCondition = "Applied"
	// This reason is used with the "ConfigurationApplied" condition when it
	// evaluates to False due to some implementation-specific condition, such as
	// when no brokers have been created and thus we can't attempt a configuration.
	ClusterConfigurationAppliedReasonNotApplied ClusterConfigurationAppliedCondition = "NotApplied"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a retryable error occurring prior to applying the
	// desired cluster state. If it is set on any non-final condition, then the
	// condition "Quiesced" will be False with a reason of "SillReconciling".
	ClusterConfigurationAppliedReasonError ClusterConfigurationAppliedCondition = "Error"
	// This reason is used when a cluster has only been partially reconciled and we
	// have early returned due to a known terminal error occurring prior to applying
	// the desired cluster state. Because the cluster should no longer be reconciled
	// when a terminal error occurs, the "Quiesced" status should be set to True.
	ClusterConfigurationAppliedReasonTerminalError ClusterConfigurationAppliedCondition = "TerminalError"

	// This reason is used with the "Quiesced" condition when it evaluates to True
	// because the operator has finished reconciling the cluster at its current
	// generation.
	ClusterQuiescedReasonQuiesced ClusterQuiescedCondition = "Quiesced"
	// This reason is used with the "Quiesced" condition when it evaluates to False
	// because the operator has not finished reconciling the cluster at its current
	// generation. This can happen when, for example, we're doing a cluster scaling
	// operation or a non-terminal error has been encountered during reconciliation.
	ClusterQuiescedReasonStillReconciling ClusterQuiescedCondition = "StillReconciling"

	// This reason is used with the "Stable" condition when it evaluates to True
	// because all dependent conditions also evaluate to True.
	ClusterStableReasonStable ClusterStableCondition = "Stable"
	// This reason is used with the "Stable" condition when it evaluates to True
	// because at least one dependent condition evaluates to False.
	ClusterStableReasonUnstable ClusterStableCondition = "Unstable"
	// This reason is used with the "Bound" condition when it evaluates to True
	// because a node pool is bound to a cluster.
	NodePoolBoundReasonBound NodePoolBoundCondition = "Bound"
	// This reason is used with the "Bound" condition when it evaluates to False
	// because a node pool is not bound to a cluster.
	NodePoolBoundReasonNotBound NodePoolBoundCondition = "NotBound"
	// This reason is used when a node pool has only been partially reconciled and
	// we have early returned due to a retryable error occurring prior to applying
	// the desired node pool state.
	NodePoolBoundReasonError NodePoolBoundCondition = "Error"
	// This reason is used when a node pool has only been partially reconciled and
	// we have early returned due to a known terminal error occurring prior to
	// applying the desired node pool state.
	NodePoolBoundReasonTerminalError NodePoolBoundCondition = "TerminalError"

	// This reason is used with the "Deployed" condition when it evaluates to True
	// because a node pool has been fully deployed for a cluster.
	NodePoolDeployedReasonDeployed NodePoolDeployedCondition = "Deployed"
	// This reason is used with the "Deployed" condition when it evaluates to False
	// because a node pool has not yet been fully deployed for a cluster.
	NodePoolDeployedReasonScaling NodePoolDeployedCondition = "Scaling"
	// This reason is used with the "Deployed" condition when it evaluates to False
	// because a node pool has not started to deploy for a cluster.
	NodePoolDeployedReasonNotDeployed NodePoolDeployedCondition = "NotDeployed"
	// This reason is used when a node pool has only been partially reconciled and
	// we have early returned due to a retryable error occurring prior to applying
	// the desired node pool state.
	NodePoolDeployedReasonError NodePoolDeployedCondition = "Error"
	// This reason is used when a node pool has only been partially reconciled and
	// we have early returned due to a known terminal error occurring prior to
	// applying the desired node pool state.
	NodePoolDeployedReasonTerminalError NodePoolDeployedCondition = "TerminalError"

	// This reason is used with the "Quiesced" condition when it evaluates to True
	// because the operator has finished reconciling the node pool at its current
	// generation.
	NodePoolQuiescedReasonQuiesced NodePoolQuiescedCondition = "Quiesced"
	// This reason is used with the "Quiesced" condition when it evaluates to False
	// because the operator has not finished reconciling the node pool at its
	// current generation.
	NodePoolQuiescedReasonStillReconciling NodePoolQuiescedCondition = "StillReconciling"

	// This reason is used with the "Stable" condition when it evaluates to True
	// because all dependent conditions also evaluate to True.
	NodePoolStableReasonStable NodePoolStableCondition = "Stable"
	// This reason is used with the "Stable" condition when it evaluates to True
	// because at least one dependent condition evaluates to False.
	NodePoolStableReasonUnstable NodePoolStableCondition = "Unstable"
)
