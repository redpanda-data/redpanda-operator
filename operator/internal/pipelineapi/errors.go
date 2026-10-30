// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipelineapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

func newInvalidArgument(field, description string) *connect.Error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s: %s", field, description))
}

func newNotManagedError(name string) *connect.Error {
	return connect.NewError(connect.CodeFailedPrecondition,
		fmt.Errorf("pipeline %q is managed outside Console (for example with kubectl or GitOps) and is read-only here", name))
}

func newUnmanageableFieldsError(name string, fields []string) *connect.Error {
	return connect.NewError(connect.CodeFailedPrecondition,
		fmt.Errorf("pipeline %s uses fields Console can't manage: %s; manage it with kubectl", name, strings.Join(fields, ", ")))
}

// kubeError maps a Kubernetes API error to a connect error. Validation and
// conflict messages pass through; anything else is logged here and returned
// without detail, so the caller never sees API server internals.
func (s *Service) kubeError(ctx context.Context, err error, msg, pipelineID string) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, errors.New(msg+": request canceled"))
	}

	var status k8serrors.APIStatus
	if !errors.As(err, &status) {
		s.logger.ErrorContext(ctx, msg, slog.String("pipeline_id", pipelineID), slog.Any("error", err))
		code := connect.CodeInternal
		if errors.Is(err, context.DeadlineExceeded) {
			code = connect.CodeDeadlineExceeded
		}
		return connect.NewError(code, errors.New(msg+": the Kubernetes API request failed"))
	}

	code := codeFromKubernetesError(err)
	switch {
	case k8serrors.IsResourceExpired(err), k8serrors.IsGone(err):
		return connect.NewError(code, errors.New(msg+": listing expired, restart from the first page"))
	case k8serrors.IsInvalid(err), k8serrors.IsBadRequest(err), k8serrors.IsConflict(err),
		k8serrors.IsNotFound(err), k8serrors.IsAlreadyExists(err):
		return connect.NewError(code, fmt.Errorf("%s: %s", msg, status.Status().Message))
	}
	s.logger.ErrorContext(ctx, msg, slog.String("pipeline_id", pipelineID), slog.Any("error", err))
	return connect.NewError(code, fmt.Errorf("%s: the Kubernetes API returned %s", msg, status.Status().Reason))
}

func codeFromKubernetesError(err error) connect.Code {
	switch {
	case k8serrors.IsUnauthorized(err):
		return connect.CodeUnauthenticated
	case k8serrors.IsForbidden(err):
		return connect.CodePermissionDenied
	case k8serrors.IsNotFound(err):
		return connect.CodeNotFound
	case k8serrors.IsAlreadyExists(err):
		return connect.CodeAlreadyExists
	case k8serrors.IsConflict(err), k8serrors.IsResourceExpired(err), k8serrors.IsGone(err):
		return connect.CodeAborted
	case k8serrors.IsInvalid(err), k8serrors.IsBadRequest(err):
		return connect.CodeInvalidArgument
	case k8serrors.IsTooManyRequests(err):
		return connect.CodeResourceExhausted
	case k8serrors.IsTimeout(err), k8serrors.IsServerTimeout(err):
		return connect.CodeDeadlineExceeded
	case k8serrors.IsServiceUnavailable(err):
		return connect.CodeUnavailable
	default:
		return connect.CodeInternal
	}
}
