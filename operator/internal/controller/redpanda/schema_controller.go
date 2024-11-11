// Copyright 2024 Redpanda Data, Inc.
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
	"time"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=schemas,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=schemas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=schemas/finalizers,verbs=update

// SchemaReconciler reconciles a schema object
type SchemaReconciler struct{}

func (r *SchemaReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.Schema]) client.Patch {
	schema := request.object
	config := redpandav1alpha2ac.Schema(schema.Name, schema.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *SchemaReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Schema]) (client.Patch, error) {
	schema := request.object
	createPatch := func(err error, hash string, versions []int) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.Schema(schema.Name, schema.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(schema.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.SchemaStatus().
			WithObservedGeneration(schema.Generation).
			WithVersions(versions...).
			WithSchemaHash(hash).
			WithConditions(utils.StatusConditionConfigs(schema.Status.Conditions, schema.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	hash := schema.Status.SchemaHash
	versions := schema.Status.Versions
	syncer, err := request.factory.Schemas(ctx, schema)
	if err != nil {
		return createPatch(err, hash, versions)
	}

	hash, versions, err = syncer.Sync(ctx, schema)
	return createPatch(err, hash, versions)
}

func (r *SchemaReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Schema]) error {
	syncer, err := request.factory.Schemas(ctx, request.object)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	if err := syncer.Delete(ctx, request.object); err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	return nil
}

func SetupSchemaController(ctx context.Context, mgr ctrl.Manager) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)
	controller := NewResourceController(c, factory, &SchemaReconciler{}, "SchemaReconciler")

	enqueueSchema, err := registerClusterSourceIndex(ctx, mgr, "schema", &redpandav1alpha2.Schema{}, &redpandav1alpha2.SchemaList{})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Schema{}).
		Watches(&redpandav1alpha2.Redpanda{}, enqueueSchema).
		// Every 5 minutes try and check to make sure no manual modifications
		// happened on the resource synced to the cluster and attempt to correct
		// any drift.
		Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
