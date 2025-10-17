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
	"reflect"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandarolebindings,verbs=get;list;watch

// RoleReconciler reconciles a Role object
type RoleReconciler struct {
	client client.Client
	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

func (r *RoleReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.RedpandaRole]) client.Patch {
	role := request.object
	config := redpandav1alpha2ac.RedpandaRole(role.Name, role.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *RoleReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRole]) (client.Patch, error) {
	role := request.object
	hasManagedACLs, hasManagedRole := role.HasManagedACLs(), role.HasManagedRole()
	shouldManageACLs, shouldManageRole := role.ShouldManageACLs(), role.ShouldManageRole()

	var syncedPrincipals []string // Track what we successfully synced

	createPatch := func(syncedPrincipals []string, err error) (client.Patch, error) {
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
			WithPrincipals(syncedPrincipals...).
			WithConditions(utils.StatusConditionConfigs(role.Status.Conditions, role.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	rolesClient, syncer, hasRole, err := r.roleAndACLClients(ctx, request)
	if err != nil {
		return createPatch(syncedPrincipals, err)
	}
	defer rolesClient.Close()
	defer syncer.Close()

	// Fetch RoleBindings that reference this Role
	// Try using field index first, fall back to listing all and filtering client-side if index not available
	roleBindings := &redpandav1alpha2.RedpandaRoleBindingList{}
	err = r.client.List(ctx, roleBindings, &client.ListOptions{
		Namespace: role.Namespace,
	}, client.MatchingFields{"spec.roleRef.name": role.Name})
	// If field index is not supported, fall back to listing all and filtering client-side
	// this is mainly to support simple testing
	if err != nil {
		if apierrors.IsBadRequest(err) {
			allBindings := &redpandav1alpha2.RedpandaRoleBindingList{}
			if err = r.client.List(ctx, allBindings, &client.ListOptions{
				Namespace: role.Namespace,
			}); err == nil {
				// Filter client-side to find RoleBindings that reference this Role
				roleBindings.Items = make([]redpandav1alpha2.RedpandaRoleBinding, 0)
				for _, rb := range allBindings.Items {
					if rb.Spec.RoleRef.Name == role.Name {
						roleBindings.Items = append(roleBindings.Items, rb)
					}
				}
			}
			// If the fallback List() fails, err is still set and will be handled below
		}
		// If it's not a BadRequest, it's a real error (permission denied, network issue, etc.)
	}

	// Report any errors in the status
	if err != nil {
		return createPatch(syncedPrincipals, err)
	}

	// Filter out RoleBindings that are being deleted (with deletionTimestamp set)
	// RoleBindings with deletionTimestamp should not contribute principals to the Role
	var activeBindings []*redpandav1alpha2.RedpandaRoleBinding
	for i := range roleBindings.Items {
		rb := &roleBindings.Items[i]
		if rb.DeletionTimestamp.IsZero() {
			activeBindings = append(activeBindings, rb)
		} else {
			request.logger.V(2).Info("excluding deleting rolebinding from principal aggregation",
				"rolebinding", rb.Name, "deletionTimestamp", rb.DeletionTimestamp)
		}
	}

	if !hasRole && shouldManageRole {
		syncedPrincipals, err = rolesClient.Create(ctx, role, activeBindings)
		if err != nil {
			return createPatch(syncedPrincipals, err)
		}
		hasManagedRole = true
	}

	if hasRole && !shouldManageRole {
		if err := rolesClient.Delete(ctx, role); err != nil {
			return createPatch(syncedPrincipals, err)
		}
		hasManagedRole = false
	}

	// Update role membership if it exists and:
	// 1. Principals are defined (inline or via RoleBindings), OR
	// 2. We previously managed principals and need to clean up (transitioning to manual mode)
	if hasRole && (len(role.Spec.Principals) > 0 || len(activeBindings) > 0 || len(role.Status.Principals) > 0) {
		syncedPrincipals, err = rolesClient.Update(ctx, role, activeBindings)
		if err != nil {
			return createPatch(syncedPrincipals, err)
		}
	}

	if shouldManageACLs {
		if err := syncer.Sync(ctx, role); err != nil {
			return createPatch(syncedPrincipals, err)
		}
		hasManagedACLs = true
	}

	if !shouldManageACLs && hasManagedACLs {
		if err := syncer.DeleteAll(ctx, role); err != nil {
			return createPatch(syncedPrincipals, err)
		}
		hasManagedACLs = false
	}

	return createPatch(syncedPrincipals, nil)
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

// roleBindingSpecChangesPredicate filters RoleBinding events to only trigger
// reconciliation when fields relevant to Role principal aggregation change.
func roleBindingSpecChangesPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true // Always process creates
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldRB := e.ObjectOld.(*redpandav1alpha2.RedpandaRoleBinding)
			newRB := e.ObjectNew.(*redpandav1alpha2.RedpandaRoleBinding)

			// Trigger if principals changed
			if !reflect.DeepEqual(oldRB.Spec.Principals, newRB.Spec.Principals) {
				return true
			}

			// Trigger if roleRef changed
			if oldRB.Spec.RoleRef.Name != newRB.Spec.RoleRef.Name {
				return true
			}

			// Trigger if deletion status changed
			if oldRB.DeletionTimestamp.IsZero() != newRB.DeletionTimestamp.IsZero() {
				return true
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true // Always process deletes
		},
	}
}

func SetupRoleController(ctx context.Context, mgr ctrl.Manager, includeV1, includeV2 bool) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)

	// Set up field index for querying RoleBindings by their roleRef.name
	// This allows the Role controller to efficiently find all RoleBindings that reference a specific Role
	if err := mgr.GetFieldIndexer().IndexField(ctx, &redpandav1alpha2.RedpandaRoleBinding{}, "spec.roleRef.name", func(obj client.Object) []string {
		rb := obj.(*redpandav1alpha2.RedpandaRoleBinding)
		return []string{rb.Spec.RoleRef.Name}
	}); err != nil {
		return err
	}

	bldr := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.RedpandaRole{}).
		Watches(&redpandav1alpha2.RedpandaRoleBinding{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			rb := o.(*redpandav1alpha2.RedpandaRoleBinding)
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Namespace: rb.Namespace,
					Name:      rb.Spec.RoleRef.Name,
				},
			}}
		}), builder.WithPredicates(roleBindingSpecChangesPredicate()))

	if includeV1 {
		enqueueV1Role, err := controller.RegisterV1ClusterSourceIndex(ctx, mgr, "role_v1", &redpandav1alpha2.RedpandaRole{}, &redpandav1alpha2.RedpandaRoleList{})
		if err != nil {
			return err
		}
		bldr.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1Role)
	}

	if includeV2 {
		enqueueV2Role, err := controller.RegisterClusterSourceIndex(ctx, mgr, "role", &redpandav1alpha2.RedpandaRole{}, &redpandav1alpha2.RedpandaRoleList{})
		if err != nil {
			return err
		}
		bldr.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2Role)
	}

	ctrl := NewResourceController(c, factory, &RoleReconciler{
		client: c,
	}, "RoleReconciler")

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return bldr.Complete(ctrl.PeriodicallyReconcile(5 * time.Minute))
}
