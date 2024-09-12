// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import "github.com/go-logr/logr"

type SetupError struct {
	inner   error
	message string
	args    []any
}

var _ error = (*SetupError)(nil)

func (s *SetupError) Error() string {
	return s.inner.Error()
}

func (s *SetupError) Log(logger logr.Logger) {
	logger.Error(s.inner, s.message, s.args...)
}

func setupError(err error, message string, args ...any) *SetupError {
	if err == nil {
		return nil
	}
	return &SetupError{
		inner:   err,
		message: message,
		args:    args,
	}
}
