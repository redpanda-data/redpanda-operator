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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=shadowlinks/finalizers,verbs=update

// ShadowLinkReconciler reconciles a ShadowLink object
type ShadowLinkReconciler struct{}

func (r *ShadowLinkReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.ShadowLink]) client.Patch {
	shadowLink := request.object
	config := redpandav1alpha2ac.ShadowLink(shadowLink.Name, shadowLink.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *ShadowLinkReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.ShadowLink]) (client.Patch, error) {
	shadowLink := request.object

	createPatch := func(err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.ShadowLink(shadowLink.Name, shadowLink.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(shadowLink.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.ShadowLinkStatus().
			// TODO: read back shadow link state
			WithConditions(utils.StatusConditionConfigs(shadowLink.Status.Conditions, shadowLink.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	// TODO: add in sync logic

	return createPatch(nil)
}

func (r *ShadowLinkReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.ShadowLink]) error {
	request.logger.V(2).Info("Deleting shadow link from cluster")

	// TODO: add in deletion logic

	return nil
}

func SetupShadowLinkController(ctx context.Context, mgr ctrl.Manager) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)
	controller := NewResourceController(c, factory, &ShadowLinkReconciler{}, "ShadowLinkReconciler")

	enqueueShadowLink, err := registerClusterSourceIndex(ctx, mgr, "shadowLink", &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.ShadowLink{}).
		Watches(&redpandav1alpha2.Redpanda{}, enqueueShadowLink).
		// Every 5 minutes try and check to make sure no manual modifications
		// happened on the resource synced to the cluster and attempt to correct
		// any drift.
		Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
