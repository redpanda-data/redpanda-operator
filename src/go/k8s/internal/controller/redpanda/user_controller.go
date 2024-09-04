// Copyright 2021-2023 Redpanda Data, Inc.
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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client/acls"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client/users"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/utils"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const fieldOwner client.FieldOwner = "redpanda-operator"

// UserReconciler reconciles a Topic object
type UserReconciler struct {
	client.Client
	internalclient.ClientFactory

	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users/finalizers,verbs=update

// For cluster scoped operator

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *UserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("UserReconciler.Reconcile")
	l.Info("Starting reconcile loop")

	user := &redpandav1alpha2.User{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !user.DeletionTimestamp.IsZero() {
		if err := r.deleteUser(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.RemoveFinalizer(user, FinalizerKey) {
			return ctrl.Result{}, r.Update(ctx, user)
		}
		return ctrl.Result{}, nil
	}

	config := redpandav1alpha2ac.User(user.Name, user.Namespace)

	if !controllerutil.ContainsFinalizer(user, FinalizerKey) {
		patch := kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
		if err := r.Patch(ctx, user, patch, client.ForceOwnership, fieldOwner); err != nil {
			return ctrl.Result{}, err
		}
	}

	syncCondition, manageUser, manageACLs, err := r.syncUser(ctx, user)

	patch := kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.UserStatus().
		WithObservedGeneration(user.Generation).
		WithManagedUser(manageUser).
		WithManagedACLs(manageACLs).
		WithConditions(utils.StatusConditionConfigs(user.Status.Conditions, user.Generation, []metav1.Condition{
			syncCondition,
		})...)))
	syncError := r.Status().Patch(ctx, user, patch, client.ForceOwnership, fieldOwner)

	return ctrl.Result{}, errors.Join(err, syncError)
}

func (r *UserReconciler) syncUser(ctx context.Context, user *redpandav1alpha2.User) (condition metav1.Condition, managedUser, managedACLs bool, err error) {
	hasManagedACLs, hasManagedUser := user.HasManagedACLs(), user.HasManagedUser()
	shouldManageACLs, shouldManageUser := user.ShouldManageACLs(), user.ShouldManageUser()

	handleErrors := func(err error) (metav1.Condition, bool, bool, error) {
		// If we have a known terminal error, just set the sync condition and don't re-run reconciliation.
		if internalclient.IsInvalidClusterError(err) {
			return redpandav1alpha2.UserNotSyncedCondition(redpandav1alpha2.UserConditionReasonClusterRefInvalid, err), hasManagedUser, hasManagedACLs, nil
		}
		if internalclient.IsConfigurationError(err) {
			return redpandav1alpha2.UserNotSyncedCondition(redpandav1alpha2.UserConditionReasonConfigurationInvalid, err), hasManagedUser, hasManagedACLs, nil
		}
		if internalclient.IsTerminalClientError(err) {
			return redpandav1alpha2.UserNotSyncedCondition(redpandav1alpha2.UserConditionReasonTerminalClientError, err), hasManagedUser, hasManagedACLs, nil
		}

		// otherwise, set a generic unexpected error and return an error so we can re-reconcile.
		return redpandav1alpha2.UserNotSyncedCondition(redpandav1alpha2.UserConditionReasonUnexpectedError, err), hasManagedUser, hasManagedACLs, err
	}

	usersClient, syncer, hasUser, err := r.userAndACLClients(ctx, user)
	if err != nil {
		return handleErrors(err)
	}

	if !hasUser && shouldManageUser {
		if err := usersClient.Create(ctx, user); err != nil {
			return handleErrors(err)
		}
		hasManagedUser = true
	}

	if hasUser && !shouldManageUser {
		if err := usersClient.Delete(ctx, user); err != nil {
			return handleErrors(err)
		}
		hasManagedUser = false
	}

	if shouldManageACLs {
		if err := syncer.Sync(ctx, user); err != nil {
			return handleErrors(err)
		}
		hasManagedACLs = true
	}

	if !shouldManageACLs && hasManagedACLs {
		if err := syncer.DeleteAll(ctx, user); err != nil {
			return handleErrors(err)
		}
		hasManagedACLs = false
	}

	return redpandav1alpha2.UserSyncedCondition(user.Name), hasManagedUser, hasManagedACLs, nil
}

func (r *UserReconciler) deleteUser(ctx context.Context, user *redpandav1alpha2.User) error {
	hasManagedACLs, hasManagedUser := user.HasManagedACLs(), user.HasManagedUser()

	ignoreAllConnectionErrors := func(err error) error {
		// If we have known errors where we're unable to actually establish
		// a connection to the cluster due to say, invalid connection parameters
		// we're going to just skip the cleanup phase since we likely won't be
		// able to clean ourselves up anyway.
		if internalclient.IsTerminalClientError(err) ||
			internalclient.IsConfigurationError(err) ||
			internalclient.IsInvalidClusterError(err) {
			return nil
		}
		return err
	}

	usersClient, syncer, hasUser, err := r.userAndACLClients(ctx, user)
	if err != nil {
		return ignoreAllConnectionErrors(err)
	}

	if hasUser && hasManagedUser {
		if err := usersClient.Delete(ctx, user); err != nil {
			return ignoreAllConnectionErrors(err)
		}
	}

	if hasManagedACLs {
		if err := syncer.DeleteAll(ctx, user); err != nil {
			return ignoreAllConnectionErrors(err)
		}
	}

	return nil
}

func (r *UserReconciler) userAndACLClients(ctx context.Context, user *redpandav1alpha2.User) (*users.Client, *acls.Syncer, bool, error) {
	usersClient, err := r.Users(ctx, user, r.extraOptions...)
	if err != nil {
		return nil, nil, false, err
	}

	syncer, err := r.ACLs(ctx, user, r.extraOptions...)
	if err != nil {
		return nil, nil, false, err
	}

	hasUser, err := usersClient.Has(ctx, user)
	if err != nil {
		return nil, nil, false, err
	}

	return usersClient, syncer, hasUser, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := registerUserClusterIndex(ctx, mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.User{}).
		Owns(&corev1.Secret{}).
		Watches(&redpandav1alpha2.Redpanda{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			requests, err := usersForCluster(ctx, r, client.ObjectKeyFromObject(o))
			if err != nil {
				mgr.GetLogger().V(1).Info("possibly skipping user reconciliation due to failure to fetch users associated with cluster", "error", err)
				return nil
			}
			return requests
		})).
		Complete(r)
}
