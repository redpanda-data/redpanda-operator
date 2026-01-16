// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"context"
	"fmt"

	"github.com/cockroachdb/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager sets up the webhook with the manager
func (r *RedpandaRole) SetupWebhookWithManager(mgr ctrl.Manager) error {
	validator := &roleValidator{client: mgr.GetClient()}
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(validator).
		Complete()
}

// roleValidator handles validation for RedpandaRole resources
// This is intentionally not exported to avoid controller-gen issues
type roleValidator struct {
	client client.Client
}

//+kubebuilder:webhook:path=/validate-redpandarole,mutating=false,failurePolicy=fail,sideEffects=None,groups=cluster.redpanda.com,resources=redpandaroles,verbs=create;update,versions=v1alpha2,name=vredpandarole.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.CustomValidator = &roleValidator{}

// ValidateCreate implements webhook.CustomValidator for the new validator pattern
func (v *roleValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	role, ok := obj.(*RedpandaRole)
	if !ok {
		return nil, errors.Newf("expected a RedpandaRole but got a %T", obj)
	}

	if allErrs := v.validateEffectiveRoleNameUniqueness(ctx, role, nil); len(allErrs) > 0 {
		return nil, apierrors.NewInvalid(role.GroupVersionKind().GroupKind(), role.Name, allErrs)
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator for the new validator pattern
func (v *roleValidator) ValidateUpdate(ctx context.Context, old runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	oldRole, ok := old.(*RedpandaRole)
	if !ok {
		return nil, errors.Newf("expected a RedpandaRole but got a %T", old)
	}

	newRole, ok := newObj.(*RedpandaRole)
	if !ok {
		return nil, errors.Newf("expected a RedpandaRole but got a %T", newObj)
	}

	// Don't validate if the role is being deleted
	if !newRole.GetDeletionTimestamp().IsZero() {
		return nil, nil
	}

	if allErrs := v.validateEffectiveRoleNameUniqueness(ctx, newRole, oldRole); len(allErrs) > 0 {
		return nil, apierrors.NewInvalid(newRole.GroupVersionKind().GroupKind(), newRole.Name, allErrs)
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator for the new validator pattern
func (v *roleValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateEffectiveRoleNameUniqueness checks that no two roles have the same effective role name.
func (v *roleValidator) validateEffectiveRoleNameUniqueness(ctx context.Context, role *RedpandaRole, oldRole *RedpandaRole) field.ErrorList {
	internalRoleNamePath := field.NewPath("spec").Child("internalRoleName")

	if v.client == nil {
		return field.ErrorList{field.InternalError(internalRoleNamePath, fmt.Errorf("validation client not available"))}
	}

	var roleList RedpandaRoleList
	if err := v.client.List(ctx, &roleList, client.InNamespace(role.Namespace)); err != nil {
		return field.ErrorList{field.InternalError(internalRoleNamePath, fmt.Errorf("cannot validate effective role name uniqueness: %w", err))}
	}

	currentEffectiveRoleName := role.GetEffectiveRoleName()

	for _, existingRole := range roleList.Items {
		// Skip self-comparison (handles both updates via oldRole and creates via role.UID)
		if existingRole.UID == role.UID || (oldRole != nil && existingRole.UID == oldRole.UID) {
			continue
		}

		if currentEffectiveRoleName == existingRole.GetEffectiveRoleName() {
			return field.ErrorList{field.Invalid(
				internalRoleNamePath,
				role.Spec.InternalRoleName,
				fmt.Sprintf("effective role name conflicts with existing role %q", existingRole.Name),
			)}
		}
	}

	return nil
}
