// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"context"

	"github.com/cucumber/godog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

// Redefine the interfaces from the internal package

type TagHandler func(ctx context.Context, t TestingT, arguments ...string) context.Context

type (
	Provider     = internaltesting.PartialProvider
	FullProvider = internaltesting.Provider
)

type ParsedTag struct {
	Name      string
	Arguments []string
}

type TestingT interface {
	godog.TestingT
	client.Client

	Provider(ctx context.Context) Provider

	Cleanup(fn func(context.Context))
	ResourceKey(name string) types.NamespacedName

	ApplyFixture(ctx context.Context, fileOrDirectory string)
	ApplyManifest(ctx context.Context, fileOrDirectory string)

	ShutdownNode(ctx context.Context, name string)
	DeleteNode(ctx context.Context, name string)
	AddNode(ctx context.Context, name string)

	IsolateNamespace(ctx context.Context) string

	InstallHelmChart(ctx context.Context, url, repo, chart string, options helm.InstallOptions)
	UpgradeHelmChart(ctx context.Context, repo, chart, release string, options helm.UpgradeOptions)
	InstallLocalHelmChart(ctx context.Context, path string, options helm.InstallOptions, deps ...helm.Dependency)
	UpgradeLocalHelmChart(ctx context.Context, path, release string, options helm.UpgradeOptions)

	Namespace() string
	RestConfig() *rest.Config

	RequireCondition(expected metav1.Condition, conditions []metav1.Condition)
	HasCondition(expected metav1.Condition, conditions []metav1.Condition) bool
}

func T(ctx context.Context) TestingT {
	return internaltesting.T(ctx)
}

var NoopProvider = &noopProvider{}

type noopProvider struct{}

func (n *noopProvider) Initialize() error {
	return nil
}

func (n *noopProvider) Setup(ctx context.Context) error {
	return nil
}

func (n *noopProvider) Teardown(ctx context.Context) error {
	return nil
}

func (n *noopProvider) LoadImages(ctx context.Context, images []string) error {
	return nil
}

func (n *noopProvider) GetBaseContext() context.Context {
	return context.Background()
}

func (n *noopProvider) DeleteNode(ctx context.Context, name string) error {
	return nil
}

func (n *noopProvider) AddNode(ctx context.Context, name string) error {
	return nil
}
