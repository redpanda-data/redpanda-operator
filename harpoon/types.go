// Copyright 2024 Redpanda Data, Inc.
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/helm-charts/pkg/helm"
	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Redefine the interfaces from the internal package

type TagHandler func(ctx context.Context, t TestingT, arguments ...string) context.Context

type TestingT interface {
	godog.TestingT
	client.Client

	Cleanup(fn func(context.Context))
	ResourceKey(name string) types.NamespacedName

	ApplyFixture(ctx context.Context, fileOrDirectory string)
	ApplyManifest(ctx context.Context, fileOrDirectory string)

	IsolateNamespace(ctx context.Context) string

	InstallHelmChart(ctx context.Context, url, repo, chart string, options helm.InstallOptions)

	Namespace() string
	RestConfig() *rest.Config

	RequireCondition(expected metav1.Condition, conditions []metav1.Condition)
	HasCondition(expected metav1.Condition, conditions []metav1.Condition) bool
}

type Provider interface {
	Initialize() error
	Setup(ctx context.Context) error
	Teardown(ctx context.Context) error
	GetBaseContext() context.Context
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

func (n *noopProvider) GetBaseContext() context.Context {
	return context.Background()
}
