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
	"time"

	"github.com/cockroachdb/errors"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/operator/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles/finalizers,verbs=update

// RoleReconciler reconciles a Role object
type RoleReconciler struct {
	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

// isRoleRename returns true if a role rename operation is needed.
// A rename is needed when:
// - We have a previous effective name tracked in status (not empty)
// - The effective name has changed
// - We are managing this role
func isRoleRename(previousEffectiveName, currentEffectiveName string, hasManagedRole bool) bool {
	return previousEffectiveName != "" && previousEffectiveName != currentEffectiveName && hasManagedRole
}

func (r *RoleReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.RedpandaRole]) client.Patch {
	role := request.object
	config := redpandav1alpha2ac.RedpandaRole(role.Name, role.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *RoleReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRole]) (client.Patch, error) {
	role := request.object
	hasManagedACLs, hasManagedRole, hasManagedPrincipals := role.HasManagedACLs(), role.HasManagedRole(), role.HasManagedPrincipals()
	shouldManageACLs, shouldManageRole, shouldManagePrincipals := role.ShouldManageACLs(), role.ShouldManageRole(), role.ShouldManagePrincipals()

	// Get current and previous effective role names to detect renames
	currentEffectiveName := role.GetEffectiveRoleName()
	previousEffectiveName := role.Status.EffectiveRoleName

	createPatch := func(err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.RedpandaRole(role.Name, role.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(role.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.RoleStatus().
			WithObservedGeneration(role.Generation).
			WithManagedRole(hasManagedRole).
			WithManagedACLs(hasManagedACLs).
			WithManagedPrincipals(hasManagedPrincipals).
			WithEffectiveRoleName(currentEffectiveName).
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

	// Handle role rename if effective name changed
	if isRoleRename(previousEffectiveName, currentEffectiveName, hasManagedRole) {
		request.logger.V(1).Info("Role rename", "from", previousEffectiveName, "to", currentEffectiveName)

		// Create new role
		if !hasRole {
			if err := rolesClient.Create(ctx, role); err != nil {
				return createPatch(errors.Wrap(err, "creating renamed role"))
			}
		} else {
			request.logger.V(1).Info("New role already exists, skipping creation", "role", currentEffectiveName)
		}

		// Sync new ACLs first
		if shouldManageACLs {
			if err := syncer.Sync(ctx, role); err != nil {
				return createPatch(errors.Wrap(err, "syncing new ACLs"))
			}
		}

		// Clean up old resources
		previousRole := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{Name: previousEffectiveName},
		}

		if hasManagedACLs {
			if err := syncer.DeleteAll(ctx, previousRole); err != nil {
				return createPatch(errors.Wrap(err, "deleting old ACLs"))
			}
		}

		if err := rolesClient.Delete(ctx, previousRole); err != nil {
			return createPatch(errors.Wrap(err, "deleting old role"))
		}

		hasManagedRole = true
		hasManagedPrincipals = shouldManagePrincipals
		hasManagedACLs = shouldManageACLs
		return createPatch(nil)
	}

	if !hasRole && shouldManageRole {
		if err := rolesClient.Create(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedRole = true
		hasManagedPrincipals = shouldManagePrincipals
	}

	if hasRole && shouldManageRole {
		if shouldManagePrincipals {
			if err := rolesClient.Update(ctx, role); err != nil {
				return createPatch(err)
			}
			hasManagedPrincipals = true
		} else if hasManagedPrincipals {
			if err := rolesClient.ClearPrincipals(ctx, role); err != nil {
				return createPatch(err)
			}
			hasManagedPrincipals = false
		}
		hasManagedRole = true
	}

	if hasRole && !shouldManageRole {
		if err := rolesClient.Delete(ctx, role); err != nil {
			return createPatch(err)
		}
		hasManagedRole = false
		hasManagedPrincipals = false
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

func (r *RoleReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRole]) error {
	request.logger.V(2).Info("Deleting role data from cluster")

	role := request.object
	hasManagedACLs, hasManagedRole := role.HasManagedACLs(), role.HasManagedRole()

	rolesClient, syncer, hasRole, err := r.roleAndACLClients(ctx, request)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}
	defer rolesClient.Close()
	defer syncer.Close()

	// Get current and previous effective names for comprehensive cleanup
	currentEffectiveName := role.GetEffectiveRoleName()
	previousEffectiveName := role.Status.EffectiveRoleName

	// Delete current role (from spec)
	if hasRole && hasManagedRole {
		request.logger.V(2).Info("Deleting current managed role", "name", currentEffectiveName)
		if err := rolesClient.Delete(ctx, role); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	// Delete previous role if different (handles incomplete rename scenarios)
	if isRoleRename(previousEffectiveName, currentEffectiveName, hasManagedRole) {
		request.logger.V(2).Info("Deleting previous role from incomplete rename", "name", previousEffectiveName)
		previousRole := &redpandav1alpha2.RedpandaRole{
			ObjectMeta: metav1.ObjectMeta{Name: previousEffectiveName},
		}
		if err := rolesClient.Delete(ctx, previousRole); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	if hasManagedACLs {
		request.logger.V(2).Info("Deleting managed ACLs")

		if err := syncer.DeleteAll(ctx, role); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}

		// Delete ACLs for previous principal if it differs (handles rename scenarios)
		if previousEffectiveName != "" && previousEffectiveName != currentEffectiveName {
			request.logger.V(2).Info("Deleting ACLs for previous principal", "previousName", previousEffectiveName)

			previousRole := &redpandav1alpha2.RedpandaRole{
				ObjectMeta: metav1.ObjectMeta{Name: previousEffectiveName},
			}

			if err := syncer.DeleteAll(ctx, previousRole); err != nil {
				return ignoreAllConnectionErrors(request.logger, err)
			}
		}
	}

	return nil
}

func (r *RoleReconciler) roleAndACLClients(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRole]) (*roles.Client, *acls.Syncer, bool, error) {
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

func SetupRoleController(ctx context.Context, mgr multicluster.Manager, expander *secrets.CloudExpander, includeV1, includeV2 bool) error {
	factory := internalclient.NewFactory(mgr, expander)

	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.RedpandaRole{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Owns(&corev1.Secret{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))

	for _, clusterName := range mgr.GetClusterNames() {
		if includeV1 {
			enqueueV1Role, err := controller.RegisterV1ClusterSourceIndex(ctx, mgr, "role_v1", clusterName, &redpandav1alpha2.RedpandaRole{}, &redpandav1alpha2.RedpandaRoleList{})
			if err != nil {
				return err
			}
			builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1Role, controller.WatchOptions(clusterName)...)
		}

		if includeV2 {
			enqueueV2Role, err := controller.RegisterClusterSourceIndex(ctx, mgr, "role", clusterName, &redpandav1alpha2.RedpandaRole{}, &redpandav1alpha2.RedpandaRoleList{})
			if err != nil {
				return err
			}
			builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2Role, controller.WatchOptions(clusterName)...)
		}
	}

	controller := NewResourceController(mgr, factory, &RoleReconciler{}, "RoleReconciler")

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
