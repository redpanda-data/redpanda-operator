// Package pvcunbinder implements a reconciler that allows StatefulSet Pods to
// reschedule in the event of a lost HostPath PersistentVolume.
package pvcunbinder

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var schedulingFailureRE = regexp.MustCompile(`(^0/[1-9]\d* nodes are available)|(volume node affinity)`)

// Reconciler is a Kubernetes Reconciler that watches for Pods stuck in a
// Pending state due to volume affinities and attempts a remediation.
//
// It watches for Pod events rather than Node events because:
//  1. Node Deletion events could be missed if the operator is scheduled on the node that's died
//  2. We don't want to re-implement label matching. In theory, it should be
//     easy but it's quite risky and behaviors could diverge between Kubernetes
//     versions.
//
// To get the Pod to reschedule we:
//  1. Find all PVs and PVCs associated with our Pod.
//  2. Ensure that all PVs in question have a Retain policy
//  3. Delete all PVCs from step 1. (PVCs are immutable after creation,
//     deletion is the only option)
//  4. "Recycle" all PVs from step 1 by clearing the ClaimRef.
//     Kubernetes will only consider binding PVs that have a satisfiable
//     NodeAffinity. By "recycling" we permit Flakey Nodes to rejoin the cluster
//     which _might_ reclaim the now freed volume.
//  5. Deleting the Pod to re-trigger PVC creation and rebinding.
type Reconciler struct {
	Client client.Client
	// Timeout is the duration a Pod must be stuck in Pending before
	// remediation is attempted.
	Timeout time.Duration
	// Selector, if specified, will narrow the scope of Pods that this
	// Reconciler will consider for remediation.
	Selector labels.Selector
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&corev1.Pod{}, builder.WithPredicates(predicate.NewPredicateFuncs(pvcUnbinderPredicate))).Complete(r)
}

// Reconcile implements the algorithm described in the docs of [Reconciler]. To
// the best of it's ability, Reconcile is implemented to be idempotent. Due to
// the lack of transactions in Kubernetes/etc and the need to operate across
// many objects, it's quite difficult to guarantee this. The general strategy
// is to fetch a snapshot of the world as early as possible and then rely on
// ResourceVersions to inform us about changes from external actors, in which
// case we'll re-queue.
// TODO use an in memory timeout to prevent complete unbinding of Pods.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("PVCUnbinder")
	ctx = log.IntoContext(ctx, logger)

	var pod corev1.Pod
	if err := r.Client.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if ok, requeueAfter := r.shouldRemediate(ctx, &pod); !ok || requeueAfter > 0 {
		logger.Info("shouldn't remediate Pod; skipping", "name", pod.Name, "ok", ok, "requeue-after", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter, Requeue: ok}, nil
	}

	// NB: We denote PVCs that are deleted as a nil entry within this map. If a
	// PVC is not to be considered, it should be removed from this map.
	pvcByKey := map[client.ObjectKey]*corev1.PersistentVolumeClaim{}

	for _, pvcKey := range StsPVCs(&pod) {
		var pvc corev1.PersistentVolumeClaim
		if err := r.Client.Get(ctx, pvcKey, &pvc); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			pvcByKey[pvcKey] = nil
			continue
		}
		pvcByKey[pvcKey] = &pvc
	}

	// If there are no StatefulSet managed PVCs, there's nothing we can do.
	if len(pvcByKey) == 0 {
		logger.Info("Pod had no detectable StatefulSet PVCs. Skipping.")
		return ctrl.Result{}, nil
	}

	// Nothing can be done to scope this query unless we decide to bind the
	// implementation to rancher's local path provisioner which adds a label we
	// could query against.
	var pvList corev1.PersistentVolumeList
	if err := r.Client.List(ctx, &pvList); err != nil {
		return ctrl.Result{}, err
	}

	// Filter PVs down to ones that are:
	// 1. Bound to a PVC we care about.
	// 2. Have a NodeAffinity (which we assume is the cause of our Pod being in Pending)
	var pvs []*corev1.PersistentVolume
	for i := range pvList.Items {
		pv := &pvList.Items[i]

		if pv.Spec.ClaimRef == nil {
			continue
		}

		key := client.ObjectKey{
			Name:      pv.Spec.ClaimRef.Name,
			Namespace: pv.Spec.ClaimRef.Namespace,
		}

		// Skip over any PVs that aren't bound to one of our targeted PVCs
		if _, ok := pvcByKey[key]; !ok {
			continue
		}

		// Filter out PVCs and PVs that don't have a NodeAffinity or aren't a
		// HostPath/Local volume.
		if pv.Spec.NodeAffinity == nil || (pv.Spec.HostPath == nil && pv.Spec.Local == nil) {
			delete(pvcByKey, key)
			continue
		}

		pvs = append(pvs, pv)
	}

	// 3. Ensure that all PVs have reclaim set to Retain
	for _, pv := range pvs {
		if err := r.ensureRetainPolicy(ctx, pv); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 4. Delete all Bound PVCs
	for key, pvc := range pvcByKey {
		if pvc == nil || pvc.Spec.VolumeName == "" {
			continue
		}

		logger.Info("deleting PVC to re-trigger volume binding", "name", pvc.Name)
		if err := r.Client.Delete(ctx, pvc, &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID:             &pvc.UID,
				ResourceVersion: &pvc.ResourceVersion,
			},
		}); err != nil {
			return ctrl.Result{}, err
		}

		// Indicate that this PVC is now deleted.
		pvcByKey[key] = nil
	}

	// 5. "Recycle" PVs that have been released. Technically optional, this
	// allows disks to rebind if a Node happens to recover.
	for _, pv := range pvs {
		if err := r.recyclePersistentVolume(ctx, pv); err != nil {
			return ctrl.Result{}, err
		}
	}

	missingPVCs := false
	for _, pvc := range pvcByKey {
		if pvc == nil {
			missingPVCs = true
			break
		}
	}

	// 5. Delete the Pod to cause the StatefulSet controller to re-create both
	// the PVCs and the Pod but only if there are missing PVCs.
	if !missingPVCs {
		logger.Info("not deleting Pod; no PVCs were deleted", "name", pod.Name)
		return ctrl.Result{}, nil
	}

	logger.Info("deleting Pod to trigger PVC recreation", "name", pod.Name)
	if err := r.Client.Delete(ctx, &pod, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID:             &pod.UID,
			ResourceVersion: &pod.ResourceVersion,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureRetainPolicy(ctx context.Context, pv *corev1.PersistentVolume) error {
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain {
		return nil
	}

	log.FromContext(ctx).Info("setting reclaim policy to retain", "name", pv.Name)

	patch := client.StrategicMergeFrom(pv.DeepCopy(), &client.MergeFromWithOptimisticLock{})
	pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

// recyclePersistentVolume "recycles" a released PV by clearing it's .ClaimRef
// which makes it available for binding once again.
// This strategy is only valid for volumes that utilize .HostPath.
func (r *Reconciler) recyclePersistentVolume(ctx context.Context, pv *corev1.PersistentVolume) error {
	if pv.Spec.HostPath == nil && pv.Spec.Local == nil {
		return fmt.Errorf("%T must specify .Spec.HostPath or .Spec.Local for recycling: %q", pv, pv.Name)
	}

	// Skip over unbound PVs.
	if pv.Spec.ClaimRef == nil {
		return nil
	}

	log.FromContext(ctx).Info("Releasing PersistentVolume", "name", pv.Name)

	// NB: We explicitly don't use an optimistic lock here as the control plane
	// will likely have updated this PV's Status to indicate that it's now
	// Released.
	patch := client.StrategicMergeFrom(pv.DeepCopy())
	pv.Spec.ClaimRef = nil
	if err := r.Client.Patch(ctx, pv, patch); err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) shouldRemediate(ctx context.Context, pod *corev1.Pod) (bool, time.Duration) {
	if r.Selector != nil && !r.Selector.Matches(labels.Set(pod.Labels)) {
		log.FromContext(ctx).Info("selector not satisfied; skipping", "name", pod.Name, "labels", pod.Labels, "selector", r.Selector.String())
		return false, 0
	}

	idx := slices.IndexFunc(pod.Status.Conditions, func(cond corev1.PodCondition) bool {
		return cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == "Unschedulable"
	})

	// Paranoid check, ensure that the Pod we've fetched still passes our predicate.
	if idx == -1 || !pvcUnbinderPredicate(pod) {
		return false, 0
	}

	cond := pod.Status.Conditions[idx]

	// Short of re-implementing or importing scheduler, this is the best way to
	// detect if a scheduling failure is _likely_ due to volume node affinity
	// conflict. We check for a either an explicit mention of volume node
	// affinity issues OR a message indicating that no nodes within the cluster
	// may host this Pod.
	// As of Kubernetes >1.21.x <=1.28.x (Didn't track down an exact version),
	// volume node affinity conflicts no longer seem to appear in the message,
	// hence the need to check for a much weaker case.
	if !schedulingFailureRE.MatchString(cond.Message) {
		log.FromContext(ctx).Info("scheduling failure does not appear to indicate volume affinity issues; skipping", "name", pod.Name, "condition", cond)
		return false, 0
	}

	if delta := r.Timeout - time.Since(cond.LastTransitionTime.Time); delta > 0 {
		return true, delta
	}

	return true, 0
}

func pvcUnbinderPredicate(obj client.Object) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false
	}

	stsManaged := slices.ContainsFunc(pod.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
		return ref.APIVersion == "apps/v1" && ref.Kind == "StatefulSet" && ptr.Deref(ref.Controller, false)
	})

	isPending := pod.Status.Phase == corev1.PodPending

	return stsManaged && isPending
}

// StsPVCs returns a slice of [client.ObjectKey] of PVCs that are attached to
// this Pod and are determined to be managed by the StatefulSet controller.
func StsPVCs(pod *corev1.Pod) []client.ObjectKey {
	var found []client.ObjectKey
	for i := range pod.Spec.Volumes {
		vol := &pod.Spec.Volumes[i]

		if vol.PersistentVolumeClaim == nil {
			continue
		}

		// Easiest way to tell is if the PVC's name ends with the Pods name.
		if !strings.HasSuffix(vol.PersistentVolumeClaim.ClaimName, pod.Name) {
			continue
		}

		found = append(found, client.ObjectKey{
			Name:      vol.PersistentVolumeClaim.ClaimName,
			Namespace: pod.Namespace,
		})
	}
	return found
}
