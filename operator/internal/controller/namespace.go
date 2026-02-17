package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type filteringReconciler struct {
	namespace  string
	reconciler reconcile.TypedReconciler[mcreconcile.Request]
}

func FilterNamespaceReconciler(namespace string, reconciler reconcile.TypedReconciler[mcreconcile.Request]) reconcile.TypedReconciler[mcreconcile.Request] {
	return &filteringReconciler{
		namespace:  namespace,
		reconciler: reconciler,
	}
}

func (r *filteringReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	if r.namespace != "" && req.Namespace != r.namespace {
		// if we don't match the given namespace, just no-op
		return ctrl.Result{}, nil
	}
	return r.reconciler.Reconcile(ctx, req)
}
