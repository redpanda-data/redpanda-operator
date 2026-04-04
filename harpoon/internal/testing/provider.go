// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import "context"

type providerContext struct{}

var providerContextKey = providerContext{}

// Provider is the internal interface that a provider
// must implement to be consumed by harpoon.
type Provider interface {
	PartialProvider

	Initialize() error
	Setup(ctx context.Context) error
	Teardown(ctx context.Context) error
	GetBaseContext() context.Context
}

// ProvisionedProvider is a provider that actually provisions
// a cluster for you and writes a kube config file to disk
// providing it at the path returned by ConfigPath()
type ProvisionedProvider interface {
	Provider

	ConfigPath() string
}

// PartialProvider is the provider interface exposed to
// running tests. It is a subset of the full provider and
// intentionally only exposes some interfaces that are
// safe to call during tests.
type PartialProvider interface {
	DeleteNode(ctx context.Context, name string) error
	AddNode(ctx context.Context, name string) error
	LoadImages(ctx context.Context, images []string) error
}

func ProviderIntoContext(ctx context.Context, provider Provider) context.Context {
	return context.WithValue(ctx, providerContextKey, provider)
}
