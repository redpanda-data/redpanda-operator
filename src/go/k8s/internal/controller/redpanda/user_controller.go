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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2ac "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/applyconfiguration/redpanda/v1alpha2"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

const (
	fieldOwner client.FieldOwner = "redpanda-operator"
)

// UserReconciler reconciles a Topic object
type UserReconciler struct {
	internalclient.ClientFactory
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
	if err := r.KubernetesClient().Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !user.DeletionTimestamp.IsZero() {
		// cleanup finalizer
		return ctrl.Result{}, r.KubernetesClient().ClearFinalizer(ctx, user, FinalizerKey)
	}

	config := redpandav1alpha2ac.User(user.Name, user.Namespace).WithFinalizers(FinalizerKey)
	if err := r.KubernetesClient().Apply(ctx, config, fieldOwner); err != nil {
		return ctrl.Result{}, err
	}

	status := redpandav1alpha2ac.UserStatus().WithConditions(updateStatusConditions(user.Status.Conditions, user.Generation, []metav1.Condition{{
		Type:    redpandav1alpha2.UserConditionTypeSynced,
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "reconciled",
	}})...)
	return ctrl.Result{}, r.KubernetesClient().ApplyStatus(ctx, config.WithStatus(status), fieldOwner)
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.User{}).
		Complete(r)
}

func updateStatusConditions(existing []metav1.Condition, generation int64, conditions []metav1.Condition) []*metav1ac.ConditionApplyConfiguration {
	now := metav1.Now()
	configurations := []*metav1ac.ConditionApplyConfiguration{}

	for _, condition := range conditions {
		existingCondition := apimeta.FindStatusCondition(existing, condition.Type)
		if existingCondition == nil {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Status != condition.Status {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Reason != condition.Reason {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.Message != condition.Message {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		if existingCondition.ObservedGeneration != generation {
			configurations = append(configurations, conditionToConfig(generation, now, condition))
			continue
		}

		configurations = append(configurations, conditionToConfig(generation, existingCondition.LastTransitionTime, *existingCondition))
	}

	return configurations
}

func conditionToConfig(generation int64, now metav1.Time, condition metav1.Condition) *metav1ac.ConditionApplyConfiguration {
	return metav1ac.Condition().
		WithType(condition.Type).
		WithStatus(condition.Status).
		WithReason(condition.Reason).
		WithObservedGeneration(generation).
		WithLastTransitionTime(now).
		WithMessage(condition.Message)
}
