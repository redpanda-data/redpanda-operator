// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package otelkube provides a [client.Client] and functions to propagate otel
// spans, traces, and baggage via Annotations on Kubernetes resources.
package otelkube

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PropagationOption interface {
	Apply(*PropagationOptions)
}

func WithTextMapPropagator(propagator propagation.TextMapPropagator) *PropagationOptions {
	return &PropagationOptions{
		TextMapPropagator: propagator,
	}
}

type PropagationOptions struct {
	TextMapPropagator propagation.TextMapPropagator
}

var _ PropagationOption = (*PropagationOptions)(nil)

func options(opts ...PropagationOption) PropagationOptions {
	defaults := PropagationOptions{
		TextMapPropagator: otel.GetTextMapPropagator(),
	}
	for _, opt := range opts {
		opt.Apply(&defaults)
	}
	return defaults
}

func (o *PropagationOptions) Apply(to *PropagationOptions) {
	if o.TextMapPropagator != nil {
		to.TextMapPropagator = o.TextMapPropagator
	}
}

// Inject set cross-cutting concerns from the [context.Context] into the carrier [client.Object].
// You probably want to use [NewClient] instead of manually injecting this information.
func Inject(ctx context.Context, obj client.Object, opts ...PropagationOption) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	options(opts...).TextMapPropagator.Inject(ctx, propagation.MapCarrier(annotations))

	obj.SetAnnotations(annotations)
}

// Extract reads cross-cutting concerns from the carrier [client.Object] into a [context.Context].
// Usage:
//
//	ctx, span := trace.Start(otelkube.Extract(ctx, rp), "Reconcile", trace.WithAttributes(
//		attribute.String("name", req.Name),
//		attribute.String("namespace", req.Namespace),
//	))
//	defer func() { trace.EndSpan(span, err) }()
func Extract(ctx context.Context, obj client.Object, opts ...PropagationOption) context.Context {
	return options(opts...).TextMapPropagator.Extract(ctx, propagation.MapCarrier(obj.GetAnnotations()))
}

func NewClient(c client.Client, opts ...PropagationOption) *tracingClient {
	return &tracingClient{Client: c, options: options(opts...)}
}

// tracingClient is a [client.Client] that propagates otel span and trace
// information through object annotations upon write requests. This client
// should be used by clients of an operator and not the operator itself.
// Operator's should use [Extract] to interact with spans and traces propagated
// via the tracingClient.
type tracingClient struct {
	client.Client
	options PropagationOptions
}

func (c *tracingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	Inject(ctx, obj)
	return c.Client.Create(ctx, obj, opts...)
}

func (c *tracingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	Inject(ctx, obj)
	return c.Client.Update(ctx, obj, opts...)
}

func (c *tracingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	Inject(ctx, obj)
	return c.Client.Patch(ctx, obj, patch, opts...)
}
