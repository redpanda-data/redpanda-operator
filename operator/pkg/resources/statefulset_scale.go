// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/patch"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
)

const (
	decommissionWaitJitterFactor = 0.2
)

// handleScaling is responsible for managing the current number of replicas running for a cluster.
//
// Replicas are controlled via the field `status.currentReplicas` that is set in the current method and should be
// respected by all other external reconcile functions, including the one that sets the actual amount of replicas in the StatefulSet.
// External functions should use `cluster.getCurrentReplicas()` to get the number of expected replicas for a cluster,
// since it deals with cases where the status is not initialized yet.
//
// When users change the value of `spec.replicas` for a cluster, this function is responsible for progressively changing the `status.currentReplicas` to match that value.
// In the case of a Cluster downscaled by 1 replica (i.e. `spec.replicas` lower than current value of `status.currentReplicas` by `1`), before lowering the
// value of `status.currentReplicas`, this handler will first decommission the last node, using the admin API of the cluster.
//
// There are cases where the cluster will not decommission a node (e.g. user reduces `spec.replicas` to `2`, but there are topics with `3` partition replicas),
// in which case the draining phase will hang indefinitely in Redpanda. When this happens, the controller will not downscale the cluster and
// users will find in `status.decommissioningNode` that a node is constantly being decommissioned.
//
// In cases where the decommissioning process hangs, users can increase again the value of `spec.replicas` and the handler will contact the admin API
// to recommission the last node, ensuring that the number of replicas matches the expected value and that the node joins the cluster again.
//
// Users can change the value of `spec.replicas` freely (except it cannot be 0). In case of downscaling by multiple replicas, the handler will
// decommission one node at time, until the desired number of replicas is met.
//
// When a new cluster is created, the value of `status.currentReplicas` will be initially set to `1`, no matter what the user sets in `spec.replicas`.
// This handler will first ensure that the initial node forms a cluster, by retrieving the list of brokers using the admin API.
// After the cluster is formed, the handler will increase the `status.currentReplicas` as requested.
//
// This is due to the fact that Redpanda is currently unable to initialize a cluster if each node is given the full list of seed servers: https://github.com/redpanda-data/redpanda/issues/333.
// Previous versions of the operator use to hack the list of seeds server (in the configurator pod) of node with ordinal 0, to set it always to an empty set,
// allowing it to create an initial cluster. That strategy worked because the list of seed servers is not read again from the `redpanda.yaml` file once the
// cluster is initialized.
// But the drawback was that, on an existing running cluster, when node 0 loses its data directory (e.g. because it's using local storage, and it undergoes a k8s node upgrade),
// then node 0 (having no data and an empty seed server list in `redpanda.yaml`) creates a brand new cluster ignoring the other nodes (split-brain).
// The strategy implemented here (to initialize the cluster at 1 replica, then upscaling to the desired number, without hacks on the seed server list),
// should fix this problem, since the list of seeds servers will be the same in all nodes once the cluster is created.
//
//nolint:nestif // for clarity
func (r *StatefulSetResource) handleScaling(ctx context.Context) error {
	log := r.logger.WithName("handleScaling").WithValues("nodepool", r.nodePool.Name)

	// This is special - we allow only one decom at a time, ACROSS ALL nodePools. It's not per nodepool.
	// if a decommission is already in progress, handle it first. If it's not finished, it will return an error
	// which will requeue the reconciliation. We can't (and don't want to) do any further scaling until it's finished.
	// handleDecommissionInProgress is supposed to exit with error if a decom is already in progress, so we don't start another decom.
	if err := r.handleDecommissionInProgress(ctx, log); err != nil {
		return err
	}

	npStatus := r.getNodePoolStatus()
	if npStatus.CurrentReplicas == 0 && ptr.Deref(r.nodePool.Replicas, 0) != 0 && !r.nodePool.Deleted {
		// Initialize the currentReplicas field.
		npStatus.CurrentReplicas = *r.nodePool.Replicas
		r.pandaCluster.Status.NodePools[r.nodePool.Name] = npStatus
		log.Info("updating current replicas during handleScaling", "current replicas", npStatus.CurrentReplicas)
		return r.Status().Update(ctx, r.pandaCluster)
	}

	npCurrentReplicas := r.pandaCluster.Status.NodePools[r.nodePool.Name].CurrentReplicas

	if ptr.Deref(r.nodePool.Replicas, 0) > npCurrentReplicas {
		r.logger.Info("Upscaling cluster", "nodepool replicas", ptr.Deref(r.nodePool.Replicas, 0), "npcurrentreplicas", npCurrentReplicas)

		log.Info("upscaling setCurrentreplicas request", "replicas", r.nodePool.Replicas, "np", r.nodePool.Name)
		// Upscaling request: this is already handled by Redpanda, so we just increase status currentReplicas
		return r.setCurrentReplicas(ctx, *r.nodePool.Replicas, r.nodePool.Name, r.logger)
	}

	if ptr.Deref(r.nodePool.Replicas, 0) == npCurrentReplicas && !r.nodePool.Deleted {
		log.V(logger.DebugLevel).Info("No scaling changes required for this nodepool", "replicas", *r.nodePool.Replicas, "spec replicas", *r.LastObservedState.Spec.Replicas) // No changes to replicas, we do nothing here

		return nil
	}

	if npCurrentReplicas == 0 {
		// we 're done here
		return nil
	}

	// User required replicas is lower than current replicas (currentReplicas): start the decommissioning process
	r.logger.Info("Downscaling cluster", "nodepool_replicas", ptr.Deref(r.nodePool.Replicas, 0), "nodepool_current_replicas", npCurrentReplicas)

	targetOrdinal := npStatus.CurrentReplicas - 1 // Always decommission last node
	targetBroker, err := r.getBrokerIDForPod(ctx, targetOrdinal)
	if err != nil {
		return fmt.Errorf("error getting broker ID for pod with ordinal %d when downscaling cluster: %w", targetOrdinal, err)
	}

	if targetBroker == nil {
		// The target pod isn't in the broker list.
		// Broker ID could be missing for a variety of reasons? If we just ignore
		// this, and remove the pod anyway and skip decom'ing..we may remove pod w/
		// ghost broker left. Instead, we only decommission, if both pod and brokerID are present.
		// This effectively means, our "state machine" requires upscaling to have finished
		// (pod started + broker registered), before we consider downscaling.
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          fmt.Sprintf("the broker for pod with ordinal %d is not registered with the cluster. Requeuing.", targetOrdinal),
		}
	}

	podName := fmt.Sprintf("%s-%d", r.LastObservedState.Name, targetOrdinal)
	log.WithValues("pod", podName, "ordinal", targetOrdinal, "node_id", targetBroker).Info("start decommission broker")

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &vectorizedv1alpha1.Cluster{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      r.pandaCluster.Name,
			Namespace: r.pandaCluster.Namespace,
		}, cluster)
		if err != nil {
			return err
		}
		cluster.SetDecommissionBrokerID(targetBroker)
		err = r.Status().Update(ctx, cluster)
		if err == nil {
			// sync original cluster variable to avoid conflicts on subsequent operations
			r.pandaCluster.SetDecommissionBrokerID(targetBroker)
		}
		return err
	})
	if err != nil {
		return err
	}

	return nil
}

// getDecommissioningPod finds the pod that belongs to *THIS* StatefulSet. If none is found - nil,nil is returned.
// This may be the case, if the pod belongs to a different STS.
func (r *StatefulSetResource) getDecommissioningPod(ctx context.Context, brokerID int32) (*corev1.Pod, error) {
	// Get all pods of *THIS* nodePool
	podList, err := r.getPodList(ctx)
	if err != nil {
		return nil, err
	}

	for _, pod := range podList.Items {
		if pod.Annotations[labels.PodNodeIDKey] == strconv.Itoa(int(brokerID)) {
			return &pod, nil
		}
	}

	return nil, nil
}

func (r *StatefulSetResource) handleDecommissionInProgress(ctx context.Context, l logr.Logger) error {
	log := l.WithName("handleDecommissionInProgress").WithValues("nodepool", r.nodePool.Name)
	brokerID := r.pandaCluster.GetDecommissionBrokerID()
	if brokerID == nil {
		return nil
	}

	brokerPod, err := r.getDecommissioningPod(ctx, *brokerID)
	if err != nil {
		return fmt.Errorf("failed to get decom pod: %w", err)
	}

	// Decom Pod is from another NodePool. Super important to early exit here, as it's not our business (in this StatefulSet handler).
	if brokerPod == nil {
		log.Info("decom on other NodePool in progress. asking for requeue so this nodepool can get reconciled afterwards.")
		return &RequeueAfterError{
			RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          "decommission on other NodePool in progress, can't handle decom for this one yet",
		}

	}

	if !r.nodePool.Deleted && *r.nodePool.Replicas >= r.pandaCluster.Status.NodePools[r.nodePool.Name].CurrentReplicas {
		// Decommissioning can also be canceled and we need to recommission
		err := r.handleRecommission(ctx)
		if !errors.Is(err, &RecommissionFatalError{}) {
			return err
		}
		// if it's impossible to recommission, fall through and let the decommission complete
		log.WithValues("node_id", r.pandaCluster.GetDecommissionBrokerID()).Info("cannot recommission broker", "error", err)
	}

	// handleDecommission will return an error until the decommission is completed
	if err := r.handleDecommission(ctx, log); err != nil {
		return err
	}

	// Broker is now removed
	npReplicas := r.pandaCluster.Status.NodePools[r.nodePool.Name].CurrentReplicas - 1
	log.WithValues("targetReplicas", npReplicas, "currentReplicas", r.pandaCluster.Status.NodePools[r.nodePool.Name].CurrentReplicas).Info("broker decommission complete: scaling down StatefulSet")

	// We set status.currentReplicas accordingly to trigger scaling down of the statefulset
	if err := r.setCurrentReplicas(ctx, npReplicas, r.nodePool.Name, r.logger); err != nil {
		return err
	}

	scaledDown, err := r.verifyRunningCount(ctx, npReplicas)
	if err != nil {
		return err
	}
	if !scaledDown {
		return &RequeueAfterError{
			RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          fmt.Sprintf("Waiting for statefulset to downscale to %d replicas", npReplicas),
		}
	}
	return nil
}

// handleDecommission manages the case of decommissioning of the last node of a cluster.
//
// When this handler is called, the `status.decommissioningNode` is populated with the
// pod ordinal of the node that needs to be decommissioned.
//
// The handler verifies that the node is not present in the list of brokers registered in the cluster, via admin API,
// then downscales the StatefulSet via decreasing the `status.currentReplicas`.
//
// Before completing the process, it double-checks if the node is still not registered, for handling cases where the node was
// about to start when the decommissioning process started. If the broker is found, the process is restarted.
func (r *StatefulSetResource) handleDecommission(ctx context.Context, l logr.Logger) error {
	brokerID := r.pandaCluster.GetDecommissionBrokerID()
	if brokerID == nil {
		return nil
	}

	log := l.WithName("handleDecommission").WithValues("node_id", *brokerID)
	log.Info("handling broker decommissioning")

	decomPod, err := r.getDecommissioningPod(ctx, *brokerID)
	if err != nil {
		return fmt.Errorf("failed to get decom pod: %w", err)
	}

	// Ignore - decom pod is from another NodePool
	if decomPod == nil {
		return fmt.Errorf("handleDecommission invoked but decomPod not found")
	}

	log.Info("Getting admin api client")
	adminAPI, err := r.getAdminAPIClient(ctx)
	if err != nil {
		return err
	}

	broker, err := getBrokerByBrokerID(ctx, *brokerID, adminAPI)
	if err != nil {
		return err
	}

	if broker == nil {
		log.Info("Broker has finished decommissioning")

		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cluster := &vectorizedv1alpha1.Cluster{}
			err := r.Get(ctx, types.NamespacedName{
				Name:      r.pandaCluster.Name,
				Namespace: r.pandaCluster.Namespace,
			}, cluster)
			if err != nil {
				return err
			}
			cluster.SetDecommissionBrokerID(nil)
			err = r.Status().Update(ctx, cluster)
			if err == nil {
				log.Info("Cleared decomm broker ID from status")
				// sync original cluster variable to avoid conflicts on subsequent operations
				r.pandaCluster.SetDecommissionBrokerID(nil)
			}
			return err
		})
	}

	if broker.MembershipStatus == rpadmin.MembershipStatusDraining {
		log.Info("broker is still draining")
		return &RequeueAfterError{
			RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          fmt.Sprintf("broker %d is in the process of draining", *brokerID),
		}
	}

	// start decommissioning
	err = adminAPI.DecommissionBroker(ctx, broker.NodeID)
	if err != nil {
		return fmt.Errorf("error while trying to decommission node %d: %w", broker.NodeID, err)
	}
	log.Info("broker marked for decommissioning")
	return &RequeueAfterError{
		RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
		Msg:          fmt.Sprintf("waiting for broker %d to be decommissioned", *brokerID),
	}
}

type RecommissionFatalError struct {
	Err string
}

func (e *RecommissionFatalError) Error() string {
	return fmt.Sprintf("recommission error: %v", e.Err)
}

// handleRecommission manages the case of a broker being recommissioned after a failed/wrong decommission.
//
// Recommission can only work for brokers that are still in the "draining" phase according to Redpanda.
//
// When this handler is triggered, `status.decommissioningNode` is populated with the broker id that was being decommissioned and
// `spec.replicas` reports a value that include a pod for that broker, indicating the intention from the user to recommission it.
//
// The handler ensures that the broker is running and also calls the admin API to recommission it.
// The process finishes when the broker is registered with redpanda and the StatefulSet is correctly scaled.
func (r *StatefulSetResource) handleRecommission(ctx context.Context) error {
	brokerID := r.pandaCluster.GetDecommissionBrokerID()
	if brokerID == nil {
		return nil
	}
	log := r.logger.WithName("handleRecommission").WithValues("node_id", *brokerID)
	log.Info("handling broker recommissioning")

	adminAPI, err := r.getAdminAPIClient(ctx)
	if err != nil {
		return err
	}

	broker, err := getBrokerByBrokerID(ctx, *r.pandaCluster.GetDecommissionBrokerID(), adminAPI)
	if err != nil {
		return err
	}

	// ensure the pod still exists
	if pod, shadowErr := r.getPodByBrokerID(ctx, brokerID); shadowErr != nil || pod == nil {
		return &RecommissionFatalError{Err: fmt.Sprintf("the pod for broker %d does not exist", *brokerID)}
	}

	if broker == nil {
		return &RecommissionFatalError{Err: fmt.Sprintf("cannot recommission broker %d: already fully decommissioned", *brokerID)}
	}

	if broker.MembershipStatus == rpadmin.MembershipStatusActive {
		log.Info("Recommissioning process successfully completed")
		r.pandaCluster.SetDecommissionBrokerID(nil)
		return r.Status().Update(ctx, r.pandaCluster)
	}

	err = adminAPI.RecommissionBroker(ctx, int(*brokerID))
	if err != nil {
		return fmt.Errorf("error while trying to recommission broker %d: %w", *brokerID, err)
	}
	log.Info("broker being recommissioned")

	return &RequeueAfterError{
		RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
		Msg:          fmt.Sprintf("waiting for broker %d to be recommissioned", *brokerID),
	}
}

func (r *StatefulSetResource) getAdminAPIClient(
	ctx context.Context, pods ...string,
) (adminutils.AdminAPIClient, error) {
	return r.adminAPIClientFactory(ctx, r, r.pandaCluster, r.serviceFQDN, r.adminTLSConfigProvider, pods...)
}

// disableMaintenanceModeOnDecommissionedNodes can be used to put a cluster in a consistent state, disabling maintenance mode on
// nodes that have been decommissioned.
//
// A decommissioned node may activate maintenance mode via shutdown hooks and the cluster may enter an inconsistent state,
// preventing other pods clean shutdown.
//
// See: https://github.com/redpanda-data/redpanda/issues/4999
func (r *StatefulSetResource) disableMaintenanceModeOnDecommissionedNodes(
	ctx context.Context,
) error {
	brokerID := r.pandaCluster.GetDecommissionBrokerID()
	if brokerID == nil {
		// Only if actually in a decommissioning phase
		return nil
	}
	log := r.logger.WithName("disableMaintenanceModeOnDecommissionedNodes").WithValues("node_id", *brokerID)

	if !featuregates.MaintenanceMode(r.pandaCluster.Status.Version) {
		return nil
	}

	pod, err := r.getPodByBrokerID(ctx, brokerID)
	if err != nil {
		return err
	}

	// if there is a pod for this broker, it's not finished decommissioning
	if pod != nil {
		return nil
	}

	adminAPI, err := r.getAdminAPIClient(ctx)
	if err != nil {
		return err
	}

	log.Info("Forcing deletion of maintenance mode for the decommissioned node")
	err = adminAPI.DisableMaintenanceMode(ctx, int(*brokerID), false)
	if err != nil {
		var httpErr *rpadmin.HTTPResponseError
		if errors.As(err, &httpErr) {
			if httpErr.Response != nil && httpErr.Response.StatusCode/100 == 4 {
				// Cluster says we don't need to do it
				log.Info("No need to disable maintenance mode on the decommissioned node", "status_code", httpErr.Response.StatusCode)
				return nil
			}
		}
		return fmt.Errorf("could not disable maintenance mode on decommissioning node %d: %w", *brokerID, err)
	}
	log.Info("Maintenance mode disabled for the decommissioned node")
	return nil
}

// verifyRunningCount checks if the statefulset is configured to run the given amount of replicas and that also pods match the expectations
func (r *StatefulSetResource) verifyRunningCount(
	ctx context.Context, replicas int32,
) (bool, error) {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, r.Key(), &sts); err != nil {
		return false, fmt.Errorf("could not get statefulset for checking replicas: %w", err)
	}
	if sts.Spec.Replicas == nil || *sts.Spec.Replicas != replicas || sts.Status.Replicas != replicas {
		return false, nil
	}

	podList, err := r.GetNodePoolPods(ctx)
	if err != nil {
		return false, fmt.Errorf("could not list pods for checking replicas: %w", err)
	}

	return len(podList.Items) == int(replicas), nil
}

// getBrokerByBrokerID allows to get broker information using the admin API
func getBrokerByBrokerID(
	ctx context.Context, brokerID int32, adminAPI adminutils.AdminAPIClient,
) (*rpadmin.Broker, error) {
	brokers, err := adminAPI.Brokers(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get the list of brokers for checking decommission: %w", err)
	}
	for i := range brokers {
		if brokers[i].NodeID == int(brokerID) {
			return &brokers[i], nil
		}
	}
	return nil, nil
}

// setCurrentReplicas allows to set the number of status.currentReplicas in the CR, which in turns controls the replicas
// assigned to the StatefulSet
func (r *StatefulSetResource) setCurrentReplicas(
	ctx context.Context,
	npReplicas int32,
	npName string,
	l logr.Logger,
) error {
	log := l.WithName("setCurrentReplicas")
	log.Info("setting currentReplicas", "npReplicas", npReplicas, "np", npName)

	var newReplicas int32
	result, err := patch.PatchStatus(ctx, r, r.pandaCluster, func(cluster *vectorizedv1alpha1.Cluster) {
		if cluster.Status.NodePools == nil {
			cluster.Status.NodePools = make(map[string]vectorizedv1alpha1.NodePoolStatus)
		}
		npStatus, ok := cluster.Status.NodePools[npName]
		if !ok {
			npStatus = vectorizedv1alpha1.NodePoolStatus{}
			cluster.Status.NodePools[npName] = npStatus
		}

		if r.nodePool.Deleted {
			npStatus.Replicas = npReplicas
		} else {
			npStatus.Replicas = *r.nodePool.Replicas
		}
		npStatus.CurrentReplicas = npReplicas
		newReplicas = npStatus.CurrentReplicas
		cluster.Status.NodePools[npName] = npStatus
	})
	if err != nil {
		return fmt.Errorf("could not scale cluster %s nodePool %s to %d replicas: %w", r.pandaCluster.Name, npName, npReplicas, err)
	}
	r.pandaCluster.Status = result
	log.Info("StatefulSet scaled", "replicas", newReplicas)
	return nil
}

// NodeReappearingError indicates that a node has appeared in the cluster before completion of the a direct downscale
type NodeReappearingError struct {
	NodeID int
}

// Error makes the NodeReappearingError a proper error
func (e *NodeReappearingError) Error() string {
	return fmt.Sprintf("node has appeared in the cluster with id=%d", e.NodeID)
}
