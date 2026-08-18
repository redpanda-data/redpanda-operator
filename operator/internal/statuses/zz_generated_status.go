package statuses

// GENERATED from ./statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"strings"
	"time"

	"github.com/redpanda-data/common-go/rp-controller-utils/status"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// ratelimitedCondition is a condition wrapped in some rate limiting configuration for doing
// things like debouncing reconciliation.
type ratelimitedCondition struct {
	condition metav1.Condition
	rate      time.Duration
}

// ClusterReadyCondition - This condition indicates whether a cluster is ready
// to serve any traffic. This can happen, for example if a cluster is partially
// degraded but still can process requests.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterReadyCondition string

// ClusterHealthyCondition - This condition indicates whether a cluster is
// healthy as defined by the Redpanda Admin API's cluster health endpoint.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterHealthyCondition string

// ClusterLicenseValidCondition - This condition indicates whether a cluster has
// a valid license.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a cluster.
type ClusterLicenseValidCondition string

// ClusterResourcesSyncedCondition - This condition indicates whether the
// Kubernetes resources for a cluster have been synchronized.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterResourcesSyncedCondition string

// ClusterConfigurationAppliedCondition - This condition indicates whether
// cluster configuration parameters have currently been applied to a cluster for
// the given generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterConfigurationAppliedCondition string

// ClusterQuiescedCondition - This condition is used as to indicate that the
// cluster is no longer reconciling due to it being in a finalized state for the
// current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterQuiescedCondition string

// ClusterStableCondition - This condition is used as a roll-up status for any
// sort of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a cluster.
type ClusterStableCondition string

// NodePoolBoundCondition - This condition indicates whether a node pool is
// bound to a known Redpanda cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
type NodePoolBoundCondition string

// NodePoolDeployedCondition - This condition indicates whether a node pool has
// been deployed for a known Redpanda cluster.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a node pool.
type NodePoolDeployedCondition string

// NodePoolQuiescedCondition - This condition is used as to indicate that the
// node pool is no longer reconciling due to it being in a finalized state for
// the current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
type NodePoolQuiescedCondition string

// NodePoolStableCondition - This condition is used as a roll-up status for any
// sort of automation such as terraform.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a node pool.
type NodePoolStableCondition string

// BrokerReadyCondition - This condition indicates whether the broker's pod is
// running and healthy (containers started, liveness probe passing). It does not
// imply cluster membership — see BrokerRegistered for that. It is also
// independent of the pod's Kubernetes readiness probe, which reflects overall
// cluster health via rpk cluster health and may be false even when this
// specific broker is serving data.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a broker.
type BrokerReadyCondition string

// BrokerPodScheduledCondition - This condition indicates whether the broker's
// pod has been scheduled to a node. False when the pod is stuck due to node
// affinity, resource constraints, or PV affinity.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a broker.
type BrokerPodScheduledCondition string

// BrokerStorageBoundCondition - This condition indicates whether all PVCs for
// this broker are bound to PVs.
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a broker.
type BrokerStorageBoundCondition string

// BrokerBrokerRegisteredCondition - This condition indicates whether the
// broker's node_id has been discovered via the admin API, confirming it has
// joined the Raft group. This is orthogonal to the Ready condition, which
// tracks pod health only. A broker can be Ready (pod running) but not yet
// BrokerRegistered (still joining the cluster), or BrokerRegistered but not
// Ready (pod crashed after initial registration).
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a broker.
type BrokerBrokerRegisteredCondition string

// BrokerConfigSyncedCondition - This condition indicates whether the broker's
// pod matches the desired pod template: both the config checksum and the
// restart-requiring cluster-config version. False means a rotation is pending
// (the pod will be recreated once the broker holds a valid roll-grant).
//
// This condition defaults to "Unknown" with a reason of "NotReconciled" and
// must be set by a controller when it subsequently reconciles a broker.
type BrokerConfigSyncedCondition string

// BrokerQuiescedCondition - This condition indicates the broker is no longer
// reconciling for its current generation.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a broker.
type BrokerQuiescedCondition string

// BrokerStableCondition - This condition is a roll-up status for automation
// (e.g. parent controller checking all brokers are stable before proceeding).
// It is True when Ready, StorageBound, BrokerRegistered, ConfigSynced, and
// Quiesced all evaluate to True. Each tracks one dimension: Ready = pod health,
// StorageBound = PVC binding, BrokerRegistered = cluster membership,
// ConfigSynced = no rotation pending, Quiesced = reconciliation complete.
//
// This condition defaults to "False" with a reason of "NotReconciled" and must
// be set by a controller when it subsequently reconciles a broker.
type BrokerStableCondition string

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
	// BrokerReady - This condition indicates whether the broker's pod is running
	// and healthy (containers started, liveness probe passing). It does not imply
	// cluster membership — see BrokerRegistered for that. It is also independent
	// of the pod's Kubernetes readiness probe, which reflects overall cluster
	// health via rpk cluster health and may be false even when this specific broker
	// is serving data.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a broker.
	BrokerReady = "Ready"
	// BrokerReadyReasonReady - This reason is used with the "Ready" condition when
	// it evaluates to True because the broker's pod is running and healthy.
	BrokerReadyReasonReady BrokerReadyCondition = "Ready"
	// BrokerReadyReasonNotReady - This reason is used with the "Ready" condition
	// when it evaluates to False because the broker's pod is not yet running or
	// healthy (e.g. crash loop, liveness failure).
	BrokerReadyReasonNotReady BrokerReadyCondition = "NotReady"
	// BrokerReadyReasonError - This reason is used when a broker has only been
	// partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired broker state.
	BrokerReadyReasonError BrokerReadyCondition = "Error"
	// BrokerReadyReasonTerminalError - This reason is used when a broker has
	// encountered a terminal error and will not be reconciled further.
	BrokerReadyReasonTerminalError BrokerReadyCondition = "TerminalError"

	// BrokerPodScheduled - This condition indicates whether the broker's pod has
	// been scheduled to a node. False when the pod is stuck due to node affinity,
	// resource constraints, or PV affinity.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a broker.
	BrokerPodScheduled = "PodScheduled"
	// BrokerPodScheduledReasonScheduled - This reason is used with the
	// "PodScheduled" condition when it evaluates to True because the broker's pod
	// has been scheduled to a Kubernetes node.
	BrokerPodScheduledReasonScheduled BrokerPodScheduledCondition = "Scheduled"
	// BrokerPodScheduledReasonUnschedulable - This reason is used with the
	// "PodScheduled" condition when it evaluates to False because the broker's pod
	// cannot be scheduled (e.g. node affinity, insufficient resources).
	BrokerPodScheduledReasonUnschedulable BrokerPodScheduledCondition = "Unschedulable"
	// BrokerPodScheduledReasonPodMissing - This reason is used with the
	// "PodScheduled" condition when it evaluates to False because the broker's pod
	// does not exist (initial bootstrap, mid-rotation, or after decommission).
	BrokerPodScheduledReasonPodMissing BrokerPodScheduledCondition = "PodMissing"
	// BrokerPodScheduledReasonError - This reason is used when a broker has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired broker state.
	BrokerPodScheduledReasonError BrokerPodScheduledCondition = "Error"
	// BrokerPodScheduledReasonTerminalError - This reason is used when a broker has
	// encountered a terminal error and will not be reconciled further.
	BrokerPodScheduledReasonTerminalError BrokerPodScheduledCondition = "TerminalError"

	// BrokerStorageBound - This condition indicates whether all PVCs for this
	// broker are bound to PVs.
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a broker.
	BrokerStorageBound = "StorageBound"
	// BrokerStorageBoundReasonBound - This reason is used with the "StorageBound"
	// condition when it evaluates to True because all PVCs for this broker are
	// bound to PVs.
	BrokerStorageBoundReasonBound BrokerStorageBoundCondition = "Bound"
	// BrokerStorageBoundReasonPending - This reason is used with the "StorageBound"
	// condition when it evaluates to False because one or more PVCs for this broker
	// are not yet bound.
	BrokerStorageBoundReasonPending BrokerStorageBoundCondition = "Pending"
	// BrokerStorageBoundReasonError - This reason is used when a broker has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired broker state.
	BrokerStorageBoundReasonError BrokerStorageBoundCondition = "Error"
	// BrokerStorageBoundReasonTerminalError - This reason is used when a broker has
	// encountered a terminal error and will not be reconciled further.
	BrokerStorageBoundReasonTerminalError BrokerStorageBoundCondition = "TerminalError"

	// BrokerBrokerRegistered - This condition indicates whether the broker's
	// node_id has been discovered via the admin API, confirming it has joined the
	// Raft group. This is orthogonal to the Ready condition, which tracks pod
	// health only. A broker can be Ready (pod running) but not yet BrokerRegistered
	// (still joining the cluster), or BrokerRegistered but not Ready (pod crashed
	// after initial registration).
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a broker.
	BrokerBrokerRegistered = "BrokerRegistered"
	// BrokerBrokerRegisteredReasonRegistered - This reason is used with the
	// "BrokerRegistered" condition when it evaluates to True because the broker has
	// registered its node_id with the cluster.
	BrokerBrokerRegisteredReasonRegistered BrokerBrokerRegisteredCondition = "Registered"
	// BrokerBrokerRegisteredReasonNotRegistered - This reason is used with the
	// "BrokerRegistered" condition when it evaluates to False because the broker
	// has not yet registered with the cluster.
	BrokerBrokerRegisteredReasonNotRegistered BrokerBrokerRegisteredCondition = "NotRegistered"
	// BrokerBrokerRegisteredReasonIdentityChanged - This reason is used with the
	// "BrokerRegistered" condition when it evaluates to False because the broker's
	// pod re-registered under a different node_id than previously recorded — its
	// data directory did not survive. This requires an operator decision (replace
	// the broker) rather than silent adoption of the new identity.
	BrokerBrokerRegisteredReasonIdentityChanged BrokerBrokerRegisteredCondition = "IdentityChanged"
	// BrokerBrokerRegisteredReasonError - This reason is used when a broker has
	// only been partially reconciled and we have early returned due to a retryable
	// error occurring prior to applying the desired broker state.
	BrokerBrokerRegisteredReasonError BrokerBrokerRegisteredCondition = "Error"
	// BrokerBrokerRegisteredReasonTerminalError - This reason is used when a broker
	// has encountered a terminal error and will not be reconciled further.
	BrokerBrokerRegisteredReasonTerminalError BrokerBrokerRegisteredCondition = "TerminalError"

	// BrokerConfigSynced - This condition indicates whether the broker's pod
	// matches the desired pod template: both the config checksum and the
	// restart-requiring cluster-config version. False means a rotation is pending
	// (the pod will be recreated once the broker holds a valid roll-grant).
	//
	// This condition defaults to "Unknown" with a reason of "NotReconciled" and
	// must be set by a controller when it subsequently reconciles a broker.
	BrokerConfigSynced = "ConfigSynced"
	// BrokerConfigSyncedReasonSynced - This reason is used with the "ConfigSynced"
	// condition when it evaluates to True because the pod's config checksum and
	// cluster-config version match the desired pod template.
	BrokerConfigSyncedReasonSynced BrokerConfigSyncedCondition = "Synced"
	// BrokerConfigSyncedReasonOutdated - This reason is used with the
	// "ConfigSynced" condition when it evaluates to False because the pod's config
	// checksum or cluster-config version differs from the desired pod template and
	// the pod awaits rotation.
	BrokerConfigSyncedReasonOutdated BrokerConfigSyncedCondition = "Outdated"
	// BrokerConfigSyncedReasonError - This reason is used when a broker has only
	// been partially reconciled and we have early returned due to a retryable error
	// occurring prior to applying the desired broker state.
	BrokerConfigSyncedReasonError BrokerConfigSyncedCondition = "Error"
	// BrokerConfigSyncedReasonTerminalError - This reason is used when a broker has
	// encountered a terminal error and will not be reconciled further.
	BrokerConfigSyncedReasonTerminalError BrokerConfigSyncedCondition = "TerminalError"

	// BrokerQuiesced - This condition indicates the broker is no longer reconciling
	// for its current generation.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a broker.
	BrokerQuiesced = "Quiesced"
	// BrokerQuiescedReasonQuiesced - This reason is used with the "Quiesced"
	// condition when it evaluates to True because the operator has finished
	// reconciling the broker at its current generation.
	BrokerQuiescedReasonQuiesced BrokerQuiescedCondition = "Quiesced"
	// BrokerQuiescedReasonStillReconciling - This reason is used with the
	// "Quiesced" condition when it evaluates to False because the operator has not
	// finished reconciling the broker at its current generation.
	BrokerQuiescedReasonStillReconciling BrokerQuiescedCondition = "StillReconciling"

	// BrokerStable - This condition is a roll-up status for automation (e.g. parent
	// controller checking all brokers are stable before proceeding). It is True
	// when Ready, StorageBound, BrokerRegistered, ConfigSynced, and Quiesced all
	// evaluate to True. Each tracks one dimension: Ready = pod health, StorageBound
	// = PVC binding, BrokerRegistered = cluster membership, ConfigSynced = no
	// rotation pending, Quiesced = reconciliation complete.
	//
	// This condition defaults to "False" with a reason of "NotReconciled" and must
	// be set by a controller when it subsequently reconciles a broker.
	BrokerStable = "Stable"
	// BrokerStableReasonStable - This reason is used with the "Stable" condition
	// when it evaluates to True because Ready, StorageBound, BrokerRegistered,
	// ConfigSynced, and Quiesced all evaluate to True.
	BrokerStableReasonStable BrokerStableCondition = "Stable"
	// BrokerStableReasonUnstable - This reason is used with the "Stable" condition
	// when it evaluates to False because at least one of Ready, StorageBound,
	// BrokerRegistered, ConfigSynced, or Quiesced evaluates to False.
	BrokerStableReasonUnstable BrokerStableCondition = "Unstable"
)

// ClusterStatus - Defines the observed status conditions of a cluster.
type ClusterStatus struct {
	conditions                           []metav1.Condition
	hasTerminalError                     bool
	isReadySet                           bool
	isReadyTransientError                bool
	isHealthySet                         bool
	isHealthyTransientError              bool
	isLicenseValidSet                    bool
	isLicenseValidTransientError         bool
	isResourcesSyncedSet                 bool
	isResourcesSyncedTransientError      bool
	isConfigurationAppliedSet            bool
	isConfigurationAppliedTransientError bool
}

// NewCluster() returns a new ClusterStatus
func NewCluster() *ClusterStatus {
	return &ClusterStatus{}
}

// UpdateConditions updates any conditions for the passed in object that need to be updated.
func (s *ClusterStatus) UpdateConditions(o client.Object) bool {
	var conditions *[]metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		conditions = &kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	updated := false
	for _, condition := range s.getRateLimitedConditions(o.GetGeneration()) {
		if setStatusCondition(conditions, condition) {
			updated = true
		}
	}

	return updated
}

// StatusConditionConfigs returns a set of configurations that can be used with Server Side Apply.
func (s *ClusterStatus) StatusConditionConfigs(o client.Object) []*applymetav1.ConditionApplyConfiguration {
	var conditions []metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		conditions = kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	return status.ConditionApplyConfigs(conditions, o.GetGeneration(), s.getConditions(o.GetGeneration()))
}

// getRateLimit returns the rate limiting configuration for a given condition
func (s *ClusterStatus) getRateLimit(conditionType string) time.Duration {
	switch conditionType {
	case ClusterLicenseValid:
		return 5 * time.Minute
	case ClusterConfigurationApplied:
		return 5 * time.Minute
	}
	return 0
}

// getRateLimitedConditions returns the rate limited aggregated status conditions of the ClusterStatus.
func (s *ClusterStatus) getRateLimitedConditions(generation int64) []ratelimitedCondition {
	conditions := []ratelimitedCondition{}

	for _, condition := range s.getConditions(generation) {
		conditions = append(conditions, ratelimitedCondition{
			condition: condition,
			rate:      s.getRateLimit(condition.Type),
		})
	}

	return conditions
}

// conditions returns the aggregated status conditions of the ClusterStatus.
func (s *ClusterStatus) getConditions(generation int64) []metav1.Condition {
	conditions := append([]metav1.Condition{}, s.conditions...)
	conditions = append(conditions, s.getQuiesced())
	conditions = append(conditions, s.getStable(conditions))

	for i, condition := range conditions {
		condition.ObservedGeneration = generation
		conditions[i] = condition
	}

	return conditions
}

// SetReadyFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetReadyFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterReady)
	if condition == nil {
		return
	}

	s.SetReady(ClusterReadyCondition(condition.Reason), condition.Message)
}

// SetReady sets the underlying condition to the given reason.
func (s *ClusterStatus) SetReady(reason ClusterReadyCondition, messages ...string) {
	if s.isReadySet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isReadySet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterReadyReasonReady:
		if message == "" {
			message = "Cluster ready to service requests"
		}
		status = metav1.ConditionTrue
	case ClusterReadyReasonNotReady:
		status = metav1.ConditionFalse
	case ClusterReadyReasonError:
		s.isReadyTransientError = true
		status = metav1.ConditionFalse
	case ClusterReadyReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    ClusterReady,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetHealthyFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetHealthyFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterHealthy)
	if condition == nil {
		return
	}

	s.SetHealthy(ClusterHealthyCondition(condition.Reason), condition.Message)
}

// SetHealthy sets the underlying condition to the given reason.
func (s *ClusterStatus) SetHealthy(reason ClusterHealthyCondition, messages ...string) {
	if s.isHealthySet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isHealthySet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterHealthyReasonHealthy:
		if message == "" {
			message = "Cluster is healthy"
		}
		status = metav1.ConditionTrue
	case ClusterHealthyReasonNotHealthy:
		status = metav1.ConditionFalse
	case ClusterHealthyReasonError:
		s.isHealthyTransientError = true
		status = metav1.ConditionFalse
	case ClusterHealthyReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    ClusterHealthy,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetLicenseValidFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetLicenseValidFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterLicenseValid)
	if condition == nil {
		return
	}

	s.SetLicenseValid(ClusterLicenseValidCondition(condition.Reason), condition.Message)
}

// SetLicenseValid sets the underlying condition to the given reason.
func (s *ClusterStatus) SetLicenseValid(reason ClusterLicenseValidCondition, messages ...string) {
	if s.isLicenseValidSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isLicenseValidSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterLicenseValidReasonValid:
		if message == "" {
			message = "Cluster has a valid license"
		}
		status = metav1.ConditionTrue
	case ClusterLicenseValidReasonExpired:
		if message == "" {
			message = "Cluster license has expired"
		}
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonNotPresent:
		if message == "" {
			message = "No cluster license is present"
		}
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonError:
		s.isLicenseValidTransientError = true
		status = metav1.ConditionFalse
	case ClusterLicenseValidReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    ClusterLicenseValid,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetResourcesSyncedFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetResourcesSyncedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterResourcesSynced)
	if condition == nil {
		return
	}

	s.SetResourcesSynced(ClusterResourcesSyncedCondition(condition.Reason), condition.Message)
}

// SetResourcesSynced sets the underlying condition to the given reason.
func (s *ClusterStatus) SetResourcesSynced(reason ClusterResourcesSyncedCondition, messages ...string) {
	if s.isResourcesSyncedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isResourcesSyncedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterResourcesSyncedReasonSynced:
		if message == "" {
			message = "Cluster resources successfully synced"
		}
		status = metav1.ConditionTrue
	case ClusterResourcesSyncedReasonError:
		s.isResourcesSyncedTransientError = true
		status = metav1.ConditionFalse
	case ClusterResourcesSyncedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    ClusterResourcesSynced,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetConfigurationAppliedFromCurrent sets the underlying condition based on an existing object.
func (s *ClusterStatus) SetConfigurationAppliedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), ClusterConfigurationApplied)
	if condition == nil {
		return
	}

	s.SetConfigurationApplied(ClusterConfigurationAppliedCondition(condition.Reason), condition.Message)
}

// SetConfigurationApplied sets the underlying condition to the given reason.
func (s *ClusterStatus) SetConfigurationApplied(reason ClusterConfigurationAppliedCondition, messages ...string) {
	if s.isConfigurationAppliedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isConfigurationAppliedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case ClusterConfigurationAppliedReasonApplied:
		if message == "" {
			message = "Cluster configuration successfully applied"
		}
		status = metav1.ConditionTrue
	case ClusterConfigurationAppliedReasonNotApplied:
		if message == "" {
			message = "Cluster configuration not applied"
		}
		status = metav1.ConditionFalse
	case ClusterConfigurationAppliedReasonError:
		s.isConfigurationAppliedTransientError = true
		status = metav1.ConditionFalse
	case ClusterConfigurationAppliedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    ClusterConfigurationApplied,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

func (s *ClusterStatus) getQuiesced() metav1.Condition {
	transientErrorConditionsSet := s.isReadyTransientError || s.isHealthyTransientError || s.isLicenseValidTransientError || s.isResourcesSyncedTransientError || s.isConfigurationAppliedTransientError
	allConditionsSet := s.isReadySet && s.isHealthySet && s.isLicenseValidSet && s.isResourcesSyncedSet && s.isConfigurationAppliedSet

	if (allConditionsSet || s.hasTerminalError) && !transientErrorConditionsSet {
		return metav1.Condition{
			Type:    ClusterQuiesced,
			Status:  metav1.ConditionTrue,
			Reason:  string(ClusterQuiescedReasonQuiesced),
			Message: "Cluster reconciliation finished",
		}
	}

	return metav1.Condition{
		Type:    ClusterQuiesced,
		Status:  metav1.ConditionFalse,
		Reason:  string(ClusterQuiescedReasonStillReconciling),
		Message: "Cluster still reconciling",
	}
}

func (s *ClusterStatus) getStable(conditions []metav1.Condition) metav1.Condition {
	allConditionsFoundAndTrue := true
	for _, condition := range []string{ClusterQuiesced, ClusterReady, ClusterResourcesSynced, ClusterConfigurationApplied} {
		conditionFoundAndTrue := false
		for _, setCondition := range conditions {
			if setCondition.Type == condition {
				conditionFoundAndTrue = setCondition.Status == metav1.ConditionTrue
				break
			}
		}
		if !conditionFoundAndTrue {
			allConditionsFoundAndTrue = false
			break
		}
	}

	if allConditionsFoundAndTrue {
		return metav1.Condition{
			Type:    ClusterStable,
			Status:  metav1.ConditionTrue,
			Reason:  string(ClusterStableReasonStable),
			Message: "Cluster Stable",
		}
	}

	return metav1.Condition{
		Type:    ClusterStable,
		Status:  metav1.ConditionFalse,
		Reason:  string(ClusterStableReasonUnstable),
		Message: "Cluster Unstable",
	}
}

// NodePoolStatus - Defines the observed status conditions of a node pool.
type NodePoolStatus struct {
	conditions               []metav1.Condition
	hasTerminalError         bool
	isBoundSet               bool
	isBoundTransientError    bool
	isDeployedSet            bool
	isDeployedTransientError bool
}

// NewNodePool() returns a new NodePoolStatus
func NewNodePool() *NodePoolStatus {
	return &NodePoolStatus{}
}

// UpdateConditions updates any conditions for the passed in object that need to be updated.
func (s *NodePoolStatus) UpdateConditions(o client.Object) bool {
	var conditions *[]metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.NodePool:
		conditions = &kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	updated := false
	for _, condition := range s.getRateLimitedConditions(o.GetGeneration()) {
		if setStatusCondition(conditions, condition) {
			updated = true
		}
	}

	return updated
}

// StatusConditionConfigs returns a set of configurations that can be used with Server Side Apply.
func (s *NodePoolStatus) StatusConditionConfigs(o client.Object) []*applymetav1.ConditionApplyConfiguration {
	var conditions []metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.NodePool:
		conditions = kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	return status.ConditionApplyConfigs(conditions, o.GetGeneration(), s.getConditions(o.GetGeneration()))
}

// getRateLimit returns the rate limiting configuration for a given condition
func (s *NodePoolStatus) getRateLimit(conditionType string) time.Duration {
	switch conditionType {
	}
	return 0
}

// getRateLimitedConditions returns the rate limited aggregated status conditions of the NodePoolStatus.
func (s *NodePoolStatus) getRateLimitedConditions(generation int64) []ratelimitedCondition {
	conditions := []ratelimitedCondition{}

	for _, condition := range s.getConditions(generation) {
		conditions = append(conditions, ratelimitedCondition{
			condition: condition,
			rate:      s.getRateLimit(condition.Type),
		})
	}

	return conditions
}

// conditions returns the aggregated status conditions of the NodePoolStatus.
func (s *NodePoolStatus) getConditions(generation int64) []metav1.Condition {
	conditions := append([]metav1.Condition{}, s.conditions...)
	conditions = append(conditions, s.getQuiesced())
	conditions = append(conditions, s.getStable(conditions))

	for i, condition := range conditions {
		condition.ObservedGeneration = generation
		conditions[i] = condition
	}

	return conditions
}

// SetBoundFromCurrent sets the underlying condition based on an existing object.
func (s *NodePoolStatus) SetBoundFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), NodePoolBound)
	if condition == nil {
		return
	}

	s.SetBound(NodePoolBoundCondition(condition.Reason), condition.Message)
}

// SetBound sets the underlying condition to the given reason.
func (s *NodePoolStatus) SetBound(reason NodePoolBoundCondition, messages ...string) {
	if s.isBoundSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isBoundSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case NodePoolBoundReasonBound:
		if message == "" {
			message = "Node pool successfully bound to cluster"
		}
		status = metav1.ConditionTrue
	case NodePoolBoundReasonNotBound:
		if message == "" {
			message = "Node pool not bound to cluster"
		}
		status = metav1.ConditionFalse
	case NodePoolBoundReasonError:
		s.isBoundTransientError = true
		status = metav1.ConditionFalse
	case NodePoolBoundReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    NodePoolBound,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetDeployedFromCurrent sets the underlying condition based on an existing object.
func (s *NodePoolStatus) SetDeployedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), NodePoolDeployed)
	if condition == nil {
		return
	}

	s.SetDeployed(NodePoolDeployedCondition(condition.Reason), condition.Message)
}

// SetDeployed sets the underlying condition to the given reason.
func (s *NodePoolStatus) SetDeployed(reason NodePoolDeployedCondition, messages ...string) {
	if s.isDeployedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isDeployedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case NodePoolDeployedReasonDeployed:
		if message == "" {
			message = "Node pool successfully deployed to cluster"
		}
		status = metav1.ConditionTrue
	case NodePoolDeployedReasonScaling:
		if message == "" {
			message = "Node pool is scaling"
		}
		status = metav1.ConditionFalse
	case NodePoolDeployedReasonNotDeployed:
		if message == "" {
			message = "Node pool not deployed to cluster"
		}
		status = metav1.ConditionFalse
	case NodePoolDeployedReasonError:
		s.isDeployedTransientError = true
		status = metav1.ConditionFalse
	case NodePoolDeployedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    NodePoolDeployed,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

func (s *NodePoolStatus) getQuiesced() metav1.Condition {
	transientErrorConditionsSet := s.isBoundTransientError || s.isDeployedTransientError
	allConditionsSet := s.isBoundSet && s.isDeployedSet

	if (allConditionsSet || s.hasTerminalError) && !transientErrorConditionsSet {
		return metav1.Condition{
			Type:    NodePoolQuiesced,
			Status:  metav1.ConditionTrue,
			Reason:  string(NodePoolQuiescedReasonQuiesced),
			Message: "Node pool reconciliation finished",
		}
	}

	return metav1.Condition{
		Type:    NodePoolQuiesced,
		Status:  metav1.ConditionFalse,
		Reason:  string(NodePoolQuiescedReasonStillReconciling),
		Message: "Node pool still reconciling",
	}
}

func (s *NodePoolStatus) getStable(conditions []metav1.Condition) metav1.Condition {
	allConditionsFoundAndTrue := true
	for _, condition := range []string{NodePoolBound, NodePoolDeployed, NodePoolQuiesced} {
		conditionFoundAndTrue := false
		for _, setCondition := range conditions {
			if setCondition.Type == condition {
				conditionFoundAndTrue = setCondition.Status == metav1.ConditionTrue
				break
			}
		}
		if !conditionFoundAndTrue {
			allConditionsFoundAndTrue = false
			break
		}
	}

	if allConditionsFoundAndTrue {
		return metav1.Condition{
			Type:    NodePoolStable,
			Status:  metav1.ConditionTrue,
			Reason:  string(NodePoolStableReasonStable),
			Message: "Node pool stable",
		}
	}

	return metav1.Condition{
		Type:    NodePoolStable,
		Status:  metav1.ConditionFalse,
		Reason:  string(NodePoolStableReasonUnstable),
		Message: "Node pool unstable",
	}
}

// BrokerStatus - Defines the observed status conditions of a single broker.
type BrokerStatus struct {
	conditions                       []metav1.Condition
	hasTerminalError                 bool
	isReadySet                       bool
	isReadyTransientError            bool
	isPodScheduledSet                bool
	isPodScheduledTransientError     bool
	isStorageBoundSet                bool
	isStorageBoundTransientError     bool
	isBrokerRegisteredSet            bool
	isBrokerRegisteredTransientError bool
	isConfigSyncedSet                bool
	isConfigSyncedTransientError     bool
}

// NewBroker() returns a new BrokerStatus
func NewBroker() *BrokerStatus {
	return &BrokerStatus{}
}

// UpdateConditions updates any conditions for the passed in object that need to be updated.
func (s *BrokerStatus) UpdateConditions(o client.Object) bool {
	var conditions *[]metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Broker:
		conditions = &kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	updated := false
	for _, condition := range s.getRateLimitedConditions(o.GetGeneration()) {
		if setStatusCondition(conditions, condition) {
			updated = true
		}
	}

	return updated
}

// StatusConditionConfigs returns a set of configurations that can be used with Server Side Apply.
func (s *BrokerStatus) StatusConditionConfigs(o client.Object) []*applymetav1.ConditionApplyConfiguration {
	var conditions []metav1.Condition
	switch kind := o.(type) {
	case *redpandav1alpha2.Broker:
		conditions = kind.Status.Conditions
	default:
		panic("unsupported kind")
	}

	return status.ConditionApplyConfigs(conditions, o.GetGeneration(), s.getConditions(o.GetGeneration()))
}

// getRateLimit returns the rate limiting configuration for a given condition
func (s *BrokerStatus) getRateLimit(conditionType string) time.Duration {
	switch conditionType {
	}
	return 0
}

// getRateLimitedConditions returns the rate limited aggregated status conditions of the BrokerStatus.
func (s *BrokerStatus) getRateLimitedConditions(generation int64) []ratelimitedCondition {
	conditions := []ratelimitedCondition{}

	for _, condition := range s.getConditions(generation) {
		conditions = append(conditions, ratelimitedCondition{
			condition: condition,
			rate:      s.getRateLimit(condition.Type),
		})
	}

	return conditions
}

// conditions returns the aggregated status conditions of the BrokerStatus.
func (s *BrokerStatus) getConditions(generation int64) []metav1.Condition {
	conditions := append([]metav1.Condition{}, s.conditions...)
	conditions = append(conditions, s.getQuiesced())
	conditions = append(conditions, s.getStable(conditions))

	for i, condition := range conditions {
		condition.ObservedGeneration = generation
		conditions[i] = condition
	}

	return conditions
}

// SetReadyFromCurrent sets the underlying condition based on an existing object.
func (s *BrokerStatus) SetReadyFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), BrokerReady)
	if condition == nil {
		return
	}

	s.SetReady(BrokerReadyCondition(condition.Reason), condition.Message)
}

// SetReady sets the underlying condition to the given reason.
func (s *BrokerStatus) SetReady(reason BrokerReadyCondition, messages ...string) {
	if s.isReadySet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isReadySet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case BrokerReadyReasonReady:
		if message == "" {
			message = "Broker pod is running and healthy"
		}
		status = metav1.ConditionTrue
	case BrokerReadyReasonNotReady:
		status = metav1.ConditionFalse
	case BrokerReadyReasonError:
		s.isReadyTransientError = true
		status = metav1.ConditionFalse
	case BrokerReadyReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    BrokerReady,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetPodScheduledFromCurrent sets the underlying condition based on an existing object.
func (s *BrokerStatus) SetPodScheduledFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), BrokerPodScheduled)
	if condition == nil {
		return
	}

	s.SetPodScheduled(BrokerPodScheduledCondition(condition.Reason), condition.Message)
}

// SetPodScheduled sets the underlying condition to the given reason.
func (s *BrokerStatus) SetPodScheduled(reason BrokerPodScheduledCondition, messages ...string) {
	if s.isPodScheduledSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isPodScheduledSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case BrokerPodScheduledReasonScheduled:
		if message == "" {
			message = "Pod scheduled to node"
		}
		status = metav1.ConditionTrue
	case BrokerPodScheduledReasonUnschedulable:
		status = metav1.ConditionFalse
	case BrokerPodScheduledReasonPodMissing:
		status = metav1.ConditionFalse
	case BrokerPodScheduledReasonError:
		s.isPodScheduledTransientError = true
		status = metav1.ConditionFalse
	case BrokerPodScheduledReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    BrokerPodScheduled,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetStorageBoundFromCurrent sets the underlying condition based on an existing object.
func (s *BrokerStatus) SetStorageBoundFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), BrokerStorageBound)
	if condition == nil {
		return
	}

	s.SetStorageBound(BrokerStorageBoundCondition(condition.Reason), condition.Message)
}

// SetStorageBound sets the underlying condition to the given reason.
func (s *BrokerStatus) SetStorageBound(reason BrokerStorageBoundCondition, messages ...string) {
	if s.isStorageBoundSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isStorageBoundSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case BrokerStorageBoundReasonBound:
		if message == "" {
			message = "All PVCs bound"
		}
		status = metav1.ConditionTrue
	case BrokerStorageBoundReasonPending:
		status = metav1.ConditionFalse
	case BrokerStorageBoundReasonError:
		s.isStorageBoundTransientError = true
		status = metav1.ConditionFalse
	case BrokerStorageBoundReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    BrokerStorageBound,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetBrokerRegisteredFromCurrent sets the underlying condition based on an existing object.
func (s *BrokerStatus) SetBrokerRegisteredFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), BrokerBrokerRegistered)
	if condition == nil {
		return
	}

	s.SetBrokerRegistered(BrokerBrokerRegisteredCondition(condition.Reason), condition.Message)
}

// SetBrokerRegistered sets the underlying condition to the given reason.
func (s *BrokerStatus) SetBrokerRegistered(reason BrokerBrokerRegisteredCondition, messages ...string) {
	if s.isBrokerRegisteredSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isBrokerRegisteredSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case BrokerBrokerRegisteredReasonRegistered:
		if message == "" {
			message = "Broker registered with cluster"
		}
		status = metav1.ConditionTrue
	case BrokerBrokerRegisteredReasonNotRegistered:
		status = metav1.ConditionFalse
	case BrokerBrokerRegisteredReasonIdentityChanged:
		status = metav1.ConditionFalse
	case BrokerBrokerRegisteredReasonError:
		s.isBrokerRegisteredTransientError = true
		status = metav1.ConditionFalse
	case BrokerBrokerRegisteredReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    BrokerBrokerRegistered,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

// SetConfigSyncedFromCurrent sets the underlying condition based on an existing object.
func (s *BrokerStatus) SetConfigSyncedFromCurrent(o client.Object) {
	condition := apimeta.FindStatusCondition(GetConditions(o), BrokerConfigSynced)
	if condition == nil {
		return
	}

	s.SetConfigSynced(BrokerConfigSyncedCondition(condition.Reason), condition.Message)
}

// SetConfigSynced sets the underlying condition to the given reason.
func (s *BrokerStatus) SetConfigSynced(reason BrokerConfigSyncedCondition, messages ...string) {
	if s.isConfigSyncedSet {
		panic("you should only ever set a condition once, doing so more than once is a programming error")
	}

	var status metav1.ConditionStatus

	s.isConfigSyncedSet = true
	message := strings.Join(messages, "; ")

	switch reason {
	case BrokerConfigSyncedReasonSynced:
		if message == "" {
			message = "Pod config matches desired"
		}
		status = metav1.ConditionTrue
	case BrokerConfigSyncedReasonOutdated:
		status = metav1.ConditionFalse
	case BrokerConfigSyncedReasonError:
		s.isConfigSyncedTransientError = true
		status = metav1.ConditionFalse
	case BrokerConfigSyncedReasonTerminalError:
		s.hasTerminalError = true
		status = metav1.ConditionFalse
	default:
		panic("unhandled reason type")
	}

	if message == "" {
		panic("message must be set")
	}

	s.conditions = append(s.conditions, metav1.Condition{
		Type:    BrokerConfigSynced,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})
}

func (s *BrokerStatus) getQuiesced() metav1.Condition {
	transientErrorConditionsSet := s.isReadyTransientError || s.isPodScheduledTransientError || s.isStorageBoundTransientError || s.isBrokerRegisteredTransientError || s.isConfigSyncedTransientError
	allConditionsSet := s.isReadySet && s.isPodScheduledSet && s.isStorageBoundSet && s.isBrokerRegisteredSet && s.isConfigSyncedSet

	if (allConditionsSet || s.hasTerminalError) && !transientErrorConditionsSet {
		return metav1.Condition{
			Type:    BrokerQuiesced,
			Status:  metav1.ConditionTrue,
			Reason:  string(BrokerQuiescedReasonQuiesced),
			Message: "Broker reconciliation finished",
		}
	}

	return metav1.Condition{
		Type:    BrokerQuiesced,
		Status:  metav1.ConditionFalse,
		Reason:  string(BrokerQuiescedReasonStillReconciling),
		Message: "Broker still reconciling",
	}
}

func (s *BrokerStatus) getStable(conditions []metav1.Condition) metav1.Condition {
	allConditionsFoundAndTrue := true
	for _, condition := range []string{BrokerReady, BrokerStorageBound, BrokerBrokerRegistered, BrokerConfigSynced, BrokerQuiesced} {
		conditionFoundAndTrue := false
		for _, setCondition := range conditions {
			if setCondition.Type == condition {
				conditionFoundAndTrue = setCondition.Status == metav1.ConditionTrue
				break
			}
		}
		if !conditionFoundAndTrue {
			allConditionsFoundAndTrue = false
			break
		}
	}

	if allConditionsFoundAndTrue {
		return metav1.Condition{
			Type:    BrokerStable,
			Status:  metav1.ConditionTrue,
			Reason:  string(BrokerStableReasonStable),
			Message: "Broker stable",
		}
	}

	return metav1.Condition{
		Type:    BrokerStable,
		Status:  metav1.ConditionFalse,
		Reason:  string(BrokerStableReasonUnstable),
		Message: "Broker unstable",
	}
}

// HasRecentCondition returns whether or not an object has a given condition with the given value that is up-to-date and set
// within the given time period.
func HasRecentCondition[T ~string](o client.Object, conditionType T, value metav1.ConditionStatus, period time.Duration) bool {
	condition := apimeta.FindStatusCondition(GetConditions(o), string(conditionType))
	if condition == nil {
		return false
	}

	recent := time.Since(condition.LastTransitionTime.Time) > period
	matchedCondition := condition.Status == value
	generationChanged := condition.ObservedGeneration != 0 && condition.ObservedGeneration < o.GetGeneration()

	return matchedCondition && !(generationChanged || recent)
}

// GetConditions returns the conditions for a given object.
func GetConditions(o client.Object) []metav1.Condition {
	switch kind := o.(type) {
	case *redpandav1alpha2.Redpanda:
		return kind.Status.Conditions
	case *redpandav1alpha2.NodePool:
		return kind.Status.Conditions
	case *redpandav1alpha2.Broker:
		return kind.Status.Conditions
	default:
		panic("unsupported kind")
	}
}

// setStatusCondition is a copy of the apimeta.SetStatusCondition with one primary change. Rather
// than only change the .LastTransitionTime if the .Status field of the condition changes, it
// sets it if .Status, .Reason, .Message, or .ObservedGeneration changes, which works nicely with our recent check leveraged
// for rate limiting above. It also normalizes this to be the same as what status.ConditionApplyConfigs does
func setStatusCondition(conditions *[]metav1.Condition, newCondition ratelimitedCondition) (changed bool) {
	if conditions == nil {
		return false
	}
	existingCondition := apimeta.FindStatusCondition(*conditions, newCondition.condition.Type)
	if existingCondition == nil {
		if newCondition.condition.LastTransitionTime.IsZero() {
			newCondition.condition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		*conditions = append(*conditions, newCondition.condition)
		return true
	}

	setTransitionTime := func() {
		if !newCondition.condition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.condition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
	}

	// we force an update of the transition time for the condition if an explicit
	// rate limit is configured (rate > 0). A zero rate means "no rate-limited
	// heartbeat" — without this guard, time.Since(anything) > 0 would mark the
	// condition dirty on every reconcile, causing a hot reconcile loop.
	if newCondition.rate > 0 && (time.Since(existingCondition.LastTransitionTime.Time) > newCondition.rate) {
		setTransitionTime()
		changed = true
	}

	if existingCondition.Status != newCondition.condition.Status {
		existingCondition.Status = newCondition.condition.Status
		setTransitionTime()
		changed = true
	}

	if existingCondition.Reason != newCondition.condition.Reason {
		existingCondition.Reason = newCondition.condition.Reason
		setTransitionTime()
		changed = true
	}
	if existingCondition.Message != newCondition.condition.Message {
		existingCondition.Message = newCondition.condition.Message
		setTransitionTime()
		changed = true
	}
	if existingCondition.ObservedGeneration != newCondition.condition.ObservedGeneration {
		existingCondition.ObservedGeneration = newCondition.condition.ObservedGeneration
		setTransitionTime()
		changed = true
	}

	return changed
}
