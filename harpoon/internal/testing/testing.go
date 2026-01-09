// Copyright 2026 Redpanda Data, Inc.
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
	"os"
	"path/filepath"
	"time"

	"github.com/cucumber/godog"
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type testingContext struct{}

type TerminationError struct {
	Message  string
	NeedsLog bool
}

var (
	testingContextKey = testingContext{}
	TerminationChan   = make(chan TerminationError)
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
	// Images says the base images to import any time a new node comes up
	Images []string

	variant string
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
		Images:            o.Images,
		variant:           o.variant,
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

	helmClient    *helm.Client
	lastError     string
	activeSubtest *TestingT
	restConfig    *rest.Config
	options       *TestingOptions
	failure       bool
	messagePrefix string
}

func NewTesting(ctx context.Context, options *TestingOptions, cleaner *Cleaner) *TestingT {
	t := godog.T(ctx)

	client, err := kubernetesClient(options.SchemeRegisterers, options.KubectlOptions)
	require.NoError(t, err)

	restConfig, err := restConfig(options.KubectlOptions)
	require.NoError(t, err)

	helmClient, err := helm.New(helm.Options{
		KubeConfig: rest.CopyConfig(restConfig),
	})
	require.NoError(t, err)

	return &TestingT{
		TestingT:   t,
		Client:     client,
		Cleaner:    cleaner,
		helmClient: helmClient,
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

func (t *TestingT) Provider(ctx context.Context) PartialProvider {
	return ctx.Value(providerContextKey).(PartialProvider)
}

func (t *TestingT) SetMessagePrefix(prefix string) {
	t.messagePrefix = prefix
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
		subtest.Error(t.PriorError())
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
	message := t.messagePrefix + fmt.Sprint(args...)
	t.lastError = message
	if t.activeSubtest != nil {
		t.activeSubtest.Error(message)
	}
	t.TestingT.Error(message)
}

// Errorf fails the current test and logs the formatted message. Equivalent to calling Logf then
// Fail.
func (t *TestingT) Errorf(format string, args ...interface{}) {
	t.failure = true
	message := t.messagePrefix + fmt.Sprintf(format, args...)
	t.lastError = message
	if t.activeSubtest != nil {
		t.activeSubtest.Error(message)
	}
	t.TestingT.Error(message)
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
	message := t.messagePrefix + fmt.Sprint(args...)
	t.lastError = message
	if t.activeSubtest != nil {
		t.activeSubtest.Error(message)
	}
	t.TestingT.Fatal(message)
}

// Fatalf logs the formatted message, marks the test as failed and halts execution of the step.
func (t *TestingT) Fatalf(format string, args ...interface{}) {
	t.failure = true
	message := t.messagePrefix + fmt.Sprintf(format, args...)
	t.lastError = message
	if t.activeSubtest != nil {
		t.activeSubtest.Error(message)
	}
	t.TestingT.Fatal(message)
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
		output, err := KubectlDelete(ctx, fileOrDirectory, opts)
		require.NoError(t, err)
		t.Logf("Deletion finished: %s", output)
	})
}

// ApplyFixture applies a set of kubernetes manifests via kubectl.
func (t *TestingT) ApplyFixture(ctx context.Context, fileOrDirectory string) {
	t.ApplyManifest(ctx, filepath.Join("fixtures", fileOrDirectory))
}

// Delete a node from the Kube API server
func (t *TestingT) DeleteNode(ctx context.Context, name string) {
	t.Logf("Deleting node %q in Kubernetes", name)
	require.NoError(t, t.Delete(ctx, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}))
}

// Shutdown a node until the end of a test and then bring it back
func (t *TestingT) ShutdownNode(ctx context.Context, name string) {
	provider := t.Provider(ctx)

	t.Logf("Deleting provider node %q", name)
	require.NoError(t, provider.DeleteNode(ctx, name))
	t.Cleanup(func(ctx context.Context) {
		t.Logf("Recreating deleted provider node %q", name)
		require.NoError(t, provider.AddNode(ctx, name))
		require.NoError(t, provider.LoadImages(ctx, t.options.Images))
	})
}

// Add a new node to the cluster temporarily until the end of the test
// when it will be deleted
func (t *TestingT) AddNode(ctx context.Context, name string) {
	provider := t.Provider(ctx)

	t.Logf("Adding temporary node %q", name)
	require.NoError(t, provider.AddNode(ctx, name))
	require.NoError(t, provider.LoadImages(ctx, t.options.Images))

	t.Cleanup(func(ctx context.Context) {
		t.Logf("Deleting temporary node %q", name)
		require.NoError(t, provider.DeleteNode(ctx, name))
		require.NoError(t, t.Delete(ctx, &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}))
	})
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

// MarkVariant marks the test with a variant tag that can be fetched later on.
func (t *TestingT) MarkVariant(variant string) {
	oldVariant := t.options.variant
	t.Logf("Marking test as variant %q", variant)
	t.options.variant = variant

	t.Cleanup(func(ctx context.Context) {
		t.options.variant = oldVariant
	})
}

// Variant retrieves the testing variant.
func (t *TestingT) Variant() string {
	return t.options.variant
}

// VCluster creates a vcluster instance and sets up the test routines to use it.
func (t *TestingT) VCluster(ctx context.Context) string {
	cluster, err := vcluster.New(ctx, t.restConfig)
	require.NoError(t, err)

	configPath, err := os.CreateTemp("", "vcluster.yaml")
	require.NoError(t, err)
	// we use background context here so that the proxy doesn't die after the
	// context changes
	proxyCtx, proxyCancel := context.WithCancel(context.Background())
	restConfig, err := cluster.PortForwardedRESTConfig(proxyCtx)
	require.NoError(t, err)
	require.NoError(t, kube.WriteToFile(kube.RestToConfig(restConfig), configPath.Name()))

	oldOptions := t.options.KubectlOptions
	oldClient := t.Client

	newOptions := &KubectlOptions{
		ConfigPath: configPath.Name(),
		Namespace:  "default",
		Env:        make(map[string]string),
	}

	for k, v := range oldOptions.Env {
		newOptions.Env[k] = v
	}
	newClient, err := kubernetesClient(t.options.SchemeRegisterers, t.options.KubectlOptions)
	require.NoError(t, err)

	t.options.KubectlOptions = newOptions
	t.Client = newClient

	t.Logf("Switching to newly created vcluster %q", cluster.Name())

	t.Cleanup(func(ctx context.Context) {
		proxyCancel()
		require.NoError(t, cluster.Delete())
		t.options.KubectlOptions = oldOptions
		t.Client = oldClient
		require.NoError(t, os.RemoveAll(configPath.Name()))
		t.Logf("Switching from vcluster %q", cluster.Name())
	})

	return cluster.Name()
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
