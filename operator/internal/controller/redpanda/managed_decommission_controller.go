// Copyright 2025 Redpanda Data, Inc.
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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/go-logr/logr"
	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

const (
	ManagedDecommissionConditionType = "ManagedDecommission"

	decommissionWaitJitterFactor = 0.2

	defaultDecommissionWaitInterval = 10 * time.Second
)

var ErrZeroReplicas = errors.New("redpanda replicas is zero")

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,namespace=default,resources=pods,verbs=update;patch;delete;get;list;watch;
// +kubebuilder:rbac:groups=core,namespace=default,resources=pods/status,verbs=update;patch
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;update;patch;delete;watch

type ManagedDecommissionReconciler struct {
	k8sclient.Client
	record.EventRecorder
	ClientFactory internalclient.ClientFactory
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedDecommissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Redpanda{}).Complete(r)
}

func (r *ManagedDecommissionReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("ManagedDecommissionReconciler.Reconcile")

	rp := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, k8sclient.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		log.V(logger.DebugLevel).Info("Redpanda resource is deleted")
		return ctrl.Result{}, nil
	}

	result, err := r.managedDecommission(ctx, rp)

	// Log reconciliation duration
	durationMsg := fmt.Sprintf("successful reconciliation finished in %s", time.Since(start).String())
	if result.RequeueAfter > 0 {
		durationMsg = fmt.Sprintf("%s, next run in %s", durationMsg, result.RequeueAfter.String())
	}
	if err != nil {
		durationMsg = fmt.Sprintf("found error, we will requeue, reconciliation attempt finished in %s", time.Since(start).String())
	}

	log.Info(durationMsg)

	return result, err
}

func (r *ManagedDecommissionReconciler) managedDecommission(ctx context.Context, rp *redpandav1alpha2.Redpanda) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("managedDecommission")

	exist, err := isManagedDecommission(rp)
	if !exist || err != nil {
		cleanupError := r.cleanUp(ctx, rp)
		if cleanupError != nil {
			return ctrl.Result{}, cleanupError
		}
		return ctrl.Result{}, err
	}

	if err := markPods(ctx, log, r.Client, r.EventRecorder, rp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.decommissionStatus(ctx, rp); err != nil {
		var requeueErr *resources.RequeueAfterError
		if errors.As(err, &requeueErr) {
			return ctrl.Result{RequeueAfter: requeueErr.RequeueAfter}, nil
		}
		return ctrl.Result{}, fmt.Errorf("managed decommission status: %w", err)
	}

	if err := r.reconcilePodsDecommission(ctx, rp); err != nil {
		var requeueErr *resources.RequeueAfterError
		if errors.As(err, &requeueErr) {
			log.Info(requeueErr.Error())
			return ctrl.Result{RequeueAfter: requeueErr.RequeueAfter}, nil
		}
		return ctrl.Result{}, fmt.Errorf("managed decommission pod eviction: %w", err)
	}

	ok, err := r.areAllPodsUpdated(ctx, rp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ok {
		return ctrl.Result{
			RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
		}, nil
	}

	if err = removeManagedDecommissionAnnotation(ctx, log, r.Client, rp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ManagedDecommissionReconciler) cleanUp(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx).WithName("cleanUp")

	if err := r.decommissionStatus(ctx, rp); err != nil {
		return err
	}

	// Clean condition from Pods
	podList, err := getPodList(ctx, r.Client, rp.Namespace, redpandaPodsSelector(rp))
	if err != nil {
		return fmt.Errorf("cleanup listing all pods: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		cond := utils.FindStatusPodCondition(pod.Status.Conditions, resources.ClusterUpdatePodCondition)
		if cond != nil {
			podPatch := k8sclient.MergeFrom(pod.DeepCopy())
			utils.RemoveStatusPodCondition(&pod.Status.Conditions, resources.ClusterUpdatePodCondition)
			if err = r.Client.Status().Patch(ctx, pod, podPatch); err != nil {
				return fmt.Errorf("error removing pod update condition: %w", err)
			}
		}
	}

	apimeta.RemoveStatusCondition(rp.GetConditions(), ManagedDecommissionConditionType)

	log.Info("Clean up", "conditions", rp.GetConditions(), "annotations", rp.GetAnnotations())

	return updateRedpanda(ctx, rp, r.Client)
}

func redpandaPodsSelector(rp *redpandav1alpha2.Redpanda) labels.Selector {
	selector, err := labels.Parse(fmt.Sprintf("!job-name,app.kubernetes.io/component=%s,app.kubernetes.io/instance=%s", componentLabelValue, rp.Name))
	if err != nil {
		panic(fmt.Errorf("label selector should always pass parsing: %w", err))
	}
	return selector
}

func (r *ManagedDecommissionReconciler) decommissionStatus(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if rp.Status.ManagedDecommissioningNode == nil {
		return nil
	}

	decommissionNodeID := int(*rp.Status.ManagedDecommissioningNode)
	log := ctrl.LoggerFrom(ctx).WithName("decommissionStatus").WithValues("decommission-node-id", decommissionNodeID)

	adminAPI, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return fmt.Errorf("creating AdminAPI: %w", err)
	}
	defer adminAPI.Close()

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
	r.EventRecorder.AnnotatedEventf(rp,
		map[string]string{redpandav1alpha2.GroupVersion.Group + revisionPath: rp.ResourceVersion},
		corev1.EventTypeNormal, redpandav1alpha2.EventTypeTrace, fmt.Sprintf("Node decommissioned: %d", decommissionNodeID))

	if err := r.podEvict(ctx, rp); err != nil {
		return err
	}

	rp.Status.ManagedDecommissioningNode = nil

	if err := updateRedpanda(ctx, rp, r.Client); err != nil {
		return err
	}

	// After Pod being evicted the time it needs for PVC and Pod to be deleted can be counted in minutes. That's why
	// default 10 seconds is multiplied by a factor of 6.
	return &resources.RequeueAfterError{
		RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval*6, decommissionWaitJitterFactor),
		Msg:          fmt.Sprintf("broker %d decommission status not finished", decommissionNodeID),
	}
}

func (r *ManagedDecommissionReconciler) podEvict(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if rp.Status.ManagedDecommissioningNode == nil {
		return nil
	}

	decommissionNodeID := int(*rp.Status.ManagedDecommissioningNode)
	log := ctrl.LoggerFrom(ctx).WithName("podEvict").WithValues("decommission-node-id", decommissionNodeID)

	pod, err := r.getPodFromRedpandaNodeID(ctx, rp, decommissionNodeID)
	if err != nil {
		return fmt.Errorf("getting pod from node-id (%d): %w", decommissionNodeID, err)
	}

	// NB: Deleting this Pod will take an unexpectedly long time as the
	// pre-stop hook will "spin" due to not handling decommissioned brokers.
	log.Info("delete Pod PVC", "pod-name", pod.Name)
	if err = utils.DeletePodPVCs(ctx, r.Client, pod, log); err != nil {
		return fmt.Errorf(`unable to remove VPCs for pod "%s/%s: %w"`, pod.GetNamespace(), pod.GetName(), err)
	}
	return r.Client.Delete(ctx, pod)
}

func (r *ManagedDecommissionReconciler) getPodFromRedpandaNodeID(ctx context.Context, rp *redpandav1alpha2.Redpanda, nodeID int) (*corev1.Pod, error) {
	log := ctrl.LoggerFrom(ctx).WithName("getPodFromRedpandaNodeID")

	pl, err := getPodList(ctx, r.Client, rp.Namespace, redpandaPodsSelector(rp))
	if err != nil {
		return nil, fmt.Errorf("get Redpanda Node ID pod list: %w", err)
	}

	adminAPI, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer adminAPI.Close()

	brokers, err := adminAPI.Brokers(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if len(brokers) != len(pl.Items)-1 {
		return nil, errors.Newf("expected Pod list to be 1 short of Broker list: %d (brokers) - 1 != %d (pods)", len(brokers), len(pl.Items))
	}

	// Paranoid check to make up for the lack of an establishing a direct
	// adminAPI connection: Assert that the decommissioned nodeID does not
	// appear in the brokers list.
	for _, broker := range brokers {
		if broker.NodeID == nodeID {
			return nil, errors.Newf("unexpectedly found a broker with NodeID %d", nodeID)
		}
	}

	// O(P*B) sadly but both should be fairly small. Being able to construct
	// the entire InternalRPC URL or having a string trie would make this
	// doable in O(P+B).
	var pod *corev1.Pod
	for i, p := range pl.Items {
		_, err := findPodBroker(&p, brokers)
		if err != nil && pod == nil {
			pod = &pl.Items[i]
		} else if err != nil {
			return nil, errors.Newf("found two Pods uncorrelatable to brokers: %q and %q", pod.Name, p.Name)
		}
	}

	if pod == nil {
		return nil, errors.Newf("failed to find Pod for decommissioned broker with Node ID %d", nodeID)
	}

	if !utils.IsPodReady(pod) {
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

	// Ideally, we'd connect directly to the adminAPI of the Broker running on
	// this Pod and verify the it's node ID before proceeding further but we
	// don't currently have a factory to connect to a specific broker.
	// However, at this point we have verified that:
	// 1. This controller is running (Decommissions are in progress and expected)
	// 2. There is exactly 1 Pod that can't be correlated to a broker.
	// 3. The broker with NodeID `nodeID` has been successfully decommissioned.
	// 4. There is no broker associated with `nodeID`.
	// Therefore it's fairly safe to assume `pod` is the broker that was
	// previously associated with `nodeID` and it can be safely evicted.

	return pod, nil
}

func updateRedpanda(ctx context.Context, rp *redpandav1alpha2.Redpanda, c k8sclient.Client) error {
	key := k8sclient.ObjectKeyFromObject(rp)
	latest := &redpandav1alpha2.Redpanda{}
	if err := c.Get(ctx, key, latest); err != nil {
		return fmt.Errorf("getting latest Redpanda resource: %w", err)
	}

	// HACK: Disable optimistic locking. Technically, the correct way to do
	// this is to set both objects' ResourceVersion to "". It's a waste of
	// resources deep copy rp, so we just copy it over.
	latest.ResourceVersion = rp.ResourceVersion
	err := c.Status().Patch(ctx, rp, k8sclient.MergeFrom(latest))
	if err != nil {
		return fmt.Errorf("patching Redpanda status: %w", err)
	}
	return nil
}

func (r *ManagedDecommissionReconciler) reconcilePodsDecommission(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx).WithName("reconcilePodsDecommission")

	pl, err := getPodList(ctx, r.Client, rp.Namespace, redpandaPodsSelector(rp))
	if err != nil {
		return err
	}

	adminAPI, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return err
	}
	defer adminAPI.Close()

	brokers, err := adminAPI.Brokers(ctx)
	if err != nil {
		return err
	}

	// First make sure that all Pods are ready before proceeding with a
	// decommission. This ensures that we wait an appropriate period of time
	// between the decommission finishing and the decommissioned Pod being
	// rescheduled after evictions.
	for _, pod := range pl.Items {
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
	}

	for _, pod := range pl.Items {
		cond := utils.FindStatusPodCondition(pod.Status.Conditions, resources.ClusterUpdatePodCondition)
		if cond == nil {
			log.V(logger.DebugLevel).Info("pod does not have cluster update pod condition", "pod-name", pod.Name)
			continue
		}

		broker, err := findPodBroker(&pod, brokers)
		if err != nil {
			return err
		}

		log.Info("Node decommissioning started", "pod-name", pod.Name, "node-id", broker.NodeID)

		if err := adminAPI.DecommissionBroker(ctx, broker.NodeID); err != nil {
			return errors.Wrapf(err, "error while trying to decommission node %d", broker.NodeID)
		}

		r.EventRecorder.AnnotatedEventf(rp,
			map[string]string{redpandav1alpha2.GroupVersion.Group + revisionPath: rp.ResourceVersion},
			corev1.EventTypeNormal, redpandav1alpha2.EventTypeTrace, fmt.Sprintf("Node decommissioning started: %d", broker.NodeID))

		rp.Status.ManagedDecommissioningNode = ptr.To(int32(broker.NodeID))

		if err := updateRedpanda(ctx, rp, r.Client); err != nil {
			return err
		}

		return &resources.RequeueAfterError{
			RequeueAfter: wait.Jitter(defaultDecommissionWaitInterval, decommissionWaitJitterFactor),
			Msg:          fmt.Sprintf("waiting for broker %d to be decommissioned", broker.NodeID),
		}
	}

	return nil
}

func (r *ManagedDecommissionReconciler) areAllPodsUpdated(ctx context.Context, rp *redpandav1alpha2.Redpanda) (bool, error) {
	podList, err := getPodList(ctx, r.Client, rp.Namespace, redpandaPodsSelector(rp))
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

func removeManagedDecommissionAnnotation(ctx context.Context, log logr.Logger, c k8sclient.Client, rp *redpandav1alpha2.Redpanda) error {
	l := log.WithName("removeManagedDecommissionAnnotation")

	key := k8sclient.ObjectKeyFromObject(rp)
	latest := &redpandav1alpha2.Redpanda{}
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

func markPods(ctx context.Context, log logr.Logger, c k8sclient.Client, er record.EventRecorder, rp *redpandav1alpha2.Redpanda) error {
	if hasManagedCondition(rp) {
		log.V(logger.DebugLevel).Info("Redpanda has managed decommission condition")
		return nil
	}

	pl, err := getPodList(ctx, c, rp.Namespace, redpandaPodsSelector(rp))
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
		map[string]string{redpandav1alpha2.GroupVersion.Group + revisionPath: rp.ResourceVersion},
		corev1.EventTypeNormal, redpandav1alpha2.EventTypeTrace, "Managed Decommission started")

	newCondition := metav1.Condition{
		Type:    ManagedDecommissionConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "ManagedDecommissionStarted",
		Message: "Managed Decommission started",
	}
	apimeta.SetStatusCondition(rp.GetConditions(), newCondition)

	return updateRedpanda(ctx, rp, c)
}

func hasManagedCondition(rp *redpandav1alpha2.Redpanda) bool {
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

func isManagedDecommission(rp *redpandav1alpha2.Redpanda) (bool, error) {
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

func findPodBroker(pod *corev1.Pod, brokers []rpadmin.Broker) (*rpadmin.Broker, error) {
	prefix := strings.Join([]string{pod.Spec.Hostname, pod.Spec.Subdomain}, ".")

	for _, broker := range brokers {
		if strings.HasPrefix(broker.InternalRPCAddress, prefix) {
			return &broker, nil
		}
	}
	return nil, errors.Newf("broker for Pod %q not found (No brokers with %q prefixing InternalRPCAddress)", pod.Name, prefix)
}
