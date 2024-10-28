// Copyright 2024 Redpanda Data, Inc.
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
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/component-helpers/storage/volume"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

var sel labels.Selector

func init() {
	nameRequirement, err := labels.NewRequirement(K8sNameLabelKey, selection.Exists, []string{})
	if err != nil {
		panic(err)
	}

	instanceRequirement, err := labels.NewRequirement(K8sInstanceLabelKey, selection.Exists, []string{})
	if err != nil {
		panic(err)
	}

	sel = labels.NewSelector()
	sel.Add(*nameRequirement)
	sel.Add(*instanceRequirement)
}

// +kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=redpandas,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,namespace=default,resources=persistentvolumeclaims,verbs=get;list;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// RedpandaNodePVCReconciler watches node objects, and sets annotation to PVC to mark them for deletion
type RedpandaNodePVCReconciler struct {
	client.Client
	OperatorMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaNodePVCReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).WithEventFilter(DeleteEventFilter).Complete(r)
}

func (r *RedpandaNodePVCReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaNodePVCReconciler.Reconcile")

	Infof(log, "Node %q was found to be deleted, checking for existing PVCs", req.Name)

	result, err := r.reconcile(ctx, req)

	// Log reconciliation duration
	durationMsg := fmt.Sprintf("reconciliation finished in %s", time.Since(start).String())
	if result.RequeueAfter > 0 {
		durationMsg = fmt.Sprintf("%s, next run in %s", durationMsg, result.RequeueAfter.String())
	}
	log.Info(durationMsg)

	return result, err
}

// nolint:funlen,unparam // the length is ok
func (r *RedpandaNodePVCReconciler) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaNodePVCReconciler.reconcile")
	Infof(log, "detected node %s deleted; checking if any PVC should be removed", req.Name)

	redpandasMap := map[string]*v1alpha2.Redpanda{}
	if r.OperatorMode {
		opts := &client.ListOptions{Namespace: req.Namespace}
		redpandaList := &v1alpha2.RedpandaList{}
		if errList := r.Client.List(ctx, redpandaList, opts); errList != nil {
			return ctrl.Result{}, fmt.Errorf("could not GET list of Redpandas in namespace: %w", errList)
		}

		for _, rp := range redpandaList.Items {
			redpandasMap[rp.Name] = &rp
		}
	}

	opts := &client.ListOptions{Namespace: req.Namespace, LabelSelector: sel}
	pvcList := &corev1.PersistentVolumeClaimList{}
	if errGET := r.Client.List(ctx, pvcList, opts); errGET != nil {
		return ctrl.Result{}, fmt.Errorf("could not GET list of PVCs: %w", errGET)
	}

	var errs error
	for _, pvc := range pvcList.Items {
		// Now we check if the node where the PVC was originally located was deleted
		// if it is, then we change the PV to change the reclaim policy and then mark the pvc for deletion
		workerNode, ok := pvc.Annotations[volume.AnnSelectedNode]

		if !ok || workerNode == "" {
			errs = errors.Join(errs, fmt.Errorf("worker node annotation not found or node name is empty: %q", workerNode)) //nolint:goerr113 // joining since we do not error here
		}

		// K8S Node label from PVC does not matches delete K8S Node event
		if workerNode != req.Name {
			continue
		}

		val := pvc.Labels[K8sInstanceLabelKey]

		if r.OperatorMode {
			rp, ok := redpandasMap[val]
			if !ok {
				Infof(log, "could not find Redpanda %q", val)
				continue
			}

			// Now check if we are allowed to operate on this pvc as there may be another controller working
			if rp.Spec.ClusterSpec.Statefulset != nil &&
				rp.Spec.ClusterSpec.Statefulset.SideCars != nil &&
				rp.Spec.ClusterSpec.Statefulset.SideCars.Controllers != nil &&
				ptr.Deref(rp.Spec.ClusterSpec.Statefulset.SideCars.Controllers.Enabled, false) {
				Infof(log, "operator can not reconcile as side-car must reconcole node delete event (%q)", req.Name)
				continue
			}
		} else {
			releaseName, ok := os.LookupEnv(EnvHelmReleaseNameKey)
			if !ok {
				Infof(log, "skip persistent volume %q, expected to find release-name env var: %q", pvc.Name, EnvHelmReleaseNameKey)
				return ctrl.Result{}, nil
			}

			if val != releaseName {
				Infof(log, "label %q does not match release name %q. Skipping PVC %q", val, releaseName, pvc.Name)
				return ctrl.Result{}, nil
			}
		}

		// we are being deleted, before moving forward, try to update PV to avoid data loss
		// this is by best effort, if we cannot, then we move on,
		pvName := pvc.Spec.VolumeName
		bestTrySetRetainPV(r.Client, log, ctx, pvName, req.Namespace)

		// now we are ready to delete PVC
		if deleteErr := r.Client.Delete(ctx, &pvc); deleteErr != nil {
			errs = errors.Join(errs, fmt.Errorf("could not delete PVC %q: %w", pvc.Name, deleteErr)) //nolint:goerr113 // joining since we do not error here
		}
	}

	return ctrl.Result{}, errs
}
