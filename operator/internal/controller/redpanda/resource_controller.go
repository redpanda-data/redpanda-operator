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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

const fieldOwner client.FieldOwner = "redpanda-operator"

type Resource[T any] interface {
	*T
	client.Object
}

type ResourceRequest[T client.Object] struct {
	factory internalclient.ClientFactory
	logger  logr.Logger
	object  T
}

type ResourceReconciler[T client.Object] interface {
	FinalizerPatch(request ResourceRequest[T]) client.Patch
	SyncResource(ctx context.Context, request ResourceRequest[T]) (client.Patch, error)
	DeleteResource(ctx context.Context, request ResourceRequest[T]) error
}

type ResourceController[T any, U Resource[T]] struct {
	client.Client
	internalclient.ClientFactory

	reconciler      ResourceReconciler[U]
	name            string
	periodicTimeout time.Duration
}

func NewResourceController[T any, U Resource[T]](c client.Client, factory internalclient.ClientFactory, reconciler ResourceReconciler[U], name string) *ResourceController[T, U] {
	return &ResourceController[T, U]{
		Client:        c,
		ClientFactory: factory,
		reconciler:    reconciler,
		name:          name,
	}
}

func (r *ResourceController[T, U]) PeriodicallyReconcile(timeout time.Duration) *ResourceController[T, U] {
	r.periodicTimeout = timeout
	return r
}

func (r *ResourceController[T, U]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName(fmt.Sprintf("%s.Reconcile", r.name))
	l.V(1).Info("Starting reconcile loop")
	start := time.Now()
	defer func() {
		l.V(1).Info("Finished reconciling", "elapsed", time.Since(start))
	}()

	object := U(new(T))
	if err := r.Get(ctx, req.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	request := ResourceRequest[U]{
		factory: r.ClientFactory,
		logger:  l,
		object:  object,
	}

	if !object.GetDeletionTimestamp().IsZero() {
		if err := r.reconciler.DeleteResource(ctx, request); err != nil {
			return ctrl.Result{}, err
		}
		if controllerutil.RemoveFinalizer(object, FinalizerKey) {
			return ctrl.Result{}, r.Update(ctx, object)
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(object, FinalizerKey) {
		patch := r.reconciler.FinalizerPatch(request)
		if patch != nil {
			if err := r.Patch(ctx, object, patch, client.ForceOwnership, fieldOwner); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	patch, err := r.reconciler.SyncResource(ctx, request)
	var syncError error
	if patch != nil {
		syncError = r.Status().Patch(ctx, object, patch, client.ForceOwnership, fieldOwner)
	}

	result := ctrl.Result{}
	if r.periodicTimeout != 0 {
		result.RequeueAfter = r.periodicTimeout
	}

	return result, errors.Join(err, syncError)
}

func ignoreAllConnectionErrors(logger logr.Logger, err error) error {
	// If we have known errors where we're unable to actually establish
	// a connection to the cluster due to say, invalid connection parameters
	// we're going to just skip the cleanup phase since we likely won't be
	// able to clean ourselves up anyway.
	if internalclient.IsTerminalClientError(err) ||
		internalclient.IsConfigurationError(err) ||
		internalclient.IsInvalidClusterError(err) {
		// We use Info rather than Error here because we don't want
		// to ignore the verbosity settings. This is really only for
		// debugging purposes.
		logger.V(2).Info("Ignoring non-retryable client error", "error", err)
		return nil
	}
	return err
}

func handleResourceSyncErrors(err error) (metav1.Condition, error) {
	// If we have a known terminal error, just set the sync condition and don't re-run reconciliation.
	if internalclient.IsInvalidClusterError(err) {
		return redpandav1alpha2.ResourceNotSyncedCondition(redpandav1alpha2.ResourceConditionReasonClusterRefInvalid, err), nil
	}
	if internalclient.IsConfigurationError(err) {
		return redpandav1alpha2.ResourceNotSyncedCondition(redpandav1alpha2.ResourceConditionReasonConfigurationInvalid, err), nil
	}
	if internalclient.IsTerminalClientError(err) {
		return redpandav1alpha2.ResourceNotSyncedCondition(redpandav1alpha2.ResourceConditionReasonTerminalClientError, err), nil
	}

	// otherwise, set a generic unexpected error and return an error so we can re-reconcile.
	return redpandav1alpha2.ResourceNotSyncedCondition(redpandav1alpha2.ResourceConditionReasonUnexpectedError, err), err
}
