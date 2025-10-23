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
	"fmt"
	"reflect"
	"time"

	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandarolebindings,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandarolebindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandaroles/status,verbs=get
//
// RoleBindingReconciler reconciles a RoleBinding object
// This controller is responsible for:
// - Validating that the referenced Role exists
// - Setting status conditions for user feedback
//
// Note: This controller does NOT call the Redpanda Admin API directly.
// The Role controller is responsible for aggregating principals from all
// RoleBindings and syncing them to Redpanda.
type RoleBindingReconciler struct {
	client client.Client
}

func (r *RoleBindingReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.RedpandaRoleBinding]) client.Patch {
	// RoleBindings don't need finalizers.
	// The Role controller watches RoleBindings and filters out those with deletionTimestamp set,
	// ensuring they don't contribute principals during deletion.
	return nil
}

func (r *RoleBindingReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRoleBinding]) (patch client.Patch, err error) {
	rb := request.object

	ctx, span := trace.Start(otelkube.Extract(ctx, rb), "SyncResource", trace.WithAttributes(
		attribute.String("name", rb.Name),
		attribute.String("namespace", rb.Namespace),
		attribute.String("roleRef", rb.Spec.RoleRef.Name),
		attribute.Int("principals", len(rb.Spec.Principals)),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	createPatch := func(cond *metav1.Condition, err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.RedpandaRoleBinding(rb.Name, rb.Namespace)

		if cond != nil {
			// Use provided condition directly
			syncCondition = *cond
		} else if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(rb.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.RedpandaRoleBindingStatus().
			WithObservedGeneration(rb.Generation).
			WithConditions(utils.StatusConditionConfigs(rb.Status.Conditions, rb.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	// Validate Role exists
	var role redpandav1alpha2.RedpandaRole
	roleKey := client.ObjectKey{
		Namespace: rb.Namespace,
		Name:      rb.Spec.RoleRef.Name,
	}

	checkErr := r.client.Get(ctx, roleKey, &role)

	if checkErr != nil {
		logger.Error(checkErr, "failed to get referenced role", "role", rb.Spec.RoleRef.Name)
		// For NotFound errors, create condition directly and return nil (don't requeue validation errors)
		if apierrors.IsNotFound(checkErr) {
			notFoundCondition := redpandav1alpha2.ResourceNotSyncedCondition(
				redpandav1alpha2.ResourceConditionReasonUnexpectedError,
				checkErr,
			)
			return createPatch(&notFoundCondition, nil)
		}
		// For other errors (network, permissions), use standard error handling
		return createPatch(nil, checkErr)
	}

	// Role exists - check if it's synced
	roleSyncedCond := apimeta.FindStatusCondition(role.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
	if roleSyncedCond == nil || roleSyncedCond.Status != metav1.ConditionTrue {
		logger.V(log.DebugLevel).Info("role not yet synced, waiting", "role", role.Name)
		// Pass condition directly with nil error - watch will trigger when role updates
		waitingCondition := redpandav1alpha2.ResourceNotSyncedCondition(
			redpandav1alpha2.ResourceConditionReasonUnexpectedError,
			fmt.Errorf("role %s not yet synced", role.Name),
		)
		return createPatch(&waitingCondition, nil)
	}

	// Check all principals are in Role status
	missing, allFound := checkPrincipalsInRole(rb.Spec.Principals, role.Status.Principals)
	if !allFound {
		logger.V(log.DebugLevel).Info("principals not yet synced to role", "role", role.Name, "missing", missing)
		// Pass condition directly with nil error - watch will trigger when role updates
		waitingCondition := redpandav1alpha2.ResourceNotSyncedCondition(
			redpandav1alpha2.ResourceConditionReasonUnexpectedError,
			fmt.Errorf("principals %v not yet synced to role %s", missing, role.Name),
		)
		return createPatch(&waitingCondition, nil)
	}

	// Success - all validation passed
	logger.V(log.DebugLevel).Info("all principals synced to role", "role", role.Name)
	return createPatch(nil, nil)
}

func (r *RoleBindingReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.RedpandaRoleBinding]) error {
	// No-op: RoleBindings don't need finalizers.
	// The Role controller watches RoleBindings and automatically excludes those with
	// deletionTimestamp set (see role_controller.go:127), ensuring they don't contribute
	// principals during deletion.
	return nil
}

// checkPrincipalsInRole verifies if all binding principals are present in the role's principal list.
// It returns the list of missing principals and a boolean indicating if all were found.
// Both binding and role principals are normalized before comparison.
func checkPrincipalsInRole(bindingPrincipals []string, rolePrincipals []string) (missing []string, allFound bool) {
	ourPrincipals := make(map[string]struct{})
	for _, p := range bindingPrincipals {
		ourPrincipals[roles.NormalizePrincipal(p)] = struct{}{}
	}

	foundPrincipals := make(map[string]struct{})
	for _, rolePrincipal := range rolePrincipals {
		if _, exists := ourPrincipals[rolePrincipal]; exists {
			foundPrincipals[rolePrincipal] = struct{}{}
		}
	}

	if len(foundPrincipals) != len(ourPrincipals) {
		for principal := range ourPrincipals {
			if _, found := foundPrincipals[principal]; !found {
				missing = append(missing, principal)
			}
		}
		return missing, false
	}

	return nil, true
}

// roleStatusChangesPredicate filters Role events to only trigger reconciliation
// when fields relevant to RoleBinding validation change.
func roleStatusChangesPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true // Always process creates
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldRole := e.ObjectOld.(*redpandav1alpha2.RedpandaRole)
			newRole := e.ObjectNew.(*redpandav1alpha2.RedpandaRole)

			// Trigger if Synced condition changed
			oldSynced := apimeta.FindStatusCondition(oldRole.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
			newSynced := apimeta.FindStatusCondition(newRole.Status.Conditions, redpandav1alpha2.ResourceConditionTypeSynced)
			if !reflect.DeepEqual(oldSynced, newSynced) {
				return true
			}

			// Trigger if Principals list changed
			if !reflect.DeepEqual(oldRole.Status.Principals, newRole.Status.Principals) {
				return true
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true // Always process deletes
		},
	}
}

func SetupRoleBindingController(ctx context.Context, mgr ctrl.Manager) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)

	resourceController := NewResourceController(c, factory, &RoleBindingReconciler{
		client: c,
	}, "RoleBindingReconciler")

	// RoleBinding controller watches:
	// 1. RoleBindings directly (via For) - triggers on RoleBinding create/update/delete
	// 2. Roles - triggers when Role changes (including status updates like Synced condition and Principals)
	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.RedpandaRoleBinding{}).
		Watches(&redpandav1alpha2.RedpandaRole{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, obj client.Object) []reconcile.Request {
				role := obj.(*redpandav1alpha2.RedpandaRole)

				// Find all RoleBindings that reference this Role using the field index
				// (The index was created by the Role controller at role_controller.go:234-239)
				roleBindings := &redpandav1alpha2.RedpandaRoleBindingList{}
				if err := c.List(ctx, roleBindings,
					client.InNamespace(role.Namespace),
					client.MatchingFields{"spec.roleRef.name": role.Name},
				); err != nil {
					// Log error but don't fail - periodic reconciliation will catch it
					log.FromContext(ctx).Error(err, "failed to list rolebindings for role watch",
						"role", role.Name, "namespace", role.Namespace)
					return nil
				}

				// Enqueue reconcile requests for each RoleBinding
				requests := make([]reconcile.Request, 0, len(roleBindings.Items))
				for _, rb := range roleBindings.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: rb.Namespace,
							Name:      rb.Name,
						},
					})
				}
				return requests
			},
		), builder.WithPredicates(roleStatusChangesPredicate())).
		Complete(resourceController.PeriodicallyReconcile(5 * time.Minute))
}
