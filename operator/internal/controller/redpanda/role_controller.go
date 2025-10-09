// Copyright 2025 Redpanda Data, Inc.
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

	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=roles,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=roles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=roles/finalizers,verbs=update

// RoleReconciler reconciles a Role object
type RoleReconciler struct {
	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

func (r *RoleReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.Role]) client.Patch {
	role := request.object
	config := redpandav1alpha2ac.Role(role.Name, role.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *RoleReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Role]) (client.Patch, error) {
	role := request.object
	hasManagedACLs, hasManagedRole := role.HasManagedACLs(), role.HasManagedRole()
	shouldManageACLs, shouldManageRole := role.ShouldManageACLs(), role.ShouldManageRole()

	createPatch := func(err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.Role(role.Name, role.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(role.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.RoleStatus().
			WithObservedGeneration(role.Generation).
			WithManagedRole(hasManagedRole).
			WithManagedACLs(hasManagedACLs).
			WithConditions(utils.StatusConditionConfigs(role.Status.Conditions, role.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	rolesClient, syncer, hasRole, err := r.roleAndACLClients(ctx, request)
	if err != nil {
		return createPatch(err)
	}
	defer rolesClient.Close()
	defer syncer.Close()

	if !hasRole && shouldManageRole {
		if err := rolesClient.Create(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedRole = true
	}

	if hasRole && !shouldManageRole {
		if err := rolesClient.Delete(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedRole = false
	}

	if shouldManageACLs {
		if err := syncer.Sync(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedACLs = true
	}

	if !shouldManageACLs && hasManagedACLs {
		if err := syncer.DeleteAll(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedACLs = false
	}

	return createPatch(nil)
}

func (r *RoleReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Role]) error {
	request.logger.V(2).Info("Deleting role data from cluster")

	role := request.object
	hasManagedACLs, hasManagedRole := role.HasManagedACLs(), role.HasManagedRole()

	rolesClient, syncer, hasRole, err := r.roleAndACLClients(ctx, request)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	defer rolesClient.Close()
	defer syncer.Close()

	if hasRole && hasManagedRole {
		request.logger.V(2).Info("Deleting managed role")
		if err := rolesClient.Delete(ctx, role); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	if hasManagedACLs {
		request.logger.V(2).Info("Deleting managed ACLs")
		if err := syncer.DeleteAll(ctx, role); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	return nil
}

func (r *RoleReconciler) roleAndACLClients(ctx context.Context, request ResourceRequest[*redpandav1alpha2.Role]) (*roles.Client, *acls.Syncer, bool, error) {
	role := request.object
	rolesClient, err := request.factory.Roles(ctx, role)
	if err != nil {
		return nil, nil, false, err
	}

	syncer, err := request.factory.ACLs(ctx, role, r.extraOptions...)
	if err != nil {
		return nil, nil, false, err
	}

	hasRole, err := rolesClient.Has(ctx, role)
	if err != nil {
		return nil, nil, false, err
	}

	return rolesClient, syncer, hasRole, nil
}

func SetupRoleController(ctx context.Context, mgr ctrl.Manager, includeV1 bool) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Role{}).
		Owns(&corev1.Secret{})

	if includeV1 {
		enqueueV1Role, err := controller.RegisterV1ClusterSourceIndex(ctx, mgr, "role_v1", &redpandav1alpha2.Role{}, &redpandav1alpha2.RoleList{})
		if err != nil {
			return err
		}
		builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1Role)
	}

	enqueueV2Role, err := controller.RegisterClusterSourceIndex(ctx, mgr, "role", &redpandav1alpha2.Role{}, &redpandav1alpha2.RoleList{})
	if err != nil {
		return err
	}
	builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2Role)

	controller := NewResourceController(c, factory, &RoleReconciler{}, "RoleReconciler")

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
