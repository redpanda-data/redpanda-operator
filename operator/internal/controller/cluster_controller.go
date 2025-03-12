// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda-operator/operator/internal/resources"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const (
	traceLevel = 2
	debugLevel = 1
	infoLevel  = 0
)

const clusterFinalizer = "cluster.redpanda.com/finalizer"

var requeueErr = errors.New("requeue")

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler[T any, U resources.Cluster[T]] struct {
	client.Client
	ResourceManager resources.ResourceManager[T, U]
	ResourceClient  resources.ResourceClient[T, U]
	ClientFactory   internalclient.ClientFactory
}

func ignoreConflict(err error) (ctrl.Result, error) {
	if k8sapierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}

func (r *ClusterReconciler[T, U]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cluster := resources.NewClusterObject[T, U]()
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T]", *cluster))

	if err := r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	sets, err := r.ResourceClient.ListOwnedResources(ctx, cluster, &appsv1.StatefulSet{})
	if err != nil {
		logger.Error(err, "fetching cluster pods")
		return ctrl.Result{}, err
	}

	pools := resources.NewPoolManager(cluster.GetGeneration())

	for _, set := range sets {
		statefulSet := set.(*appsv1.StatefulSet)

		selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
		if err != nil {
			logger.Error(err, "constructing label selector")
			return ctrl.Result{}, err
		}

		revisions, err := r.ResourceClient.ListResources(ctx, &appsv1.ControllerRevision{}, client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			logger.Error(err, "listing revisions")
			return ctrl.Result{}, err
		}
		ownedRevisions := []*appsv1.ControllerRevision{}
		for i := range revisions {
			ref := metav1.GetControllerOfNoCopy(revisions[i])
			if ref == nil || ref.UID == set.GetUID() {
				ownedRevisions = append(ownedRevisions, revisions[i].(*appsv1.ControllerRevision))
			}

		}

		pods, err := r.ResourceClient.ListResources(ctx, &corev1.Pod{}, client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			logger.Error(err, "listing pods")
			return ctrl.Result{}, err
		}

		podNames := []string{}
		for _, pod := range pods {
			podNames = append(podNames, client.ObjectKeyFromObject(pod).String())
		}
		logger.V(traceLevel).Info("adding existing pool", "StatefulSet", client.ObjectKeyFromObject(statefulSet).String(), "Pods", podNames)

		if err := pools.AddExisting(statefulSet, ownedRevisions, pods...); err != nil {
			logger.Error(err, "adding existing pool")
			return ctrl.Result{}, err
		}
	}

	status := resources.ClusterStatus{}

	syncStatus := func(err error) (ctrl.Result, error) {
		var requeue bool
		if errors.Is(err, requeueErr) {
			err = nil
			requeue = true
		}

		if r.ResourceManager.SetClusterStatus(cluster, status) {
			syncErr := r.Client.Status().Update(ctx, cluster)
			err = errors.Join(syncErr, err)
		}

		result, err := ignoreConflict(err)
		if requeue {
			result.Requeue = true
		}

		return result, err
	}

	// we are being deleted, clean up everything
	if cluster.GetDeletionTimestamp() != nil {
		// clean up all dependant resources
		if deleted, err := r.ResourceClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return syncStatus(err)
		}

		if controllerutil.RemoveFinalizer(cluster, clusterFinalizer) {
			if err := r.Client.Update(ctx, cluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point
				return ignoreConflict(err)
			}
		}

		return ctrl.Result{}, nil
	}

	// we are not deleting, so add a finalizer first before
	// allocating any additional resources
	if controllerutil.AddFinalizer(cluster, clusterFinalizer) {
		if err := r.Client.Update(ctx, cluster); err != nil {
			logger.Error(err, "updating cluster finalizer")
			return ignoreConflict(err)
		}
		return ctrl.Result{}, nil
	}

	desired, err := r.ResourceManager.NodePools(ctx, cluster)
	if err != nil {
		logger.Error(err, "constructing cluster resources")
		return syncStatus(err)
	}

	pools.AddDesired(desired...)

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.ResourceClient.SyncAll(ctx, cluster); err != nil {
		logger.Error(err, "error synchronizing resources")
		return syncStatus(err)
	}

	var (
		fetched bool
		admin   *rpadmin.AdminAPI
		health  rpadmin.ClusterHealthOverview
	)
	fetchClusterHealth := func() (*rpadmin.AdminAPI, rpadmin.ClusterHealthOverview, error) {
		if fetched {
			return admin, health, nil
		}
		adminClient, err := r.ClientFactory.RedpandaAdminClient(ctx, cluster)
		if err != nil {
			return nil, health, err
		}
		clusterHealth, err := adminClient.GetHealthOverview(ctx)
		if err != nil {
			return nil, health, err
		}

		admin = adminClient
		health = clusterHealth
		fetched = true

		return admin, health, nil
	}

	switch pools.CheckScale() {
	case resources.ScaleNotReady:
		logger.V(traceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so wait
		return syncStatus(requeueErr)
	case resources.ScaleNeedsClusterCheck:
		logger.V(traceLevel).Info("checking cluster stability")

		_, health, err := fetchClusterHealth()
		if err != nil {
			logger.Error(err, "fetching cluster health")
			return syncStatus(err)
		}
		if !health.IsHealthy {
			// TODO: is this the right check?
			// do we also need a check for decommissioning
			// nodes?
			logger.V(traceLevel).Info("cluster not currently healthy")
			return syncStatus(requeueErr)
		}

		fallthrough
	case resources.ScaleReady:
		logger.V(traceLevel).Info("ready to scale and apply node pools", "existing", pools.ExistingStatefulSets(), "desired", pools.DesiredStatefulSets())

		brokerMap := map[string]int{}

		patchStatefulSet := func(set *appsv1.StatefulSet, onError string) (ctrl.Result, error) {
			logger.V(traceLevel).Info("patching StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

			if err := r.ResourceClient.PatchOwnedResource(ctx, cluster, set); err != nil {
				logger.Error(err, onError)
				return syncStatus(err)
			}
			// we only do a statefulset at a time, waiting for them to
			// become stable first
			return syncStatus(requeueErr)
		}

		scaleDown := func(set *resources.ScaleDownSet) (ctrl.Result, error) {
			logger.V(traceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

			brokerID, ok := brokerMap[set.LastPod.GetName()]
			if ok {
				// decommission if we have a brokerID, if not
				// then the node has already been fully removed from
				// the cluster and we can go ahead and delete the pod
				// through patching the statefulset

				logger.V(traceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

				status, err := admin.DecommissionBrokerStatus(ctx, brokerID)
				if err != nil {
					if strings.Contains(err.Error(), "is not decommissioning") {
						logger.V(traceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

						if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
							logger.Error(err, "decommissioning broker")
							return syncStatus(err)
						}
						return syncStatus(requeueErr)
					} else {
						logger.Error(err, "fetching decommission status")
						return syncStatus(err)
					}
				}
				if !status.Finished {
					logger.V(traceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

					// just requeue since we're still decommissioning
					return syncStatus(requeueErr)
				}
			}

			logger.V(traceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

			// now patch the statefulset to remove the pod
			return patchStatefulSet(set.StatefulSet, "scaling down statefulset")
		}

		// first create any pools that don't currently exists
		for _, set := range pools.ToCreate() {
			logger.V(traceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
			return patchStatefulSet(set, "creating node pool statefulset")
		}

		// next scale up any under-provisioned pools and patch them to use the new spec
		for _, set := range pools.ToScaleUp() {
			logger.V(traceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
			return patchStatefulSet(set, "scaling up statefulset")
		}

		admin, health, err := fetchClusterHealth()
		if err != nil {
			logger.Error(err, "fetching cluster health")
			return syncStatus(err)
		}

		for _, brokerID := range health.AllNodes {
			broker, err := admin.Broker(ctx, brokerID)
			if err != nil {
				logger.Error(err, "fetching broker")
				return syncStatus(err)
			}

			brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
			brokerMap[brokerTokens[0]] = brokerID
		}

		// next scale down any over-provisioned pools, patching them to use the new spec
		// and decommissioning any nodes as needed
		for _, set := range pools.ToScaleDown() {
			return scaleDown(set)
		}

		// at this point any set that needs to be deleted should have 0 replicas
		// so we can attempt to delete them all in one pass
		for _, set := range pools.ToDelete() {
			logger.V(traceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
			if err := r.Client.Delete(ctx, set); err != nil {
				logger.Error(err, "deleting statefulset")
				return syncStatus(err)
			}
		}

		// now make sure all of the patch any sets that might have changed without affecting the cluster size
		// here we can just wholesale patch everything
		updates := pools.RequiresUpdate()
		for _, set := range updates {
			logger.V(traceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
			if _, err := patchStatefulSet(set, "updating statefulset"); err != nil {
				return syncStatus(err)
			}
		}

		if len(updates) != 0 {
			return syncStatus(requeueErr)
		}

		// finally, we make sure we roll every pod that is not in-sync with its statefulset
		for _, pod := range pools.PodsToRoll() {
			logger.V(traceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := r.Client.Delete(ctx, pod); err != nil {
				logger.Error(err, "deleting pod")
				return syncStatus(err)
			}
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return syncStatus(requeueErr)
		}
	}

	logger.V(traceLevel).Info("cluster quiesced")

	// if we got here, then all of the loops above were no-ops
	// and so we can mark the status as quiesced.
	status.Quiesced = true

	return syncStatus(nil)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler[T, U]) SetupWithManager(mgr ctrl.Manager) error {
	cluster := resources.NewClusterObject[T, U]()

	builder := ctrl.NewControllerManagedBy(mgr).For(cluster).Owns(&appsv1.StatefulSet{})

	if err := r.ResourceClient.WatchResources(builder, cluster); err != nil {
		return err
	}

	return builder.Complete(r)
}
