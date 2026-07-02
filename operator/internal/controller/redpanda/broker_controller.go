// Copyright 2026 Redpanda Data, Inc.
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
	"strings"

	"github.com/redpanda-data/common-go/otelutil/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=brokers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch

type BrokerReconciler struct {
	Manager       multicluster.Manager
	ClientFactory internalclient.ClientFactory
}

func SetupBrokerController(_ context.Context, mgr multicluster.Manager, clientFactory internalclient.ClientFactory, namespace string) error {
	return mcbuilder.ControllerManagedBy(mgr).
		For(
			&redpandav1alpha2.Broker{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
		).
		Owns(&corev1.Pod{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Owns(&corev1.PersistentVolumeClaim{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Complete(
			controller.FilterNamespaceReconciler(
				namespace,
				observability.Wrap[mcreconcile.Request](&BrokerReconciler{
					Manager:       mgr,
					ClientFactory: clientFactory,
				}, "Broker", periodicRequeue)))
}

func (r *BrokerReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("BrokerReconciler.Reconcile")
	l.Info("Reconciling", "object", req.NamespacedName.String(), "cluster", req.ClusterName)

	k8sCluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	k8sClient := k8sCluster.GetClient()
	scheme := k8sCluster.GetScheme()

	var broker redpandav1alpha2.Broker
	if err := k8sClient.Get(ctx, req.NamespacedName, &broker); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	podName := broker.PodName()

	// 1. Ensure PVCs from volumeClaimTemplates.
	for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
		pvcName := fmt.Sprintf("%s-%s", vct.Name, podName)
		var pvc corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: broker.Namespace}, &pvc); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			pvc = corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvcName,
					Namespace: broker.Namespace,
					Labels:    broker.Spec.PodTemplate.Labels,
				},
				Spec: vct.Spec,
			}
			if err := controllerutil.SetControllerReference(&broker, &pvc, scheme); err != nil {
				return ctrl.Result{}, err
			}
			l.Info("creating PVC", "name", pvcName)
			if err := k8sClient.Create(ctx, &pvc); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 2. Ensure pod (shadow / adopt / create).
	// All pod-mutating actions (create, adopt) require a valid roll-grant.
	granted := hasValidRollGrant(ctx, &broker)

	var pod corev1.Pod
	err = k8sClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: broker.Namespace}, &pod)
	switch {
	case apierrors.IsNotFound(err):
		if !granted {
			l.Info("no roll-grant, skipping pod creation", "name", podName)
			return r.updateStatus(ctx, k8sClient, &broker, &corev1.Pod{}, redpandav1alpha2.BrokerPhasePending)
		}
		pod := broker.BuildPod(podName)
		// Inject PVC volumes that aren't already in the pod spec.
		existingVols := map[string]bool{}
		for _, v := range pod.Spec.Volumes {
			existingVols[v.Name] = true
		}
		for _, vct := range broker.Spec.Storage.VolumeClaimTemplates {
			if existingVols[vct.Name] {
				continue
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: vct.Name,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: fmt.Sprintf("%s-%s", vct.Name, podName),
					},
				},
			})
		}
		for _, ec := range broker.Spec.Storage.ExistingClaims {
			if existingVols[ec.Name] {
				continue
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
				Name: ec.Name,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: ec.Name,
					},
				},
			})
		}
		pod.Spec.Hostname = podName
		pod.Spec.Subdomain = broker.Spec.ClusterRef.Name
		if err := controllerutil.SetControllerReference(&broker, pod, scheme); err != nil {
			return ctrl.Result{}, err
		}
		l.Info("creating pod", "name", podName)
		if err := k8sClient.Create(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil

	case err != nil:
		return ctrl.Result{}, err

	default:
		// Pod exists — shadow, adopt, or own.
		if ctrl := metav1.GetControllerOf(&pod); ctrl != nil && !metav1.IsControlledBy(&pod, &broker) {
			l.Info("pod owned by another controller (shadow mode)", "owner", ctrl.Kind+"/"+ctrl.Name)
			return r.updateStatus(ctx, k8sClient, &broker, &pod, redpandav1alpha2.BrokerPhasePending)
		}
		if metav1.GetControllerOf(&pod) == nil {
			if !granted {
				l.Info("no roll-grant, skipping pod adoption", "name", podName)
				return r.updateStatus(ctx, k8sClient, &broker, &pod, redpandav1alpha2.BrokerPhasePending)
			}
			l.Info("adopting orphaned pod", "name", podName)
			if pod.Annotations == nil {
				pod.Annotations = map[string]string{}
			}
			if cs := broker.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]; cs != "" {
				pod.Annotations["config.redpanda.com/checksum"] = cs
			}
			if err := controllerutil.SetControllerReference(&broker, &pod, scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := k8sClient.Update(ctx, &pod); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		// Pod rotation: if the pod's checksum doesn't match the desired one, delete it.
		// The next reconcile will create the pod from the updated spec.
		desiredChecksum := broker.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]
		podChecksum := pod.Annotations["config.redpanda.com/checksum"]
		if desiredChecksum != "" && podChecksum != desiredChecksum {
			if !granted {
				l.Info("pod needs rotation but no roll-grant", "name", podName)
				return r.updateStatus(ctx, k8sClient, &broker, &pod, redpandav1alpha2.BrokerPhaseProvisioning)
			}
			// Put broker in maintenance mode before deleting.
			if broker.Status.BrokerID != nil {
				if err := r.enableMaintenanceMode(ctx, &broker); err != nil {
					return ctrl.Result{}, fmt.Errorf("enabling maintenance mode for broker %d: %w", *broker.Status.BrokerID, err)
				}
			}
			l.Info("rotating pod", "name", podName, "oldChecksum", podChecksum, "newChecksum", desiredChecksum)
			if err := k8sClient.Delete(ctx, &pod); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// 3. Resolve broker ID from admin API when pod is ready.
	if isPodReady(&pod) && broker.Status.BrokerID == nil {
		if brokerID, err := r.resolveBrokerID(ctx, &broker, podName); err != nil {
			l.Info("could not resolve broker ID", "error", err)
		} else if brokerID != nil {
			broker.Status.BrokerID = brokerID
		}
	}

	// 4. Determine phase from pod status.
	phase := redpandav1alpha2.BrokerPhaseProvisioning
	if isPodReady(&pod) {
		phase = redpandav1alpha2.BrokerPhaseRunning
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == "Unschedulable" {
			phase = redpandav1alpha2.BrokerPhaseStuck
		}
	}

	// 5. Execute decommission when spec.decommission is true and broker ID is known.
	if broker.Spec.Decommission && broker.Status.BrokerID != nil {
		decommResult, err := r.executeDecommission(ctx, &broker)
		if err != nil {
			return ctrl.Result{}, err
		}
		phase = decommResult.phase
		if decommResult.requeue {
			return r.updateStatus(ctx, k8sClient, &broker, &pod, phase)
		}
	} else if broker.Spec.Decommission {
		phase = redpandav1alpha2.BrokerPhaseDecommissioning
	}

	return r.updateStatus(ctx, k8sClient, &broker, &pod, phase)
}

func (r *BrokerReconciler) updateStatus(ctx context.Context, k8sClient client.Client, broker *redpandav1alpha2.Broker, pod *corev1.Pod, phase redpandav1alpha2.BrokerPhase) (ctrl.Result, error) {
	broker.Status.Phase = phase
	broker.Status.PodName = pod.Name
	broker.Status.PodIP = pod.Status.PodIP

	readyCond := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: broker.Generation,
	}
	if isPodReady(pod) {
		readyCond.Status = metav1.ConditionTrue
		readyCond.Reason = "PodReady"
		readyCond.Message = "Pod is ready"
	} else {
		readyCond.Status = metav1.ConditionFalse
		readyCond.Reason = "PodNotReady"
		readyCond.Message = "Pod is not ready"
	}
	apimeta.SetStatusCondition(&broker.Status.Conditions, readyCond)

	if err := k8sClient.Status().Update(ctx, broker); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: periodicRequeue}, nil
}

type decommissionResult struct {
	phase   redpandav1alpha2.BrokerPhase
	requeue bool
}

func (r *BrokerReconciler) executeDecommission(ctx context.Context, broker *redpandav1alpha2.Broker) (decommissionResult, error) {
	l := log.FromContext(ctx)
	brokerID := int(*broker.Status.BrokerID)

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, broker)
	if err != nil {
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}
	defer admin.Close()

	// Last-broker guard (RFC Q2).
	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}
	if len(brokers) <= 1 {
		l.Info("blocking decommission: last broker in cluster", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseStuck}, nil
	}

	status, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			l.Info("initiating decommission", "brokerID", brokerID)
			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
			}
			return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning, requeue: true}, nil
		}
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning}, err
	}

	if !status.Finished {
		l.Info("decommission in progress", "brokerID", brokerID)
		return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioning, requeue: true}, nil
	}

	l.Info("decommission finished", "brokerID", brokerID)
	return decommissionResult{phase: redpandav1alpha2.BrokerPhaseDecommissioned}, nil
}

func (r *BrokerReconciler) enableMaintenanceMode(ctx context.Context, broker *redpandav1alpha2.Broker) error {
	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, broker)
	if err != nil {
		return err
	}
	defer admin.Close()
	return admin.EnableMaintenanceMode(ctx, int(*broker.Status.BrokerID))
}

func (r *BrokerReconciler) resolveBrokerID(ctx context.Context, broker *redpandav1alpha2.Broker, podName string) (*int32, error) {
	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, broker)
	if err != nil {
		return nil, err
	}
	defer admin.Close()

	brokers, err := admin.Brokers(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range brokers {
		if strings.Split(b.InternalRPCAddress, ".")[0] == podName {
			return ptr.To(int32(b.NodeID)), nil
		}
	}
	return nil, nil
}

// hasValidRollGrant returns true if the Broker CR carries a roll-grant
// annotation whose config-checksum portion matches the Broker's desired
// pod template checksum.
func hasValidRollGrant(ctx context.Context, broker *redpandav1alpha2.Broker) bool {
	grant := feature.RollGrant.Get(ctx, broker)
	if grant == "" {
		return false
	}
	parts := strings.SplitN(grant, "/", 2)
	grantChecksum := parts[0]
	desiredChecksum := broker.Spec.PodTemplate.Annotations["config.redpanda.com/checksum"]
	return grantChecksum == desiredChecksum
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
