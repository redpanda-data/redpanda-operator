package testing

import "context"

type providerContext struct{}

var providerContextKey = providerContext{}

type Provider interface {
	PartialProvider

	Initialize() error
	Setup(ctx context.Context) error
	Teardown(ctx context.Context) error
	GetBaseContext() context.Context
}

type ProvisionedProvider interface {
	Provider

	ConfigPath() string
}

type PartialProvider interface {
	DeleteNode(ctx context.Context, name string) error
	AddNode(ctx context.Context, name string) error
	LoadImages(ctx context.Context, images []string) error
}

func ProviderIntoContext(ctx context.Context, provider Provider) context.Context {
	return context.WithValue(ctx, providerContextKey, provider)
}
