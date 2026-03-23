// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type filteringReconciler struct {
	namespace  string
	reconciler reconcile.Reconciler
}

func FilterNamespaceReconciler(namespace string, reconciler reconcile.Reconciler) reconcile.Reconciler {
	return &filteringReconciler{
		namespace:  namespace,
		reconciler: reconciler,
	}
}

func (r *filteringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.namespace != "" && req.Namespace != r.namespace {
		// if we don't match the given namespace, just no-op
		return ctrl.Result{}, nil
	}
	return r.reconciler.Reconcile(ctx, req)
}
