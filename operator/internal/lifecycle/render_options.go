// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

// RenderOption adjusts how the simple-resource renderers shape a cluster's
// resources for operator-wide features that no cluster spec expresses.
type RenderOption func(*renderOptions)

type renderOptions struct {
	endpointSteering bool
}

func applyRenderOptions(opts []RenderOption) renderOptions {
	var options renderOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// WithEndpointSteering renders every cluster's internal Service for the
// operator's endpoint steering controller, which publishes the Service's
// EndpointSlices per port: the Service carries the controller's annotation
// and no selector, so the native controller stays out of it.
func WithEndpointSteering(enabled bool) RenderOption {
	return func(options *renderOptions) {
		options.endpointSteering = enabled
	}
}
