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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	"github.com/prometheus/common/expfmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/common-go/rpadmin"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/featuregates"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

const (
	// RequeueDuration is the time controller should
	// requeue resource reconciliation.
	RequeueDuration        = time.Second * 10
	defaultAdminAPITimeout = time.Second * 2

	ManagedDecommissionAnnotation = "operator.redpanda.com/managed-decommission"
	ClusterUpdatePodCondition     = corev1.PodConditionType("ClusterUpdate")
)

var (
	errRedpandaNotReady         = errors.New("redpanda not ready")
	errUnderReplicatedPartition = errors.New("partition under replicated")
)

func (r *StatefulSetResource) getNodePoolStatus() vectorizedv1alpha1.NodePoolStatus {
	if r.pandaCluster.Status.NodePools == nil {
		r.pandaCluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{}
	}

	npStatus, ok := r.pandaCluster.Status.NodePools[r.nodePool.Name]
	if !ok {
		npStatus = vectorizedv1alpha1.NodePoolStatus{
			CurrentReplicas: ptr.Deref(r.nodePool.Replicas, 0),
		}
		r.pandaCluster.Status.NodePools[r.nodePool.Name] = npStatus
	}
	return npStatus
}

// runUpdate handles image changes and additional storage in the redpanda cluster
// CR by removing statefulset with orphans Pods. The stateful set is then recreated
// and all Pods are restarted accordingly to the ordinal number.
//
// The process maintains an Restarting bool status that is set to true once the
// generated stateful differentiate from the actual state. It is set back to
// false when all pods are verified.
//
// The steps are as follows: 1) check the Restarting status or if the statefulset
// differentiate from the current stored statefulset definition 2) if true,
// set the Restarting status to true and remove statefulset with the orphan Pods
// 3) perform rolling update like removing Pods accordingly to theirs ordinal
// number 4) requeue until the pod is in ready state 5) prior to a pod update
// verify the previously updated pod and requeue as necessary. Currently, the
// verification checks the pod has started listening in its http Admin API port and may be
// extended.
func (r *StatefulSetResource) runUpdate(
	ctx context.Context, current, modified *appsv1.StatefulSet,
) error {
	log := r.logger.WithName("runUpdate").WithValues("nodepool", r.nodePool.Name)

	// Keep existing central config hash annotation during standard reconciliation
	if ann, ok := current.Spec.Template.Annotations[CentralizedConfigurationHashAnnotationKey]; ok {
		if modified.Spec.Template.Annotations == nil {
			modified.Spec.Template.Annotations = make(map[string]string)
		}
		modified.Spec.Template.Annotations[CentralizedConfigurationHashAnnotationKey] = ann
	}

	// Check if we should run update on this specific STS.

	log.V(logger.DebugLevel).Info("Checking that we should update")
	update, err := r.shouldUpdate(current, modified)
	if err != nil {
		return fmt.Errorf("unable to determine the update procedure: %w", err)
	}

	if !update {
		return nil
	}

	// At this point, we have seen a diff and want to update the StatefulSet.
	npStatus := r.getNodePoolStatus()

	// If NodePool is not yet flagged as Restarting, do it.
	if !npStatus.Restarting {
		log.V(logger.DebugLevel).Info("NodePool is not yet flagged as restarting, but has modifications. Setting restarting to true.")
		if err = r.updateRestartingStatus(ctx, true); err != nil {
			return fmt.Errorf("unable to turn on restarting status in cluster custom resource: %w", err)
		}

		log.V(logger.DebugLevel).Info("Setting ClusterUpdate condition on pods")
		if err = r.MarkPodsForUpdate(ctx); err != nil {
			return fmt.Errorf("unable to mark pods for update: %w", err)
		}
	}

	log.V(logger.DebugLevel).Info("updating statefulset")
	if err = r.updateStatefulSet(ctx, current, modified); err != nil {
		return err
	}

	// Cluster must be healthy before doing a rolling update.
	log.V(logger.DebugLevel).Info("checking if cluster is healthy")
	if err = r.isClusterHealthy(ctx); err != nil {
		return err
	}

	log.V(logger.DebugLevel).Info("performing rolling update")
	if err = r.rollingUpdate(ctx, &modified.Spec.Template); err != nil {
		return err
	}

	ok, err := r.areAllPodsUpdated(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return &RequeueAfterError{
			RequeueAfter: wait.Jitter(r.decommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          "waiting for all pods to be updated",
		}
	}

	// If update is complete for all pods (and all are ready). Set restarting status to false.
	log.V(logger.DebugLevel).Info("pod update complete, set Cluster restarting status to false")
	if err = r.updateRestartingStatus(ctx, false); err != nil {
		return fmt.Errorf("unable to turn off restarting status in cluster custom resource: %w", err)
	}
	if err = r.removeManagedDecommissionAnnotation(ctx); err != nil {
		return fmt.Errorf("unable to remove managed decommission annotation: %w", err)
	}

	return nil
}

func (r *StatefulSetResource) isClusterHealthy(ctx context.Context) error {
	if !featuregates.ClusterHealth(r.pandaCluster.Status.Version) {
		r.logger.V(logger.DebugLevel).Info("Cluster health endpoint is not available", "version", r.pandaCluster.Spec.Version)
		return nil
	}

	adminAPIClient, err := r.getAdminAPIClient(ctx)
	if err != nil {
		return fmt.Errorf("creating admin API client: %w", err)
	}

	health, err := adminAPIClient.GetHealthOverview(ctx)
	if err != nil {
		return fmt.Errorf("getting cluster health overview: %w", err)
	}

	restarting := "not restarting"
	if r.getRestartingStatus() {
		restarting = "restarting"
	}

	if !health.IsHealthy {
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          fmt.Sprintf("wait for cluster to become healthy (cluster %s)", restarting),
		}
	}

	return nil
}

func (r *StatefulSetResource) getPodList(ctx context.Context) (*corev1.PodList, error) {
	var podList corev1.PodList
	err := r.List(ctx, &podList, &k8sclient.ListOptions{
		Namespace:     r.pandaCluster.Namespace,
		LabelSelector: labels.ForCluster(r.pandaCluster).WithNodePool(r.nodePool.Name).AsClientSelectorForNodePool(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list pods: %w", err)
	}

	return sortPodList(&podList, r.pandaCluster), nil
}

// getPodListWithoutNodePoolLabel is required for migration from pre-nodepool to nodepool only.
func (r *StatefulSetResource) getPodListWithoutNodePoolLabel(ctx context.Context) (*corev1.PodList, error) {
	var podList corev1.PodList
	err := r.List(ctx, &podList, &k8sclient.ListOptions{
		Namespace:     r.pandaCluster.Namespace,
		LabelSelector: labels.ForCluster(r.pandaCluster).AsClientSelector(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list pods: %w", err)
	}

	podList.Items = slices.DeleteFunc(podList.Items, func(pod corev1.Pod) bool {
		if _, ok := pod.GetObjectMeta().GetLabels()[labels.NodePoolKey]; ok {
			return true
		}

		return false
	})

	return sortPodList(&podList, r.pandaCluster), nil
}

func sortPodList(podList *corev1.PodList, cluster *vectorizedv1alpha1.Cluster) *corev1.PodList {
	sort.Slice(podList.Items, func(i, j int) bool {
		iOrdinal, _ := utils.GetPodOrdinal(podList.Items[i].GetName(), cluster.GetName())
		jOrdinal, _ := utils.GetPodOrdinal(podList.Items[j].GetName(), cluster.GetName())

		return iOrdinal < jOrdinal
	})
	return podList
}

func (r *StatefulSetResource) MarkPodsForUpdate(ctx context.Context) error {
	podList, err := r.getPodList(ctx)
	if err != nil {
		return fmt.Errorf("error getting pods %w", err)
	}

	// Handle legacy nodepool not having NP label possibly.
	if err := r.maybeAddLegacyPods(ctx, podList); err != nil {
		return err
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		podPatch := k8sclient.MergeFrom(pod.DeepCopy())
		newCondition := corev1.PodCondition{
			Type:    ClusterUpdatePodCondition,
			Status:  corev1.ConditionTrue,
			Message: "Cluster update pending",
		}
		utils.SetStatusPodCondition(&pod.Status.Conditions, &newCondition)
		if err := r.Client.Status().Patch(ctx, pod, podPatch); err != nil {
			return fmt.Errorf("error setting pod update condition: %w", err)
		}
	}
	return nil
}

func (r *StatefulSetResource) areAllPodsUpdated(ctx context.Context) (bool, error) {
	podList, err := r.getPodList(ctx)
	if err != nil {
		return false, fmt.Errorf("error getting pods %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		cond := utils.FindStatusPodCondition(pod.Status.Conditions, ClusterUpdatePodCondition)
		if cond != nil && cond.Status == corev1.ConditionTrue {
			return false, nil
		}
	}
	return true, nil
}

func (r *StatefulSetResource) maybeAddLegacyPods(ctx context.Context, podList *corev1.PodList) error {
	log := r.logger.WithName("addLegacyPods")

	// Special case: if there's still pods w/o nodepool annotation, mark these.
	// We can just mark them, because at this point the STS has been modified accordingly already, and the label was added to pod template
	if r.nodePool.Name == vectorizedv1alpha1.DefaultNodePoolName {
		withoutPodLabels, err := r.getPodListWithoutNodePoolLabel(ctx)
		if err != nil {
			return fmt.Errorf("failed to get pods without nodePool labels: %w", err)
		}
		podList.Items = append(podList.Items, withoutPodLabels.Items...)
		if len(withoutPodLabels.Items) > 0 {
			log.Info("Added additional pods from unmigrated default pool", "len", len(withoutPodLabels.Items))
		}
	}

	return nil
}

func (r *StatefulSetResource) rollingUpdate(ctx context.Context, template *corev1.PodTemplateSpec) error {
	log := r.logger.WithName("rollingUpdate")
	podList, err := r.getPodList(ctx)
	if err != nil {
		return fmt.Errorf("error getting pods %w", err)
	}
	if err := r.maybeAddLegacyPods(ctx, podList); err != nil {
		return err
	}

	updateItems, maintenanceItems := r.listPodsForUpdateOrMaintenance(log, podList)

	if err = r.checkMaintenanceModeForPods(ctx, maintenanceItems); err != nil {
		return fmt.Errorf("error checking maintenance mode for pods: %w", err)
	}

	// There are no pods left to update, we're done.
	if len(updateItems) == 0 {
		log.V(logger.DebugLevel).Info("no pods to roll")
		return nil
	}
	log.V(logger.DebugLevel).Info("rolling pods", "number of pods", len(updateItems))

	return r.updatePods(ctx, log, updateItems, template)
}

func (r *StatefulSetResource) updatePods(ctx context.Context, l logr.Logger, updateItems []*corev1.Pod, template *corev1.PodTemplateSpec) error {
	log := l.WithName("updatePods")

	var artificialPod corev1.Pod
	artificialPod.Annotations = template.Annotations
	artificialPod.Spec = template.Spec

	volumes := make(map[string]interface{})

	for i := range template.Spec.Volumes {
		vol := template.Spec.Volumes[i]
		volumes[vol.Name] = new(interface{})
	}
	for i := range updateItems {
		pod := updateItems[i]

		if err := r.podEviction(ctx, pod, &artificialPod, volumes); err != nil {
			log.V(logger.DebugLevel).Error(err, "podEviction", "pod name", pod.Name)
			return err
		}

		if !utils.IsPodReady(pod) {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("wait for %s pod to become ready", pod.Name),
			}
		}

		admin, err := r.getAdminAPIClient(ctx)
		if err != nil {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("error getting admin client: %v", err),
			}
		}
		health, err := admin.GetHealthOverview(ctx)
		if err != nil {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("error getting health overview: %v", err),
			}
		}
		if !health.IsHealthy {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          "wait for cluster to become healthy",
			}
		}

		headlessServiceWithPort := fmt.Sprintf("%s:%d", r.serviceFQDN,
			r.pandaCluster.AdminAPIInternal().Port)

		adminURL := url.URL{
			Scheme: "http",
			Host:   hostOverwrite(pod, headlessServiceWithPort),
			Path:   "metrics",
		}

		params := url.Values{}
		if featuregates.MetricsQueryParamName(r.pandaCluster.Spec.Version) {
			params.Add("__name__", "cluster_partition_under_replicated_replicas*")
		} else {
			params.Add("name", "cluster_partition_under_replicated_replicas*")
		}
		adminURL.RawQuery = params.Encode()

		if err = r.evaluateUnderReplicatedPartitions(ctx, &adminURL); err != nil {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("broker reported under replicated partitions: %v", err),
			}
		}
	}

	log.V(logger.DebugLevel).Info("rollingUpdate completed")
	return nil
}

func (r *StatefulSetResource) listPodsForUpdateOrMaintenance(l logr.Logger, podList *corev1.PodList) (updateItems, maintenanceItems []*corev1.Pod) {
	log := l.WithName("listPodsForUpdateOrMaintenance")

	// only roll pods marked with the ClusterUpdate condition
	for i := range podList.Items {
		pod := &podList.Items[i]
		if utils.IsStatusPodConditionTrue(pod.Status.Conditions, ClusterUpdatePodCondition) {
			log.V(logger.DebugLevel).Info("pod needs updated", "pod name", pod.GetName())
			updateItems = append(updateItems, pod)
		} else {
			log.V(logger.DebugLevel).Info("pod needs maintenance check", "pod name", pod.GetName())
			maintenanceItems = append(maintenanceItems, pod)
		}
	}
	return updateItems, maintenanceItems
}

func (r *StatefulSetResource) checkMaintenanceModeForPods(ctx context.Context, maintenanceItems []*corev1.Pod) error {
	for i := range maintenanceItems {
		pod := maintenanceItems[i]
		if err := r.checkMaintenanceMode(ctx, pod.Name); err != nil {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("checking maintenance node %q: %v", pod.GetName(), err),
			}
		}
	}
	return nil
}

var UnderReplicatedPartitionsHostOverwrite string

func hostOverwrite(pod *corev1.Pod, headlessServiceWithPort string) string {
	if UnderReplicatedPartitionsHostOverwrite != "" {
		return UnderReplicatedPartitionsHostOverwrite
	}
	return fmt.Sprintf("%s.%s", pod.Name, headlessServiceWithPort)
}

func (r *StatefulSetResource) podEviction(ctx context.Context, pod, artificialPod *corev1.Pod, newVolumes map[string]interface{}) error {
	log := r.logger.WithName("podEviction").WithValues("pod", pod.GetName(), "namespace", pod.GetNamespace())
	opts := []patch.CalculateOption{
		patch.IgnoreStatusFields(),
		ignoreKubernetesTokenVolumeMounts(),
		ignoreDefaultToleration(),
		ignoreExistingVolumes(newVolumes),
	}

	if pod.DeletionTimestamp != nil {
		return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "waiting for pod to be deleted"}
	}

	managedDecommission, err := r.IsManagedDecommission()
	if err != nil {
		log.Error(err, "not performing a managed decommission")
	}

	patchResult, err := patch.NewPatchMaker(patch.NewAnnotator(redpandaAnnotatorKey), &patch.K8sStrategicMergePatcher{}, &patch.BaseJSONMergePatcher{}).Calculate(pod, artificialPod, opts...)
	if err != nil {
		return err
	}

	if !managedDecommission && patchResult.IsEmpty() {
		podPatch := k8sclient.MergeFrom(pod.DeepCopy())
		utils.RemoveStatusPodCondition(&pod.Status.Conditions, ClusterUpdatePodCondition)
		if err = r.Client.Status().Patch(ctx, pod, podPatch); err != nil {
			return fmt.Errorf("error removing pod update condition: %w", err)
		}
		return nil
	}

	var ordinal int32
	ordinal, err = utils.GetPodOrdinal(pod.Name, r.pandaCluster.Name)
	if err != nil {
		return fmt.Errorf("cluster %s: cannot convert pod name (%s) to ordinal: %w", r.pandaCluster.Name, pod.Name, err)
	}

	if *r.nodePool.Replicas == 1 {
		log.Info("Changes in Pod definition other than activeDeadlineSeconds, configurator and Redpanda container name. Deleting pod",
			"pod-name", pod.Name,
			"patch", patchResult.Patch)

		if err = r.Delete(ctx, pod); err != nil {
			return fmt.Errorf("unable to remove Redpanda pod: %w", err)
		}
		return nil
	}
	if managedDecommission {
		log.Info("managed decommission is set: decommission broker")
		var id *int32
		if id, err = r.getBrokerIDForPod(ctx, ordinal); err != nil {
			return fmt.Errorf("cannot get broker id for pod: %w", err)
		}
		r.pandaCluster.SetDecommissionBrokerID(id)

		if err = r.handleDecommission(ctx, log); err != nil {
			return err
		}

		if err = utils.DeletePodPVCs(ctx, r.Client, pod, log); err != nil {
			return fmt.Errorf(`unable to remove PVCs for pod "%s/%s: %w"`, pod.GetNamespace(), pod.GetName(), err)
		}

		log.Info("deleting pod")
		if err = r.Delete(ctx, pod); err != nil {
			return fmt.Errorf("unable to remove Redpanda pod: %w", err)
		}

		return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "wait for pod restart"}
	}

	log.Info("Put broker into maintenance mode", "patch", patchResult.Patch)
	if err = r.putInMaintenanceMode(ctx, pod.Name); err != nil {
		// As maintenance mode can not be easily watched using controller runtime the requeue error
		// is always returned. That way a rolling update will not finish when operator waits for
		// maintenance mode finished.
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          fmt.Sprintf("putting node (%s) into maintenance mode: %v", pod.Name, err),
		}
	}
	log.Info("Changes in Pod definition other than activeDeadlineSeconds, configurator and Redpanda container name. Deleting pod",
		"patch", patchResult.Patch)

	if err = r.Delete(ctx, pod); err != nil {
		return fmt.Errorf("unable to remove Redpanda pod: %w", err)
	}

	return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "wait for pod restart"}
}

var (
	ErrMaintenanceNotFinished = errors.New("maintenance mode is not finished")
	ErrMaintenanceMissing     = errors.New("maintenance definition not returned")
)

func (r *StatefulSetResource) putInMaintenanceMode(ctx context.Context, pod string) error {
	adminAPIClient, err := r.getAdminAPIClient(ctx, pod)
	if err != nil {
		return fmt.Errorf("creating admin API client: %w", err)
	}

	nodeConf, err := adminAPIClient.GetNodeConfig(ctx)
	if err != nil {
		return fmt.Errorf("getting node config: %w", err)
	}

	err = adminAPIClient.EnableMaintenanceMode(ctx, nodeConf.NodeID)
	if err != nil {
		return fmt.Errorf("enabling maintenance mode: %w", err)
	}

	br, err := adminAPIClient.Broker(ctx, nodeConf.NodeID)
	if err != nil {
		return fmt.Errorf("getting broker information: %w", err)
	}

	if br.Maintenance == nil {
		return ErrMaintenanceMissing
	}

	if !ptr.Deref(br.Maintenance.Finished, false) {
		return fmt.Errorf("draining (%t), errors (%t), failed (%d), finished (%t): %w", br.Maintenance.Draining, ptr.Deref(br.Maintenance.Errors, false), br.Maintenance.Failed, ptr.Deref(br.Maintenance.Finished, false), ErrMaintenanceNotFinished)
	}

	return nil
}

func (r *StatefulSetResource) checkMaintenanceMode(ctx context.Context, pod string) error {
	if r.pandaCluster.GetDesiredReplicas() <= 1 {
		return nil
	}

	// Remove maint. mode for in-decom nodes - these may fail to get decom'd otherwise.
	if err := r.disableMaintenanceModeOnDecommissionedNodes(ctx); err != nil {
		return err
	}

	adminAPIClient, err := r.getAdminAPIClient(ctx, pod)
	if err != nil {
		return fmt.Errorf("creating admin API client: %w", err)
	}

	nodeConf, err := adminAPIClient.GetNodeConfig(ctx)
	if err != nil {
		return fmt.Errorf("getting node config: %w", err)
	}

	br, err := adminAPIClient.Broker(ctx, nodeConf.NodeID)
	if err != nil {
		if e := new(rpadmin.HTTPResponseError); errors.As(err, &e) && e.Response.StatusCode == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("getting broker info: %w", err)
	}

	if br.Maintenance != nil && br.Maintenance.Draining {
		r.logger.Info("Disable broker maintenance", "pod-ordinal", pod)
		err = adminAPIClient.DisableMaintenanceMode(ctx, nodeConf.NodeID, false)
		if err != nil {
			return fmt.Errorf("disabling maintenance mode: %w", err)
		}
	}

	return nil
}

func (r *StatefulSetResource) updateStatefulSet(
	ctx context.Context,
	current *appsv1.StatefulSet,
	modified *appsv1.StatefulSet,
) error {
	log := r.logger.WithName("StatefulSetResource.updateStatefulSet")
	_, err := Update(ctx, current, modified, r.Client, r.logger)
	if err != nil && strings.Contains(err.Error(), "spec: Forbidden: updates to statefulset spec for fields other than") {
		// REF: https://github.com/kubernetes/kubernetes/issues/69041#issuecomment-723757166
		// https://www.giffgaff.io/tech/resizing-statefulset-persistent-volumes-with-zero-downtime
		// in-place rolling update of a pod - https://github.com/kubernetes/kubernetes/issues/9043
		log.Info("forbidden change to statefulset. deleting StatefulSet orphaning pods", "StatefulSet", current.GetName())
		orphan := metav1.DeletePropagationOrphan
		err = r.Client.Delete(ctx, current, &k8sclient.DeleteOptions{
			PropagationPolicy: &orphan,
		})
		if err != nil {
			return fmt.Errorf("unable to delete statefulset using orphan propagation policy: %w", err)
		}
		log.Info("StatefulSet has been deleted, requeueing to allow it to be recreated.", "StatefulSet", current.GetName())
		return &RequeueAfterError{RequeueAfter: RequeueDuration, Msg: "wait for sts to be deleted"}
	}
	if err != nil {
		return fmt.Errorf("unable to update statefulset: %w", err)
	}
	log.Info("StatefulSet has been updated.", "StatefulSet", current.GetName())
	return nil
}

// shouldUpdate returns true if changes on the CR require update
func (r *StatefulSetResource) shouldUpdate(
	current, modified *appsv1.StatefulSet,
) (bool, error) {
	log := r.logger.WithName("shouldUpdate")
	managedDecommission, err := r.IsManagedDecommission()
	if err != nil {
		log.Error(err, "isManagedDecommission")
	}

	npStatus := r.getNodePoolStatus()

	if managedDecommission || npStatus.Restarting || r.pandaCluster.Status.Restarting {
		return true, nil
	}
	prepareResourceForPatch(current, modified)
	opts := []patch.CalculateOption{
		patch.IgnoreStatusFields(),
		patch.IgnoreVolumeClaimTemplateTypeMetaAndStatus(),
		utils.IgnoreAnnotation(redpandaAnnotatorKey),
		utils.IgnoreAnnotation(CentralizedConfigurationHashAnnotationKey),
	}
	patchResult, err := patch.NewPatchMaker(patch.NewAnnotator(redpandaAnnotatorKey), &patch.K8sStrategicMergePatcher{}, &patch.BaseJSONMergePatcher{}).Calculate(current, modified, opts...)
	if err != nil || patchResult.IsEmpty() {
		return false, err
	}
	log.Info("Detected diff", "patchResult", string(patchResult.Patch))
	return true, nil
}

func (r *StatefulSetResource) getRestartingStatus() bool {
	return r.pandaCluster.Status.IsRestarting()
}

func (r *StatefulSetResource) updateRestartingStatus(ctx context.Context, restarting bool) error {
	npStatus := r.getNodePoolStatus()

	if !reflect.DeepEqual(restarting, npStatus.Restarting) {
		npStatus.Restarting = restarting
		// Since NodePools is map-to-struct (non pointer) we need to set the entire struct.
		r.pandaCluster.Status.NodePools[r.nodePool.Name] = npStatus

		// If any NP is restarting, flag the entire cluster as restarting.
		var globalRestarting bool
		for _, np := range r.pandaCluster.Status.NodePools {
			globalRestarting = globalRestarting || np.Restarting
		}
		r.pandaCluster.Status.SetRestarting(globalRestarting)

		r.logger.Info("Status updated",
			"nodePool", r.nodePool.Name,
			"restarting", restarting,
			"resource name", r.pandaCluster.Name)
		if err := r.Status().Update(ctx, r.pandaCluster); err != nil {
			return err
		}
	}

	return nil
}

func (r *StatefulSetResource) removeManagedDecommissionAnnotation(ctx context.Context) error {
	p := k8sclient.MergeFrom(r.pandaCluster.DeepCopy())
	for k := range r.pandaCluster.Annotations {
		if k == ManagedDecommissionAnnotation {
			delete(r.pandaCluster.Annotations, k)
		}
	}
	if err := r.Patch(ctx, r.pandaCluster, p); err != nil {
		return fmt.Errorf("unable to remove managed decommission annotation from Cluster %q: %w", r.pandaCluster.Name, err)
	}
	return nil
}

func ignoreExistingVolumes(
	volumes map[string]interface{},
) patch.CalculateOption {
	return func(current, modified []byte) ([]byte, []byte, error) {
		current, err := deleteExistingVolumes(current, volumes)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from current byte sequence: %w", err)
		}

		modified, err = deleteExistingVolumes(modified, volumes)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from modified byte sequence: %w", err)
		}

		return current, modified, nil
	}
}

func deleteExistingVolumes(
	obj []byte, volumes map[string]interface{},
) ([]byte, error) {
	var pod corev1.Pod
	err := json.Unmarshal(obj, &pod)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal byte sequence: %w", err)
	}

	var newVolumes []corev1.Volume
	for i := range pod.Spec.Volumes {
		_, ok := volumes[pod.Spec.Volumes[i].Name]
		if !ok {
			newVolumes = append(newVolumes, pod.Spec.Volumes[i])
		}
	}
	pod.Spec.Volumes = newVolumes

	obj, err = json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("could not marshal byte sequence: %w", err)
	}

	return obj, nil
}

func ignoreDefaultToleration() patch.CalculateOption {
	return func(current, modified []byte) ([]byte, []byte, error) {
		current, err := deleteDefaultToleration(current)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from current byte sequence: %w", err)
		}

		modified, err = deleteDefaultToleration(modified)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from modified byte sequence: %w", err)
		}

		return current, modified, nil
	}
}

func deleteDefaultToleration(obj []byte) ([]byte, error) {
	var pod corev1.Pod
	err := json.Unmarshal(obj, &pod)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal byte sequence: %w", err)
	}

	var newToleration []corev1.Toleration
	tolerations := pod.Spec.Tolerations
	for i := range tolerations {
		switch tolerations[i].Key {
		case "node.kubernetes.io/not-ready":
		case "node.kubernetes.io/unreachable":
			continue
		default:
			newToleration = append(newToleration, tolerations[i])
		}
	}
	pod.Spec.Tolerations = newToleration

	obj, err = json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("could not marshal byte sequence: %w", err)
	}

	return obj, nil
}

func ignoreKubernetesTokenVolumeMounts() patch.CalculateOption {
	return func(current, modified []byte) ([]byte, []byte, error) {
		current, err := deleteKubernetesTokenVolumeMounts(current)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from current byte sequence: %w", err)
		}

		modified, err = deleteKubernetesTokenVolumeMounts(modified)
		if err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not delete configurator init container field from modified byte sequence: %w", err)
		}

		return current, modified, nil
	}
}

func deleteKubernetesTokenVolumeMounts(obj []byte) ([]byte, error) {
	var pod corev1.Pod
	err := json.Unmarshal(obj, &pod)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal byte sequence: %w", err)
	}

	tokenVolumeName := fmt.Sprintf("%s-token-", pod.Name)
	containers := pod.Spec.Containers
	for i := range containers {
		c := containers[i]
		for j := range c.VolumeMounts {
			vol := c.VolumeMounts[j]
			if strings.HasPrefix(vol.Name, tokenVolumeName) {
				c.VolumeMounts = append(c.VolumeMounts[:j], c.VolumeMounts[j+1:]...)
			}
		}
	}

	initContainers := pod.Spec.InitContainers
	for i := range initContainers {
		ic := initContainers[i]
		for j := range ic.VolumeMounts {
			vol := ic.VolumeMounts[j]
			if strings.HasPrefix(vol.Name, tokenVolumeName) {
				ic.VolumeMounts = append(ic.VolumeMounts[:j], ic.VolumeMounts[j+1:]...)
			}
		}
	}

	obj, err = json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("could not marshal byte sequence: %w", err)
	}

	return obj, nil
}

// Temporarily using the status/ready endpoint until we have a specific one for restarting.
func (r *StatefulSetResource) evaluateUnderReplicatedPartitions(
	ctx context.Context, adminURL *url.URL,
) error {
	client := &http.Client{Timeout: r.metricsTimeout}

	// TODO right now we support TLS only on one listener so if external
	// connectivity is enabled, TLS is enabled only on external listener. This
	// will be fixed by https://github.com/redpanda-data/redpanda/issues/1084
	if r.pandaCluster.AdminAPITLS() != nil &&
		r.pandaCluster.AdminAPIExternal() == nil {
		tlsConfig, err := r.adminTLSConfigProvider.GetTLSConfig(ctx, r)
		if err != nil {
			return err
		}

		client.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		adminURL.Scheme = "https"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminURL.String(), http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			r.logger.Error(err, "error closing connection to Redpanda admin API")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("getting broker metrics (%s): %w", adminURL.String(), errRedpandaNotReady)
	}

	var parser expfmt.TextParser
	metrics, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return err
	}

	for name, metricFamily := range metrics {
		if name != "vectorized_cluster_partition_under_replicated_replicas" {
			continue
		}

		for _, m := range metricFamily.Metric {
			if m == nil {
				continue
			}
			if m.Gauge == nil {
				r.logger.Info("cluster_partition_under_replicated_replicas metric does not have value", "labels", m.Label)
				continue
			}

			var namespace, partition, shard, topic string
			for _, l := range m.Label {
				switch *l.Name {
				case "namespace":
					namespace = *l.Value
				case "partition":
					partition = *l.Value
				case "shard":
					shard = *l.Value
				case "topic":
					topic = *l.Value
				}
			}

			if r.pandaCluster.Spec.RestartConfig != nil && *m.Gauge.Value > float64(r.pandaCluster.Spec.RestartConfig.UnderReplicatedPartitionThreshold) {
				return fmt.Errorf("in topic (%s), partition (%s), shard (%s), namespace (%s): %w", topic, partition, shard, namespace, errUnderReplicatedPartition)
			}
		}
	}

	return nil
}

// RequeueAfterError error carrying the time after which to requeue.
type RequeueAfterError struct {
	RequeueAfter time.Duration
	Msg          string
}

func (e *RequeueAfterError) Error() string {
	return fmt.Sprintf("RequeueAfterError %s", e.Msg)
}

func (e *RequeueAfterError) Is(target error) bool {
	return e.Error() == target.Error()
}

// RequeueError error to requeue using default retry backoff.
type RequeueError struct {
	Msg string
}

func (e *RequeueError) Error() string {
	return fmt.Sprintf("RequeueError %s", e.Msg)
}
