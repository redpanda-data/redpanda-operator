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

// ClusterReadyCondition - This condition indicates whether a cluster is ready
// to serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterReadyCondition string

// ClusterHealthyCondition - This condition indicates whether a cluster is
// healthy as defined by the Redpanda Admin API's cluster health endpoint.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterHealthyCondition string

// ClusterLicenseValidCondition - This condition indicates whether a cluster has
// a valid license.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterLicenseValidCondition string

// ClusterResourcesSyncedCondition - This condition indicates whether the
// Kubernetes resources for a cluster have been synchronized.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterResourcesSyncedCondition string

// ClusterConfigurationAppliedCondition - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterConfigurationAppliedCondition string

// ClusterQuiescedCondition - This condition is used as to indicate that the
// cluster is no longer reconciling due to it being in a finalized state for the
// current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterQuiescedCondition string

// ClusterStableCondition - This condition is used as a roll-up status for any
// sort of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
// +statusType
type ClusterStableCondition string

// NodePoolBoundCondition - This condition indicates whether a node pool is
// bound to a known Redpanda cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolBoundCondition string

// NodePoolDeployedCondition - This condition indicates whether a node pool has
// been deployed for a known Redpanda cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolDeployedCondition string

// NodePoolQuiescedCondition - This condition is used as to indicate that the
// node pool is no longer reconciling due to it being in a finalized state for
// the current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolQuiescedCondition string

// NodePoolStableCondition - This condition is used as a roll-up status for any
// sort of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
// +statusType
type NodePoolStableCondition string

const (
	// ClusterReady - This condition indicates whether a cluster is ready to serve
	// any traffic. This can happen, for example if a cluster is partially degraded
	// but still can process requests.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterReady = "Ready"
	// ClusterReadyReasonReady - This reason is used with the "Ready" condition when
	// it evaluates to True because a cluster can service traffic.
	ClusterReadyReasonReady ClusterReadyCondition = "Ready"
	// ClusterReadyReasonNotReady - This reason is used with the "Ready" condition
	// when it evaluates to False because a cluster is not ready to service traffic.
	ClusterReadyReasonNotReady ClusterReadyCondition = "NotReady"
	// ClusterReadyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterReadyReasonError ClusterReadyCondition = "Error"
	// ClusterReadyReasonTerminalError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a known terminal
	// error occurring prior to applying the desired cluster state. Because the
	// cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterReadyReasonTerminalError ClusterReadyCondition = "TerminalError"

	// ClusterHealthy - This condition indicates whether a cluster is healthy as
	// defined by the Redpanda Admin API's cluster health endpoint.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterHealthy = "Healthy"
	// ClusterHealthyReasonHealthy - This reason is used with the "Healthy"
	// condition when it evaluates to True because a cluster's health endpoint says
	// the cluster is healthy.
	ClusterHealthyReasonHealthy ClusterHealthyCondition = "Healthy"
	// ClusterHealthyReasonNotHealthy - This reason is used with the "Healthy"
	// condition when it evaluates to False because a cluster's health endpoint says
	// the cluster is not healthy.
	ClusterHealthyReasonNotHealthy ClusterHealthyCondition = "NotHealthy"
	// ClusterHealthyReasonError - This reason is used when a cluster has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterHealthyReasonError ClusterHealthyCondition = "Error"
	// ClusterHealthyReasonTerminalError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Because
	// the cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterHealthyReasonTerminalError ClusterHealthyCondition = "TerminalError"

	// ClusterLicenseValid - This condition indicates whether a cluster has a valid
	// license.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a cluster.
	ClusterLicenseValid = "LicenseValid"
	// ClusterLicenseValidReasonValid - This reason is used with the "LicenseValid"
	// condition when it evaluates to True because a cluster has a valid license.
	ClusterLicenseValidReasonValid ClusterLicenseValidCondition = "Valid"
	// ClusterLicenseValidReasonExpired - This reason is used with the
	// "LicenseValid" condition when it evaluates to False because a cluster has an
	// expired license.
	ClusterLicenseValidReasonExpired ClusterLicenseValidCondition = "Expired"
	// ClusterLicenseValidReasonNotPresent - This reason is used with the
	// "LicenseValid" condition when it evaluates to False because a cluster has no
	// license.
	ClusterLicenseValidReasonNotPresent ClusterLicenseValidCondition = "NotPresent"
	// ClusterLicenseValidReasonError - This reason is used when a cluster has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired cluster state. If it is set on any
	// non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterLicenseValidReasonError ClusterLicenseValidCondition = "Error"
	// ClusterLicenseValidReasonTerminalError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired cluster state. Because
	// the cluster should no longer be reconciled when a terminal error occurs, the
	// "Quiesced" status should be set to True.
	ClusterLicenseValidReasonTerminalError ClusterLicenseValidCondition = "TerminalError"

	// ClusterResourcesSynced - This condition indicates whether the Kubernetes
	// resources for a cluster have been synchronized.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterResourcesSynced = "ResourcesSynced"
	// ClusterResourcesSyncedReasonSynced - This reason is used with the
	// "ResourcesSynced" condition when it evaluates to True because a cluster has
	// had all of its Kubernetes resources synced.
	ClusterResourcesSyncedReasonSynced ClusterResourcesSyncedCondition = "Synced"
	// ClusterResourcesSyncedReasonError - This reason is used when a cluster has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired cluster state. If it is set on
	// any non-final condition, then the condition "Quiesced" will be False with a
	// reason of "SillReconciling".
	ClusterResourcesSyncedReasonError ClusterResourcesSyncedCondition = "Error"
	// ClusterResourcesSyncedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Because the cluster should no longer be reconciled when a terminal error
	// occurs, the "Quiesced" status should be set to True.
	ClusterResourcesSyncedReasonTerminalError ClusterResourcesSyncedCondition = "TerminalError"

	// ClusterConfigurationApplied - This condition indicates whether cluster
	// configuration parameters have currently been applied to a cluster for the
	// given generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterConfigurationApplied = "ConfigurationApplied"
	// ClusterConfigurationAppliedReasonApplied - This reason is used with the
	// "ConfigurationApplied" condition when it evaluates to True because a cluster
	// has had its cluster configuration parameters applied.
	ClusterConfigurationAppliedReasonApplied ClusterConfigurationAppliedCondition = "Applied"
	// ClusterConfigurationAppliedReasonNotApplied - This reason is used with the
	// "ConfigurationApplied" condition when it evaluates to False due to some
	// implementation-specific condition, such as when no brokers have been created
	// and thus we can't attempt a configuration.
	ClusterConfigurationAppliedReasonNotApplied ClusterConfigurationAppliedCondition = "NotApplied"
	// ClusterConfigurationAppliedReasonError - This reason is used when a cluster
	// has only been partially reconciled and we have early returned due to a
	// retryable error occurring prior to applying the desired cluster state. If it
	// is set on any non-final condition, then the condition "Quiesced" will be
	// False with a reason of "SillReconciling".
	ClusterConfigurationAppliedReasonError ClusterConfigurationAppliedCondition = "Error"
	// ClusterConfigurationAppliedReasonTerminalError - This reason is used when a
	// cluster has only been partially reconciled and we have early returned due to
	// a known terminal error occurring prior to applying the desired cluster state.
	// Because the cluster should no longer be reconciled when a terminal error
	// occurs, the "Quiesced" status should be set to True.
	ClusterConfigurationAppliedReasonTerminalError ClusterConfigurationAppliedCondition = "TerminalError"

	// ClusterQuiesced - This condition is used as to indicate that the cluster is
	// no longer reconciling due to it being in a finalized state for the current
	// generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterQuiesced = "Quiesced"
	// ClusterQuiescedReasonQuiesced - This reason is used with the "Quiesced"
	// condition when it evaluates to True because the operator has finished
	// reconciling the cluster at its current generation.
	ClusterQuiescedReasonQuiesced ClusterQuiescedCondition = "Quiesced"
	// ClusterQuiescedReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when it evaluates to False because the operator has not
	// finished reconciling the cluster at its current generation. This can happen
	// when, for example, we're doing a cluster scaling operation or a non-terminal
	// error has been encountered during reconciliation.
	ClusterQuiescedReasonStillReconciling ClusterQuiescedCondition = "StillReconciling"

	// ClusterStable - This condition is used as a roll-up status for any sort of
	// automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a cluster.
	ClusterStable = "Stable"
	// ClusterStableReasonStable - This reason is used with the "Stable" condition
	// when it evaluates to True because all dependent conditions also evaluate to
	// True.
	ClusterStableReasonStable ClusterStableCondition = "Stable"
	// ClusterStableReasonUnstable - This reason is used with the "Stable" condition
	// when it evaluates to True because at least one dependent condition evaluates
	// to False.
	ClusterStableReasonUnstable ClusterStableCondition = "Unstable"
	// NodePoolBound - This condition indicates whether a node pool is bound to a
	// known Redpanda cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a node pool.
	NodePoolBound = "Bound"
	// NodePoolBoundReasonBound - This reason is used with the "Bound" condition
	// when it evaluates to True because a node pool is bound to a cluster.
	NodePoolBoundReasonBound NodePoolBoundCondition = "Bound"
	// NodePoolBoundReasonNotBound - This reason is used with the "Bound" condition
	// when it evaluates to False because a node pool is not bound to a cluster.
	NodePoolBoundReasonNotBound NodePoolBoundCondition = "NotBound"
	// NodePoolBoundReasonError - This reason is used when a node pool has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired node pool state.
	NodePoolBoundReasonError NodePoolBoundCondition = "Error"
	// NodePoolBoundReasonTerminalError - This reason is used when a node pool has
	// only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired node pool state.
	NodePoolBoundReasonTerminalError NodePoolBoundCondition = "TerminalError"

	// NodePoolDeployed - This condition indicates whether a node pool has been
	// deployed for a known Redpanda cluster.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a node pool.
	NodePoolDeployed = "Deployed"
	// NodePoolDeployedReasonDeployed - This reason is used with the "Deployed"
	// condition when it evaluates to True because a node pool has been fully
	// deployed for a cluster.
	NodePoolDeployedReasonDeployed NodePoolDeployedCondition = "Deployed"
	// NodePoolDeployedReasonScaling - This reason is used with the "Deployed"
	// condition when it evaluates to False because a node pool has not yet been
	// fully deployed for a cluster.
	NodePoolDeployedReasonScaling NodePoolDeployedCondition = "Scaling"
	// NodePoolDeployedReasonNotDeployed - This reason is used with the "Deployed"
	// condition when it evaluates to False because a node pool has not started to
	// deploy for a cluster.
	NodePoolDeployedReasonNotDeployed NodePoolDeployedCondition = "NotDeployed"
	// NodePoolDeployedReasonError - This reason is used when a node pool has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired node pool state.
	NodePoolDeployedReasonError NodePoolDeployedCondition = "Error"
	// NodePoolDeployedReasonTerminalError - This reason is used when a node pool
	// has only been partially reconciled and we have early returned due to a known
	// terminal error occurring prior to applying the desired node pool state.
	NodePoolDeployedReasonTerminalError NodePoolDeployedCondition = "TerminalError"

	// NodePoolQuiesced - This condition is used as to indicate that the node pool
	// is no longer reconciling due to it being in a finalized state for the current
	// generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a node pool.
	NodePoolQuiesced = "Quiesced"
	// NodePoolQuiescedReasonQuiesced - This reason is used with the "Quiesced"
	// condition when it evaluates to True because the operator has finished
	// reconciling the node pool at its current generation.
	NodePoolQuiescedReasonQuiesced NodePoolQuiescedCondition = "Quiesced"
	// NodePoolQuiescedReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when it evaluates to False because the operator has not
	// finished reconciling the node pool at its current generation.
	NodePoolQuiescedReasonStillReconciling NodePoolQuiescedCondition = "StillReconciling"

	// NodePoolStable - This condition is used as a roll-up status for any sort of
	// automation such as terraform.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a node pool.
	NodePoolStable = "Stable"
	// NodePoolStableReasonStable - This reason is used with the "Stable" condition
	// when it evaluates to True because all dependent conditions also evaluate to
	// True.
	NodePoolStableReasonStable NodePoolStableCondition = "Stable"
	// NodePoolStableReasonUnstable - This reason is used with the "Stable"
	// condition when it evaluates to True because at least one dependent condition
	// evaluates to False.
	NodePoolStableReasonUnstable NodePoolStableCondition = "Unstable"
)
