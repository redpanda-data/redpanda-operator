// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type contextKey struct{}

var (
	testingContextKey contextKey = struct{}{}
	TerminationChan              = make(chan string)
)

type ExitBehavior string

const (
	ExitBehaviorNone             ExitBehavior = ""
	ExitBehaviorLog              ExitBehavior = "log"
	ExitBehaviorTerminateProgram ExitBehavior = "exit"
	ExitBehaviorTestFail         ExitBehavior = "fail"
)

// TestingOptions are configurable options for the testing environment
type TestingOptions struct {
	// RetainOnFailure tells the testing environment to retain
	// any provisioned resources in the case of a test failure.
	RetainOnFailure bool
	// KubectlOptions sets the options passed to the underlying
	// kubectl commands.
	KubectlOptions *KubectlOptions
	// Timeout is the timeout for any individual scenario to run.
	Timeout time.Duration
	// CleanupTimeout is the timeout for cleaning up resources
	CleanupTimeout time.Duration
	// Provider sets the provider for the test suite.
	Provider string
	// SchemeRegisterers sets scheme registration functions for the controller-runtime client
	SchemeRegisterers []func(s *runtime.Scheme) error
	// ExitBehavior tells the test what to do if a test fails
	ExitBehavior ExitBehavior
}

func (o *TestingOptions) Clone() *TestingOptions {
	return &TestingOptions{
		RetainOnFailure:   o.RetainOnFailure,
		Timeout:           o.Timeout,
		CleanupTimeout:    o.CleanupTimeout,
		KubectlOptions:    o.KubectlOptions.Clone(),
		Provider:          o.Provider,
		SchemeRegisterers: o.SchemeRegisterers,
		ExitBehavior:      o.ExitBehavior,
	}
}

// TestingT is a wrapper around godog's test implementation that
// itself wraps go's stdlib testing.T. It adds some helpers on top
// of godog and implements some of the functionality that godog doesn't
// expose when wrapping testing.T (i.e. Cleanup methods) as well as
// acting as an entry point for initializing connections to infrastructure
// resources.
type TestingT struct {
	godog.TestingT
	client.Client
	*Cleaner

	lastError     string
	activeSubtest *TestingT
	restConfig    *rest.Config
	options       *TestingOptions
	failure       bool
}

func NewTesting(ctx context.Context, options *TestingOptions, cleaner *Cleaner) *TestingT {
	t := godog.T(ctx)

	client, err := kubernetesClient(options.SchemeRegisterers, options.KubectlOptions)
	require.NoError(t, err)

	restConfig, err := restConfig(options.KubectlOptions)
	require.NoError(t, err)

	return &TestingT{
		TestingT:   t,
		Client:     client,
		Cleaner:    cleaner,
		restConfig: restConfig,
		options:    options,
	}
}

// T pulls a TestingT from the given context.
func T(ctx context.Context) *TestingT {
	return ctx.Value(testingContextKey).(*TestingT)
}

func (t *TestingT) IntoContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, testingContextKey, t)
}

func (t *TestingT) Cleanup(fn func(context.Context)) {
	t.Cleaner.Cleanup(fn)
}

func (t *TestingT) RestConfig() *rest.Config {
	return rest.CopyConfig(t.restConfig)
}

// IsFailure returns whether this test failed.
func (t *TestingT) IsFailure() bool {
	return t.failure
}

func (t *TestingT) PropagateError(isFirst bool, subtest *TestingT) {
	t.activeSubtest = subtest
	if !isFirst && t.PriorError() != "" {
		subtest.Error("Error in parent Feature setup: " + t.PriorError())
	}
}

// PriorError returns the last arguments previously passed to ErrorF.
func (t *TestingT) PriorError() string {
	return t.lastError
}

// Error fails the current test and logs the provided arguments. Equivalent to calling Log then
// Fail.
func (t *TestingT) Error(args ...interface{}) {
	t.failure = true
	t.lastError = fmt.Sprint(args...)
	if t.activeSubtest != nil {
		t.activeSubtest.Error(args...)
	}
	t.TestingT.Error(args...)
}

// Errorf fails the current test and logs the formatted message. Equivalent to calling Logf then
// Fail.
func (t *TestingT) Errorf(format string, args ...interface{}) {
	t.failure = true
	t.lastError = fmt.Sprintf(format, args...)
	if t.activeSubtest != nil {
		t.activeSubtest.Errorf(format, args...)
	}
	t.TestingT.Errorf(format, args...)
}

// Fail marks the current test as failed, but does not halt execution of the step.
func (t *TestingT) Fail() {
	t.failure = true
	t.TestingT.Fail()
}

// FailNow marks the current test as failed and halts execution of the step.
func (t *TestingT) FailNow() {
	t.failure = true
	t.TestingT.FailNow()
}

// Fatal logs the provided arguments, marks the test as failed and halts execution of the step.
func (t *TestingT) Fatal(args ...interface{}) {
	t.failure = true
	t.lastError = fmt.Sprint(args...)
	if t.activeSubtest != nil {
		t.activeSubtest.Error(args...)
	}
	t.TestingT.Fatal(args...)
}

// Fatalf logs the formatted message, marks the test as failed and halts execution of the step.
func (t *TestingT) Fatalf(format string, args ...interface{}) {
	t.failure = true
	t.lastError = fmt.Sprintf(format, args...)
	if t.activeSubtest != nil {
		t.activeSubtest.Errorf(format, args...)
	}
	t.TestingT.Fatalf(format, args...)
}

// ApplyManifest applies a set of kubernetes manifests via kubectl.
func (t *TestingT) ApplyManifest(ctx context.Context, fileOrDirectory string) {
	namespace := t.options.KubectlOptions.Namespace

	opts := t.options.KubectlOptions.Clone()
	opts.Namespace = namespace

	t.Logf("Applying manifest %q", fileOrDirectory)
	_, err := KubectlApply(ctx, fileOrDirectory, opts)
	require.NoError(t, err)

	t.Cleanup(func(ctx context.Context) {
		t.Logf("Deleting manifest %q", fileOrDirectory)
		_, err := KubectlDelete(ctx, fileOrDirectory, opts)
		require.NoError(t, err)
	})
}

// ApplyFixture applies a set of kubernetes manifests via kubectl.
func (t *TestingT) ApplyFixture(ctx context.Context, fileOrDirectory string) {
	t.ApplyManifest(ctx, filepath.Join("fixtures", fileOrDirectory))
}

// ResourceKey returns a types.NamespaceName that can be used with the Kubernetes client,
// but scoped to the namespace given by the underlying KubectlOptions.
func (t *TestingT) ResourceKey(name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: t.Namespace(),
		Name:      name,
	}
}

// Namespace returns the namespace that this testing scenario is running in.
func (t *TestingT) Namespace() string {
	return t.options.KubectlOptions.Namespace
}

// SetNamespace sets the current namespace we're in.
func (t *TestingT) SetNamespace(namespace string) {
	t.options.KubectlOptions.Namespace = namespace
}

// IsolateNamespace creates a temporary namespace for the tests in this scope to run.
func (t *TestingT) IsolateNamespace(ctx context.Context) string {
	namespace := AddRandomSuffixTo("testing")

	require.NoError(t, createNamespace(ctx, t.options.SchemeRegisterers, namespace, t.options.KubectlOptions))

	oldNamespace := t.options.KubectlOptions.Namespace
	t.options.KubectlOptions.Namespace = namespace
	t.Logf("Switching to newly created namespace %q --> %q", oldNamespace, namespace)

	t.Cleanup(func(ctx context.Context) {
		require.NoError(t, deleteNamespace(ctx, t.options.SchemeRegisterers, namespace, t.options.KubectlOptions))
		t.options.KubectlOptions.Namespace = oldNamespace
		t.Logf("Switching namespaces after deleting %q --> %q", namespace, oldNamespace)
	})

	return namespace
}

// RequireCondition expects some condition to be found in an array of object conditions.
func (t *TestingT) RequireCondition(expected metav1.Condition, conditions []metav1.Condition) {
	if !t.HasCondition(expected, conditions) {
		t.Fatalf("condition: %+v not found in conditions: %+v", expected, conditions)
	}
}

// HasCondition returns if a condition is found in an array of object conditions.
func (t *TestingT) HasCondition(expected metav1.Condition, conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if expected.Type == condition.Type && expected.Status == condition.Status && expected.Reason == condition.Reason {
			return true
		}
	}
	return false
}
