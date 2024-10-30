// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"errors"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
)

var (
	configurationErrors = []error{
		ErrEmptyBrokerList, ErrEmptyURLList, ErrInvalidKafkaClientObject,
		ErrInvalidRedpandaClientObject, ErrUnsupportedSASLMechanism,
	}

	// For a list of errors from the Kafka API see:
	//
	// https://github.com/twmb/franz-go/blob/b77dd13e2bfaee7f5181df27b40ee4a4f6a73b09/pkg/kerr/kerr.go#L76-L192
	terminalClientErrors = []error{
		kerr.UnsupportedSaslMechanism, kerr.InvalidRequest, kerr.PolicyViolation,
		kerr.SecurityDisabled, kerr.SaslAuthenticationFailed, kerr.InvalidPrincipalType,
	}
)

// IsConfigurationError returns whether this error stems from some
// generic invalid condition within a connection's configuration.
func IsConfigurationError(err error) bool {
	for _, configuration := range configurationErrors {
		if errors.Is(err, configuration) {
			return true
		}
	}

	return false
}

// IsInvalidClusterError returns whether this error stems from some
// invalid condition with a ClusterRef configuration block.
func IsInvalidClusterError(err error) bool {
	return errors.Is(err, ErrInvalidClusterRef)
}

// IsTerminalClientError returns whether or not the error comes
// from a terminal error from a failed API request by one of our clients.
func IsTerminalClientError(err error) bool {
	for _, terminal := range terminalClientErrors {
		if errors.Is(err, terminal) {
			return true
		}
	}

	// For our REST API we check to see if we have a 400 range
	// response, which shouldn't be retried.
	var restError *rpadmin.HTTPResponseError
	if errors.As(err, &restError) {
		code := restError.Response.StatusCode
		if code >= 400 && code < 500 {
			// we have a terminal error
			return true
		}
	}

	var srError *sr.ResponseError
	if errors.As(err, &srError) {
		// from https://github.com/redpanda-data/redpanda/blob/8a12c560f73773d2b5606d35f3b585c7af67ca82/src/v/pandaproxy/error.h#L70-L72
		code := srError.ErrorCode / 100
		if code >= 400 && code < 500 {
			// we have a terminal error
			return true
		}
	}

	// alternatively, we may also have an error immediately for SASL auth
	var readEOFError *kgo.ErrFirstReadEOF

	return errors.As(err, &readEOFError)
}
