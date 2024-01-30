// Copyright 2023 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/utils"
)

const (
	ManagedDecommissionConditionType = "ManagedDecommission"

	decommissionWaitJitterFactor = 0.2

	defaultDecommissionWaitInterval = 10 * time.Second
)

// +kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=redpandas,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,namespace=default,resources=pods,verbs=update;patch;delete;get;list;watch;
// +kubebuilder:rbac:groups=core,namespace=default,resources=pods/status,verbs=update;patch
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;update;patch;delete;watch

type ManagedDecommissionReconciler struct {
	k8sclient.Client
	record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedDecommissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Redpanda{}).Complete(r)
}

func (r *ManagedDecommissionReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("ManagedDecommissionReconciler.Reconcile")

	rp := &v1alpha1.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, k8sclient.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.V(logger.DebugLevel).Info("Redpanda resource is deleted")
		return ctrl.Result{}, nil
	}

	var result ctrl.Result
	var err error

	result, err = managedDecommission(ctx, log, r.Client, r.EventRecorder, rp)

	// Log reconciliation duration
	durationMsg := fmt.Sprintf("succesfull reconciliation finished in %s", time.Since(start).String())
	if result.RequeueAfter > 0 {
		durationMsg = fmt.Sprintf("%s, next run in %s", durationMsg, result.RequeueAfter.String())
	}
	if err != nil {
		durationMsg = fmt.Sprintf("found error, we will requeue, reconciliation attempt finished in %s", time.Since(start).String())
	}

	log.Info(durationMsg)

	return result, err
}

func managedDecommission(ctx context.Context, l logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *v1alpha1.Redpanda) (ctrl.Result, error) {
	log := l.WithName("managedDecommission")

	exist, err := isManagedDecommission(rp)
	if !exist || err != nil {
		cleanupError := cleanUp(ctx, log, c, er, rp)
		if cleanupError != nil {
			return ctrl.Result{}, cleanupError
		}
		return ctrl.Result{}, err
	}

	if err = markPods(ctx, log, c, er, rp); err != nil {
		return ctrl.Result{}, err
	}

	if err = decommissionStatus(ctx, log, c, er, rp); err != nil {
		var requeueErr *resources.RequeueAfterError
		if errors.As(err, &requeueErr) {
			return ctrl.Result{RequeueAfter: requeueErr.RequeueAfter}, nil
		}
		return ctrl.Result{}, fmt.Errorf("managed decommission status: %w", err)
	}

	if err = reconcilePodsDecommission(ctx, log, c, er, rp); err != nil {
		var requeueErr *resources.RequeueAfterError
		if errors.As(err, &requeueErr) {
			log.Info(requeueErr.Error())
			return ctrl.Result{RequeueAfter: requeueErr.RequeueAfter}, nil
		}
		return ctrl.Result{}, fmt.Errorf("managed decommission pod eviction: %w", err)
	}

	ok, err := areAllPodsUpdated(ctx, c, rp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{
			RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
		}, nil
	}

	if err = removeManagedDecommissionAnnotation(ctx, log, c, rp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func cleanUp(ctx context.Context, l logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *v1alpha1.Redpanda) error {
	log := l.WithName("cleanUp")

	err := decommissionStatus(ctx, log, c, er, rp)
	if err != nil {
		return err
	}

	// Clean condition from Pods
	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		return fmt.Errorf("cleanup pods selector: %w", err)
	}
	podList, err := getPodList(ctx, c, rp.Namespace, selector)
	if err != nil {
		return fmt.Errorf("cleanup listing all pods: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		cond := utils.FindStatusPodCondition(pod.Status.Conditions, resources.ClusterUpdatePodCondition)
		if cond != nil {
			podPatch := k8sclient.MergeFrom(pod.DeepCopy())
			utils.RemoveStatusPodCondition(&pod.Status.Conditions, resources.ClusterUpdatePodCondition)
			if err = c.Status().Patch(ctx, pod, podPatch); err != nil {
				return fmt.Errorf("error removing pod update condition: %w", err)
			}
		}
	}

	apimeta.RemoveStatusCondition(rp.GetConditions(), ManagedDecommissionConditionType)

	log.Info("Clean up", "conditions", rp.GetConditions(), "annotations", rp.GetAnnotations())

	return updateRedpanda(ctx, rp, c)
}

func decommissionStatus(ctx context.Context, l logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *v1alpha1.Redpanda) error {
	if rp.Status.ManagedDecommissioningNode == nil {
		return nil
	}

	decommissionNodeID := int(*rp.Status.ManagedDecommissioningNode)
	log := l.WithName("decommissionStatus").WithValues("decommission-node-id", decommissionNodeID)

	valuesMap, err := getHelmValues(l, rp.GetHelmReleaseName(), rp.Namespace)
	if err != nil {
		return fmt.Errorf("get helm values: %w", err)
	}

	requestedReplicas, err := getRedpandaReplicas(ctx, c, rp)
	if err != nil {
		return err
	}

	adminAPI, err := buildAdminAPI(rp.GetHelmReleaseName(), rp.Namespace, int32(requestedReplicas), nil, valuesMap)
	if err != nil {
		return fmt.Errorf("creating AdminAPI: %w", err)
	}

	decomStatus, err := adminAPI.DecommissionBrokerStatus(ctx, decommissionNodeID)
	if err != nil {
		return fmt.Errorf("getting decommission broker status: %w", err)
	}

	if !decomStatus.Finished {
		log.Info("decommission status not finished", "decommission-broker-status", decomStatus)
		return &resources.RequeueAfterError{
			RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          fmt.Sprintf("broker %d decommission status not finished", decommissionNodeID),
		}
	}

	log.Info("Node decommissioned")
	er.AnnotatedEventf(rp,
		map[string]string{v1alpha1.GroupVersion.Group + revisionPath: rp.ResourceVersion},
		corev1.EventTypeNormal, v1alpha1.EventTypeTrace, fmt.Sprintf("Node decommissioned: %d", decommissionNodeID))

	err = podEvict(ctx, log, c, rp)
	if err != nil {
		return err
	}

	rp.Status.ManagedDecommissioningNode = nil

	if err := updateRedpanda(ctx, rp, c); err != nil {
		return err
	}

	// After Pod being evicted the time it needs for PVC and Pod to be deleted can be counted in minutes. That's why
	// default 10 seconds is multiplied by a factor of 6.
	return &resources.RequeueAfterError{
		RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval*6, decommissionWaitJitterFactor),
		Msg:          fmt.Sprintf("broker %d decommission status not finished", decommissionNodeID),
	}
}

var ErrZeroReplicas = errors.New("redpanda replicas is zero")

func getRedpandaReplicas(ctx context.Context, c k8sclient.Client, rp *v1alpha1.Redpanda) (int, error) {
	requestedReplicas := ptr.Deref(rp.Spec.ClusterSpec.Statefulset.Replicas, 0)
	if requestedReplicas == 0 {
		resourcesName := rp.Name
		if rp.Spec.ClusterSpec.FullNameOverride != "" {
			resourcesName = rp.Spec.ClusterSpec.FullNameOverride
		}

		var sts appsv1.StatefulSet
		err := c.Get(ctx, types.NamespacedName{
			Namespace: rp.Namespace,
			Name:      resourcesName,
		}, &sts)
		if err != nil {
			return 0, fmt.Errorf("getting statefulset : %w", err)
		}

		requestedReplicas = int(ptr.Deref(sts.Spec.Replicas, 0))
	}

	if requestedReplicas == 0 {
		return 0, ErrZeroReplicas
	}
	return requestedReplicas, nil
}

func podEvict(ctx context.Context, l logr.Logger, c k8sclient.Client, rp *v1alpha1.Redpanda) error {
	if rp.Status.ManagedDecommissioningNode == nil {
		return nil
	}

	decommissionNodeID := int(*rp.Status.ManagedDecommissioningNode)
	log := l.WithName("podEvict").WithValues("decommission-node-id", decommissionNodeID)

	pod, err := getPodFromRedpandaNodeID(ctx, l, c, rp, decommissionNodeID)
	if err != nil {
		return fmt.Errorf("getting pod from node-id (%d): %w", decommissionNodeID, err)
	}

	log.Info("delete Pod PVC", "pod-name", pod.Name)
	if err = utils.DeletePodPVCs(ctx, c, pod, log); err != nil {
		return fmt.Errorf(`unable to remove VPCs for pod "%s/%s: %w"`, pod.GetNamespace(), pod.GetName(), err)
	}
	return c.Delete(ctx, pod)
}

func getPodFromRedpandaNodeID(ctx context.Context, l logr.Logger, c k8sclient.Client, rp *v1alpha1.Redpanda, nodeID int) (*corev1.Pod, error) {
	log := l.WithName("getPodFromRedpandaNodeID")

	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		return nil, fmt.Errorf("get Redpanda Node ID pod selector: %w", err)
	}
	pl, err := getPodList(ctx, c, rp.Namespace, selector)
	if err != nil {
		return nil, fmt.Errorf("get Redpanda Node ID pod list: %w", err)
	}

	valuesMap, err := getHelmValues(l, rp.GetHelmReleaseName(), rp.Namespace)
	if err != nil {
		return nil, fmt.Errorf("get helm values: %w", err)
	}

	for i := range pl.Items {
		pod := pl.Items[i]

		if !utils.IsPodReady(&pod) {
			log.Info("pod is not ready",
				"containersReady", utils.IsStatusPodConditionTrue(pod.Status.Conditions, corev1.ContainersReady),
				"podReady", utils.IsStatusPodConditionTrue(pod.Status.Conditions, corev1.PodReady),
				"pod-phase", pod.Status.Phase,
				"pod-name", pod.Name)
			return nil, &resources.RequeueAfterError{
				RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
				Msg:          fmt.Sprintf("pod (%s) is not ready", pod.Name),
			}
		}

		var ordinal int32
		ordinal, err := utils.GetPodOrdinal(pod.Name, pod.GenerateName[:len(pod.GenerateName)-1])
		if err != nil {
			return nil, err
		}
		podOrdinal := int(ordinal)

		singleNodeAdminAPI, err := buildAdminAPI(rp.GetHelmReleaseName(), rp.Namespace, 0, &podOrdinal, valuesMap)
		if err != nil {
			return nil, fmt.Errorf("creating single node AdminAPI for pod (%d): %w", podOrdinal, err)
		}

		nodeCfg, err := singleNodeAdminAPI.GetNodeConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting node configuration from pod (%d): %w", podOrdinal, err)
		}

		if nodeCfg.NodeID == nodeID {
			return &pod, nil
		}
	}

	return nil, nil
}

func updateRedpanda(ctx context.Context, rp *v1alpha1.Redpanda, c k8sclient.Client) error {
	key := k8sclient.ObjectKeyFromObject(rp)
	latest := &v1alpha1.Redpanda{}
	if err := c.Get(ctx, key, latest); err != nil {
		return fmt.Errorf("getting latest Redpanda resource: %w", err)
	}

	err := c.Status().Patch(ctx, rp, k8sclient.MergeFrom(latest))
	if err != nil {
		return fmt.Errorf("patching Redpanda status: %w", err)
	}
	return nil
}

func reconcilePodsDecommission(ctx context.Context, l logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *v1alpha1.Redpanda) error {
	log := l.WithName("reconcilePodsDecommission")
	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		return fmt.Errorf("reconcile decommission pod selector: %w", err)
	}
	pl, err := getPodList(ctx, c, rp.Namespace, selector)
	if err != nil {
		return err
	}

	valuesMap, err := getHelmValues(l, rp.GetHelmReleaseName(), rp.Namespace)
	if err != nil {
		return fmt.Errorf("get helm values: %w", err)
	}

	for i := range pl.Items {
		pod := pl.Items[i]

		if !utils.IsPodReady(&pod) {
			log.Info("pod is not ready",
				"containersReady", utils.IsStatusPodConditionTrue(pod.Status.Conditions, corev1.ContainersReady),
				"podReady", utils.IsStatusPodConditionTrue(pod.Status.Conditions, corev1.PodReady),
				"pod-phase", pod.Status.Phase,
				"pod-name", pod.Name)
			return &resources.RequeueAfterError{
				RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
				Msg:          "pod is not ready",
			}
		}

		cond := utils.FindStatusPodCondition(pod.Status.Conditions, resources.ClusterUpdatePodCondition)
		if cond == nil {
			log.V(logger.DebugLevel).Info("pod does not have cluster update pod condition", "pod-name", pod.Name)
			continue
		}

		var ordinal int32
		ordinal, err := utils.GetPodOrdinal(pod.Name, pod.GenerateName[:len(pod.GenerateName)-1])
		if err != nil {
			return err
		}
		podOrdinal := int(ordinal)

		singleNodeAdminAPI, err := buildAdminAPI(rp.GetHelmReleaseName(), rp.Namespace, 0, &podOrdinal, valuesMap)
		if err != nil {
			return fmt.Errorf("creating single node AdminAPI for pod (%d): %w", podOrdinal, err)
		}

		nodeCfg, err := singleNodeAdminAPI.GetNodeConfig(ctx)
		if err != nil {
			return fmt.Errorf("getting node configuration from pod (%d): %w", podOrdinal, err)
		}

		log.Info("Node decommissioning started", "pod-name", pod.Name, "node-id", nodeCfg.NodeID)

		err = singleNodeAdminAPI.DecommissionBroker(ctx, nodeCfg.NodeID)
		if err != nil {
			return fmt.Errorf("error while trying to decommission node %d: %w", nodeCfg.NodeID, err)
		}

		er.AnnotatedEventf(rp,
			map[string]string{v1alpha1.GroupVersion.Group + revisionPath: rp.ResourceVersion},
			corev1.EventTypeNormal, v1alpha1.EventTypeTrace, fmt.Sprintf("Node decommissioning started: %d", nodeCfg.NodeID))

		nodeID := int32(nodeCfg.NodeID)
		rp.Status.ManagedDecommissioningNode = &nodeID

		err = updateRedpanda(ctx, rp, c)
		if err != nil {
			return err
		}

		return &resources.RequeueAfterError{
			RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          fmt.Sprintf("waiting for broker %d to be decommissioned", nodeCfg.NodeID),
		}
	}

	return nil
}

func areAllPodsUpdated(ctx context.Context, c k8sclient.Client, rp *v1alpha1.Redpanda) (bool, error) {
	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		return false, fmt.Errorf("selector for pods condition confirmation: %w", err)
	}
	podList, err := getPodList(ctx, c, rp.Namespace, selector)
	if err != nil {
		return false, fmt.Errorf("check all pods have condition: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		cond := utils.FindStatusPodCondition(pod.Status.Conditions, resources.ClusterUpdatePodCondition)
		if cond != nil && cond.Status == corev1.ConditionTrue {
			return false, nil
		}
	}
	return true, nil
}

func removeManagedDecommissionAnnotation(ctx context.Context, log logr.Logger, c k8sclient.Client, rp *v1alpha1.Redpanda) error {
	l := log.WithName("removeManagedDecommissionAnnotation")

	key := k8sclient.ObjectKeyFromObject(rp)
	latest := &v1alpha1.Redpanda{}
	if err := c.Get(ctx, key, latest); err != nil {
		return fmt.Errorf("getting latest Redpanda resource: %w", err)
	}

	p := k8sclient.MergeFrom(latest)
	for k := range rp.Annotations {
		if k == resources.ManagedDecommissionAnnotation {
			delete(rp.Annotations, k)
		}
	}

	l.Info("Managed decommission finished", "annotations", rp.Annotations, "conditions", rp.GetConditions())

	if err := c.Patch(ctx, rp, p); err != nil {
		return fmt.Errorf("unable to remove managed decommission annotation from Cluster %q: %w", rp.Name, err)
	}

	return nil
}

func markPods(ctx context.Context, log logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *v1alpha1.Redpanda) error {
	if hasManagedCondition(rp) {
		log.V(logger.DebugLevel).Info("Redpanda has managed decommission condition")
		return nil
	}

	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		return fmt.Errorf("marking Redpanda condition pod selector: %w", err)
	}
	pl, err := getPodList(ctx, c, rp.Namespace, selector)
	if err != nil {
		return fmt.Errorf("marking Redpanda condition pod list: %w", err)
	}

	for i := range pl.Items {
		pod := pl.Items[i]
		podPatch := k8sclient.MergeFrom(pod.DeepCopy())

		newCondition := corev1.PodCondition{
			Type:    resources.ClusterUpdatePodCondition,
			Status:  corev1.ConditionTrue,
			Message: "Managed decommission",
		}
		utils.SetStatusPodCondition(&pod.Status.Conditions, &newCondition)

		if err := c.Status().Patch(ctx, &pod, podPatch); err != nil {
			return fmt.Errorf("setting managed decommission pod condition: %w", err)
		}
	}

	log.V(logger.DebugLevel).Info("Redpanda managed decommission started", "conditions", rp.GetConditions())
	er.AnnotatedEventf(rp,
		map[string]string{v1alpha1.GroupVersion.Group + revisionPath: rp.ResourceVersion},
		corev1.EventTypeNormal, v1alpha1.EventTypeTrace, "Managed Decommission started")

	newCondition := metav1.Condition{
		Type:    ManagedDecommissionConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedDecommissionStarted",
		Message: "Managed Decommission started",
	}
	apimeta.SetStatusCondition(rp.GetConditions(), newCondition)

	return updateRedpanda(ctx, rp, c)
}

func hasManagedCondition(rp *v1alpha1.Redpanda) bool {
	conds := rp.GetConditions()
	if conds == nil {
		return false
	}

	for _, c := range *conds {
		if c.Type == ManagedDecommissionConditionType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func isManagedDecommission(rp *v1alpha1.Redpanda) (bool, error) {
	t, ok := rp.GetAnnotations()[resources.ManagedDecommissionAnnotation]
	if !ok {
		return false, nil
	}
	deadline, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return false, fmt.Errorf("managed decommission annotation must be a valid RFC3339 timestamp: %w", err)
	}
	return deadline.After(time.Now()), nil
}

func getPodList(ctx context.Context, c k8sclient.Client, namespace string, ls labels.Selector) (*corev1.PodList, error) {
	var podList corev1.PodList
	err := c.List(ctx, &podList, &k8sclient.ListOptions{
		Namespace:     namespace,
		LabelSelector: ls,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list pods: %w", err)
	}

	return sortPodList(&podList), nil
}

func sortPodList(podList *corev1.PodList) *corev1.PodList {
	sort.Slice(podList.Items, func(i, j int) bool {
		iOrdinal, _ := utils.GetPodOrdinal(podList.Items[i].GetName(), podList.Items[i].GenerateName[:len(podList.Items[i].GenerateName)-1])
		jOrdinal, _ := utils.GetPodOrdinal(podList.Items[j].GetName(), podList.Items[i].GenerateName[:len(podList.Items[i].GenerateName)-1])

		return iOrdinal < jOrdinal
	})
	return podList
}
