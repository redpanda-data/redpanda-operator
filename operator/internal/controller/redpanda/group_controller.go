// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package redpanda reconciles resources that comes from Redpanda dictionary like Topic, ACL and more.
package redpanda

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=groups,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=groups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=groups/finalizers,verbs=update

// GroupReconciler reconciles a Group object by managing ACLs
// for the group principal. Groups are external OIDC identities — the
// operator does not create or delete group entities in Redpanda.
type GroupReconciler struct {
	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

func (r *GroupReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.Group]) client.Patch {
	group := request.object
	config := redpandav1alpha2ac.Group(group.Name, group.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *GroupReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Group]) (client.Patch, error) {
	group := request.object

	var srSyncWarning error

	createPatch := func(err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.Group(group.Name, group.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else if srSyncWarning != nil {
			syncCondition = redpandav1alpha2.ResourcePartiallySyncedCondition(group.Name, srSyncWarning)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(group.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.GroupStatus().
			WithObservedGeneration(group.Generation).
			WithConditions(utils.StatusConditionConfigs(group.Status.Conditions, group.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	syncer, err := r.aclClient(ctx, request)
	if err != nil {
		return createPatch(err)
	}
	defer syncer.Close()

	// Always sync ACLs. When Authorization is nil or empty, this removes
	// any existing ACLs for the group principal.
	if err := syncer.Sync(ctx, group); err != nil {
		if !errors.Is(err, acls.ErrSchemaRegistryNotConfigured) {
			return createPatch(err)
		}
		srSyncWarning = err
	}

	return createPatch(nil)
}

func (r *GroupReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Group]) error {
	request.logger.V(2).Info("Deleting group ACLs from cluster")

	group := request.object

	syncer, err := r.aclClient(ctx, request)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	defer syncer.Close()

	if err := syncer.DeleteAll(ctx, group); err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}

	return nil
}

func (r *GroupReconciler) aclClient(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Group]) (*acls.Syncer, error) {
	return request.factory.ACLs(ctx, request.object, r.extraOptions...)
}

func SetupGroupController(ctx context.Context, mgr multicluster.Manager, expander *secrets.CloudExpander, includeV1, includeV2 bool, namespace string) error {
	factory := internalclient.NewFactory(mgr, expander)

	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.Group{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true), mcbuilder.WithPredicates(predicate.GenerationChangedPredicate{}))

	for _, clusterName := range mgr.GetClusterNames() {
		if includeV1 {
			enqueueV1Group, err := controller.RegisterV1ClusterSourceIndex(ctx, mgr, "group_v1", clusterName, &redpandav1alpha2.Group{}, &redpandav1alpha2.GroupList{})
			if err != nil {
				return err
			}
			builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1Group, controller.WatchOptions(clusterName)...)
		}

		if includeV2 {
			enqueueV2Group, err := controller.RegisterClusterSourceIndex(ctx, mgr, "group", clusterName, &redpandav1alpha2.Group{}, &redpandav1alpha2.GroupList{})
			if err != nil {
				return err
			}
			builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2Group, controller.WatchOptions(clusterName)...)
		}
	}

	controller := NewResourceController(mgr, factory, &GroupReconciler{}, "GroupReconciler")

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute).FilterNamespace(namespace))
}
