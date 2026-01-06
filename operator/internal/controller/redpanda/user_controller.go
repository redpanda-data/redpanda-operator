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
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/users"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

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
	defer usersClient.Close()
	defer syncer.Close()

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
	defer usersClient.Close()
	defer syncer.Close()

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

func SetupUserController(ctx context.Context, mgr multicluster.Manager, expander *secrets.CloudExpander, includeV1, includeV2 bool) error {
	factory := internalclient.NewFactory(mgr, expander)

	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.User{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).
		Owns(&corev1.Secret{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))

	for _, clusterName := range mgr.GetClusterNames() {
		if includeV1 {
			enqueueV1User, err := controller.RegisterV1ClusterSourceIndex(ctx, mgr, "user_v1", clusterName, &redpandav1alpha2.User{}, &redpandav1alpha2.UserList{})
			if err != nil {
				return err
			}
			builder.Watches(&vectorizedv1alpha1.Cluster{}, enqueueV1User, controller.WatchOptions(clusterName)...)
		}

		if includeV2 {
			enqueueV2User, err := controller.RegisterClusterSourceIndex(ctx, mgr, "user", clusterName, &redpandav1alpha2.User{}, &redpandav1alpha2.UserList{})
			if err != nil {
				return err
			}
			builder.Watches(&redpandav1alpha2.Redpanda{}, enqueueV2User, controller.WatchOptions(clusterName)...)
		}
	}

	controller := NewResourceController(mgr, factory, &UserReconciler{}, "UserReconciler")

	// Every 5 minutes try and check to make sure no manual modifications
	// happened on the resource synced to the cluster and attempt to correct
	// any drift.
	return builder.Complete(controller.PeriodicallyReconcile(5 * time.Minute))
}
