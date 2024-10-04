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
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/acls"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/kubernetes"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/twmb/franz-go/pkg/kgo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=users/finalizers,verbs=update

// For cluster scoped operator

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=users/finalizers,verbs=update

// UserReconciler reconciles a User object
type UserReconciler struct {
	// extraOptions can be overridden in tests
	// to change the way the underlying clients
	// function, i.e. setting low timeouts
	extraOptions []kgo.Opt
}

func (r *UserReconciler) FinalizerPatch(request ResourceRequest[*redpandav1alpha2.User]) client.Patch {
	user := request.object
	config := redpandav1alpha2ac.User(user.Name, user.Namespace)
	return kubernetes.ApplyPatch(config.WithFinalizers(FinalizerKey))
}

func (r *UserReconciler) SyncResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.User]) (client.Patch, error) {
	user := request.object
	hasManagedACLs, hasManagedUser := user.HasManagedACLs(), user.HasManagedUser()
	shouldManageACLs, shouldManageUser := user.ShouldManageACLs(), user.ShouldManageUser()

	createPatch := func(err error) (client.Patch, error) {
		var syncCondition metav1.Condition
		config := redpandav1alpha2ac.User(user.Name, user.Namespace)

		if err != nil {
			syncCondition, err = handleResourceSyncErrors(err)
		} else {
			syncCondition = redpandav1alpha2.ResourceSyncedCondition(user.Name)
		}

		return kubernetes.ApplyPatch(config.WithStatus(redpandav1alpha2ac.UserStatus().
			WithObservedGeneration(user.Generation).
			WithManagedUser(hasManagedUser).
			WithManagedACLs(hasManagedACLs).
			WithConditions(utils.StatusConditionConfigs(user.Status.Conditions, user.Generation, []metav1.Condition{
				syncCondition,
			})...))), err
	}

	usersClient, syncer, hasUser, err := r.userAndACLClients(ctx, request)
	if err != nil {
		return createPatch(err)
	}

	if !hasUser && shouldManageUser {
		if err := usersClient.Create(ctx, user); err != nil {
			return createPatch(err)
		}
		hasManagedUser = true
	}

	if hasUser && !shouldManageUser {
		if err := usersClient.Delete(ctx, user); err != nil {
			return createPatch(err)
		}
		hasManagedUser = false
	}

	if shouldManageACLs {
		if err := syncer.Sync(ctx, user); err != nil {
			return createPatch(err)
		}
		hasManagedACLs = true
	}

	if !shouldManageACLs && hasManagedACLs {
		if err := syncer.DeleteAll(ctx, user); err != nil {
			return createPatch(err)
		}
		hasManagedACLs = false
	}

	return createPatch(nil)
}

func (r *UserReconciler) DeleteResource(ctx context.Context, request ResourceRequest[*redpandav1alpha2.User]) error {
	request.logger.V(2).Info("Deleting user data from cluster")

	user := request.object
	hasManagedACLs, hasManagedUser := user.HasManagedACLs(), user.HasManagedUser()

	usersClient, syncer, hasUser, err := r.userAndACLClients(ctx, request)
	if err != nil {
		return ignoreAllConnectionErrors(request.logger, err)
	}

	if hasUser && hasManagedUser {
		request.logger.V(2).Info("Deleting managed user")
		if err := usersClient.Delete(ctx, user); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	if hasManagedACLs {
		request.logger.V(2).Info("Deleting managed acls")
		if err := syncer.DeleteAll(ctx, user); err != nil {
			return ignoreAllConnectionErrors(request.logger, err)
		}
	}

	return nil
}

func (r *UserReconciler) userAndACLClients(ctx context.Context, request ResourceRequest[*redpandav1alpha2.User]) (*users.Client, *acls.Syncer, bool, error) {
	user := request.object
	usersClient, err := request.factory.Users(ctx, user, r.extraOptions...)
	if err != nil {
		return nil, nil, false, err
	}

	syncer, err := request.factory.ACLs(ctx, user, r.extraOptions...)
	if err != nil {
		return nil, nil, false, err
	}

	hasUser, err := usersClient.Has(ctx, user)
	if err != nil {
		return nil, nil, false, err
	}

	return usersClient, syncer, hasUser, nil
}

func SetupUserController(ctx context.Context, mgr ctrl.Manager) error {
	c := mgr.GetClient()
	config := mgr.GetConfig()
	factory := internalclient.NewFactory(config, c)
	controller := NewResourceController(c, factory, &UserReconciler{}, "UserReconciler")

	enqueueUser, err := registerClusterSourceIndex(ctx, mgr, "user", &redpandav1alpha2.User{}, &redpandav1alpha2.UserList{})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.User{}).
		Owns(&corev1.Secret{}).
		Watches(&redpandav1alpha2.Redpanda{}, enqueueUser).
		// Every 5 minutes try and check to make sure no manual modifications
		// happened on the resource synced to the cluster and attempt to correct
		// any drift.
		Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
