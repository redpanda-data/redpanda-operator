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
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
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

	createPatch := func(err error, tasks []redpandav1alpha2.ShadowLinkTaskStatus, topics []redpandav1alpha2.ShadowTopicStatus) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.ShadowLink(shadowLink.Name, shadowLink.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(shadowLink.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.ShadowLinkStatus().
			WithShadowTopicStatuses(redpandav1alpha2.ShadowTopicStatusesToConfigs(shadowLink.Status.ShadowTopicStatuses, topics)...).
			WithTaskStatuses(redpandav1alpha2.ShadowLinkTaskStatusesToConfigs(shadowLink.Status.TaskStatuses, tasks)...).
			WithConditions(utils.StatusConditionConfigs(shadowLink.Status.Conditions, shadowLink.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	tasks := shadowLink.Status.TaskStatuses
	topics := shadowLink.Status.ShadowTopicStatuses
	syncer, err := request.factory.ShadowLinks(ctx, shadowLink)
	if err != nil {
		return createPatch(err, tasks, topics)
	}

	tasks, topics, err = syncer.Sync(ctx, shadowLink, nil)
	return createPatch(err, tasks, topics)
}

func (r *ShadowLinkReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.ShadowLink]) error {
	syncer, err := request.factory.ShadowLinks(ctx, request.object)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	if err := syncer.Delete(ctx, request.object); err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}

	return nil
}

func SetupShadowLinkController(ctx context.Context, mgr ctrl.Manager, includeV1 bool) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)
	controller := NewResourceController(c, factory, &ShadowLinkReconciler{}, "ShadowLinkReconciler")

	enqueueShadowLink, err := registerClusterSourceIndex(ctx, mgr, "shadow_link", &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.ShadowLink{}).
		Watches(&redpandav1alpha2.Redpanda{}, enqueueShadowLink)

	if includeV1 {
		enqueueV1ShadowLink, err := registerClusterSourceIndex(ctx, mgr, "shadow_link_v1", &redpandav1alpha2.ShadowLink{}, &redpandav1alpha2.ShadowLinkList{})
		if err != nil {
			return err
		}
		builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1ShadowLink)
	}

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
