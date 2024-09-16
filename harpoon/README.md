# Harpoon

Tools to kill a `kuttl`(fish).

## Description

Harpoon is a set of BDD lifecycle and Kubernetes helper wrappers around [github.com/cucumber/godog](https://github.com/cucumber/godog).

## Extensions

### Lifecycle Hooks

Stock `godog` explicitly [removed](https://github.com/cucumber/godog/issues/335) support for any sort of feature-based hooking of their test suite steps. This makes it very difficult to have a "medium" level of isolation where some expensive to provision component is shared between features, but isolated from the rest of the suite.

Currently the only way of adding in feature-based hooking is to hook into a `formatters` interface implementation that gets called when a new feature is started and reference counting the number of scenarios that will be run, and then detecting when all of those features finish.

### Testing Cleanup

The way that `godog` passes access to `testing.T` is weird. It passes an optional `context.Context` to every test step and you can pull a `testing.T`-like interface from the context using `godog.T(ctx)`. Unfortunately this interface doesn't have any sort of "lifecyle" helpers. Namely it doesn't expose a `Cleanup` method. This library wraps `godog.TestingT` with a cleanup manager that allows you to call teardown code on a per-scenario or per-feature basis.

### Tags

This library (ab)uses Gherkin tags for a couple of purposes:

1. Test selection based on the concept of providers. This allows you to specify which tests run/do not run on a per-environment basis. If some features are not yet supported, or at least not yet tested on say an EKS environment, you can skip those tests when specifying a test run via `-provider eks`. This is accomplished by looking for tags in the format of `@skip:provider-name`.
2. Shortcut test setup/teardown. This is, in part, tied to the shortcoming of `godog` in providing a mechanism for feature-level hooking. If you want to specify a feature-level before hook in `godog` you must implement it as a `Background` clause that has a singleton guard using something like `sync.Once` due to the fact that `Background` is implemented by just copying an additional step into the `Scenario` block. Rather than implementing a singleton, you can add a "tag" processor that allows you to do some sort of setup/teardown logic based on a tag (i.e. apply and delete some manifest with a given name via implementing a processer to handle `manifest:name`). Additionally, these tag processors allow you to inject additional state into context that can be fetched per test.

### Kubernetes Helpers

Because this is already substantially extending the footprint of `TestingT` and wrapping it in a new interface that adds lifecycle support, this interface also adds some useful helpers attached to the `TestingT` interface, namely:

1. The ability to install a helm chart
2. The ability to apply and delete manifests
3. Embedding an initialized controller-runtime client

Additionally, because the full suite setup of any Kubernetes operator generally has some typical steps, i.e. setting up dependencies. There are some top-level hooks exposed as a suite builder pattern that allows you to do things such as install CRDs or helm charts during suite initialization.

### Step function signatures

Godog heavily uses reflection to invoke test steps and allows for steps to be defined with multiple method signatures. Each test step, in addition to the step arguments, allows for passing:

1. An initial `context.Context` argument.
2. Returning an optional `error` value.

Either of the above can be elided in the function signature. We extend the step definition behavior to allow for passing an additional extra argument of a `TestingT` type to make this slightly more reflective of go-based tests. What that means is you can define step definitions with the following formats:

```golang
func someStep1noError(ctx context.Context, t TestingT, ...)
func someStep1Error(ctx context.Context, t TestingT, ...) error

func someStep2noError(t TestingT, ...)
func someStep2Error(t TestingT, ...) error

func someStep3noError(ctx context.Context, ...)
func someStep3Error(ctx context.Context, ...) error

func someStep4noError(...)
func someStep4Error(...) error
```

Out of these signatures, `someStep1noError` and `someStep2noError` are preferred alongside usage of `t.Fail` or use of something like testify's `require` or `assert` packages to keep the steps functioning similarly to typical go tests.

### Better `Fail` handling

Internally `godog` panics when calling `FailNow` or `SkipNow`, which, when using testify's `require` happens any time some assertion condition is not met. That becomes extremely problematic for testing cleanup. Therefore we install `panic` handlers around any sort of user called code that explicitly handles the message that `godog` panics with on a test assertion failure.

There are slightly different behaviors depending on when `FailNow` is called.

1. In a feature-level setup hook any sort of `FailNow` calls propagates the assertion error down to any child scenarios so that they all fail.
2. In a scenario-level hook or test step the error is handled normally and only the scenario fails
3. In any sort of `Cleanup` code, after the scenario/feature is already marked as successful, by default the failure backtrace is just logged and the test executions continue. If the suite is initialized with `ExitOnCleanupFailures` the test features may be marked as successful, but the overall tests will still fail with the error/backtrace from the cleanup failure.

### Feature Provider Table Generation

Because of the tagging extensions above that allow you to skip tests on a per-provider basis, the `tablegenerator` package in this library implements a simple markdown-based table generator for documentation to show which features are supported by what providers. Its rendered output looks like this:

|      SCENARIO      | EKS | AKS | GKE | K3D |
|--------------------|-----|-----|-----|-----|
| Some scenario      | ✅  | ✅  | ✅  | ✅  |

### Normalized flag utilization

Check `suite.go` for all flags, but some highlights:

- `-retain`: Skip cleanup phase for tests which failed
- `-timeout`: Set the timeout for the entire test suite to run
- `-provider`: Specify a provider with its own unique hooks and scenarios to run

## Usage

Included is a "stub" test suite example, but here's a slightly more complex suite builder setup:

```go
	suite, err = framework.SuiteBuilderFromFlags().
		RegisterProvider("eks", framework.NoopProvider).
		RegisterProvider("gke", framework.NoopProvider).
		RegisterProvider("aks", framework.NoopProvider).
		RegisterProvider("k3d", framework.NoopProvider).
		WithDefaultProvider("k3d").
		WithHelmChart("http://some.host.local", "some", "dependency", helm.InstallOptions{
			Name:            "dependency",
			Namespace:       "dependency",
			Version:         "v1.1.1",
			CreateNamespace: true,
		}).
		WithCRDDirectory("path/to/crds").
		OnFeature(func(ctx context.Context, t framework.TestingT) {
			t.Log("Installing some helm chart")
			t.InstallHelmChart(ctx, "http://some.host.local", "some", "chart", helm.InstallOptions{
				Name:      "chart",
				Namespace: t.IsolateNamespace(ctx),
				Values: map[string]any{},
			})
			t.Log("Successfully installed some helm chart")
		}).
		RegisterTag("mytag", 1, MyTagProcessor).
		Build()
```