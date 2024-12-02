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
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"k8s.io/apimachinery/pkg/runtime"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
	"github.com/redpanda-data/redpanda-operator/harpoon/internal/tracking"
)

func setShortTimeout(timeout *time.Duration, short time.Duration) {
	if testing.Short() && *timeout == 0 {
		*timeout = short
	}
}

type helmChart struct {
	repo    string
	chart   string
	url     string
	options helm.InstallOptions
}

type SuiteBuilder struct {
	testingOpts           *internaltesting.TestingOptions
	output                string
	opts                  godog.Options
	registry              *internaltesting.TagRegistry
	providers             map[string]Provider
	defaultProvider       string
	injectors             []func(context.Context) context.Context
	crdDirectories        []string
	helmCharts            []helmChart
	onFeatures            []func(context.Context, *internaltesting.TestingT)
	onScenarios           []func(context.Context, *internaltesting.TestingT)
	exitOnCleanupFailures bool
}

func SuiteBuilderFromFlags() *SuiteBuilder {
	godogOpts := godog.Options{Output: colors.Colored(os.Stdout)}

	var config string
	var output string
	options := &internaltesting.TestingOptions{ExitBehavior: internaltesting.ExitBehaviorLog}

	// TODO: Consider adding an -always-retain flag that doesn't actually delete anything. If
	// we add something like that we'll still have to do some "stateful" cleanup in the lifecycle,
	// i.e. switching namespaces even if we don't delete the old one depending on when namespace
	// isolation occurs, so consider if it would make sense to add something like a T.AlwaysCleanup
	// or something that allows us to switch between "optional" and "required" test cleanup.
	flag.BoolVar(&options.RetainOnFailure, "retain", false, "retain resources when a scenario fails")
	flag.DurationVar(&options.CleanupTimeout, "cleanup-timeout", 0, "timeout for running any cleanup routines after a scenario")
	flag.DurationVar(&options.Timeout, "timeout", 0, "timeout for running any individual test")
	flag.StringVar(&config, "kube-config", "", "path to kube-config to use for scenario runs")
	flag.StringVar(&output, "output", "", "path to write test formatter output to")
	flag.StringVar(&options.Provider, "provider", "", "provider for the test suite")
	flag.StringVar(&godogOpts.Format, "output-format", "pretty", "godog output format")
	flag.BoolVar(&godogOpts.NoColors, "no-color", false, "print only in black and white")
	flag.Int64Var(&godogOpts.Randomize, "seed", -1, "seed for tests, set to -1 for a random seed")

	flag.Parse()

	options.KubectlOptions = internaltesting.NewKubectlOptions(config)

	setShortTimeout(&options.Timeout, 2*time.Minute)
	setShortTimeout(&options.CleanupTimeout, 2*time.Minute)

	registry := internaltesting.NewTagRegistry()
	builder := &SuiteBuilder{
		testingOpts: options,
		output:      output,
		opts:        godogOpts,
		registry:    registry,
		providers:   make(map[string]Provider),
	}

	builder.RegisterTag("isolated", -1000, isolatedTag)

	return builder
}

func (b *SuiteBuilder) WithDefaultProvider(name string) *SuiteBuilder {
	b.defaultProvider = name
	return b
}

func (b *SuiteBuilder) RegisterTag(tag string, priority int, handler TagHandler) *SuiteBuilder {
	b.registry.Register(tag, priority, func(ctx context.Context, tt *internaltesting.TestingT, s []string) context.Context {
		// wrap since we move into the internal implementation of the interface
		handler(ctx, tt, s...)
		return ctx
	})
	return b
}

func (b *SuiteBuilder) RegisterProvider(name string, provider Provider) *SuiteBuilder {
	b.providers[name] = provider
	return b
}

func (b *SuiteBuilder) InjectContext(fn func(context.Context) context.Context) *SuiteBuilder {
	b.injectors = append(b.injectors, fn)
	return b
}

func (b *SuiteBuilder) WithSchemeFunctions(fns ...func(s *runtime.Scheme) error) *SuiteBuilder {
	b.testingOpts.SchemeRegisterers = append(b.testingOpts.SchemeRegisterers, fns...)
	return b
}

func (b *SuiteBuilder) OnFeature(fn func(context.Context, TestingT)) *SuiteBuilder {
	b.onFeatures = append(b.onFeatures, func(ctx context.Context, tt *internaltesting.TestingT) {
		// wrap since we move into the internal implementation of the interface
		fn(ctx, tt)
	})
	return b
}

func (b *SuiteBuilder) OnScenario(fn func(context.Context, TestingT)) *SuiteBuilder {
	b.onScenarios = append(b.onScenarios, func(ctx context.Context, tt *internaltesting.TestingT) {
		// wrap since we move into the internal implementation of the interface
		fn(ctx, tt)
	})
	return b
}

func (b *SuiteBuilder) WithHelmChart(url, repo, chart string, options helm.InstallOptions) *SuiteBuilder {
	b.helmCharts = append(b.helmCharts, helmChart{
		url:     url,
		repo:    repo,
		chart:   chart,
		options: options,
	})
	return b
}

func (b *SuiteBuilder) ExitOnCleanupFailures() *SuiteBuilder {
	b.exitOnCleanupFailures = true
	return b
}

func (b *SuiteBuilder) WithCRDDirectory(directory string) *SuiteBuilder {
	b.crdDirectories = append(b.crdDirectories, directory)
	return b
}

func setupErrorCheck(ctx context.Context, err error, cleanupFn func(ctx context.Context)) {
	if err != nil {
		fmt.Printf("setting up test suite: %v\n", err)
		cleanupFn(ctx)
		os.Exit(1)
	}
}

func (b *SuiteBuilder) Build() (*Suite, error) {
	tracker := tracking.NewFeatureHookTracker(b.registry, b.testingOpts, b.onFeatures, b.onScenarios)
	opts := tracker.RegisterFormatter(b.opts)

	providerName := b.testingOpts.Provider
	if providerName == "" {
		b.testingOpts.Provider = b.defaultProvider
		providerName = b.defaultProvider
	}

	provider, ok := b.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %q", providerName)
	}

	if err := provider.Initialize(); err != nil {
		return nil, err
	}
	ctx := provider.GetBaseContext()
	opts.DefaultContext = ctx
	opts.Tags = fmt.Sprintf("~@skip:%s", providerName)

	restConfig, err := b.testingOpts.KubectlOptions.RestConfig()
	if err != nil {
		return nil, err
	}

	helmClient, err := helm.New(helm.Options{
		KubeConfig: restConfig,
	})
	if err != nil {
		return nil, err
	}

	return &Suite{
		output: b.output,
		suite: &godog.TestSuite{
			Name: "acceptance",
			TestSuiteInitializer: func(suiteContext *godog.TestSuiteContext) {
				cleanup := func(ctx context.Context) {
					// teardown in reverse order from setup
					for _, directory := range b.crdDirectories {
						_, err := internaltesting.KubectlDelete(ctx, directory, b.testingOpts.KubectlOptions)
						if err != nil {
							fmt.Printf("WARNING: error uninstalling crds: %v\n", err)
						}
					}
					for _, chart := range b.helmCharts {
						if err := helmClient.Uninstall(ctx, helm.Release{
							Namespace: chart.options.Namespace,
							Name:      chart.options.Name,
						}); err != nil {
							fmt.Printf("WARNING: error uninstalling helm chart: %v\n", err)
						}
					}
					if err := provider.Teardown(ctx); err != nil {
						fmt.Printf("WARNING: error running provider teardown: %v\n", err)
					}
				}

				suiteContext.BeforeSuite(func() {
					err = provider.Setup(ctx)
					setupErrorCheck(ctx, err, cleanup)

					// now add helm charts
					for _, chart := range b.helmCharts {
						err = helmClient.RepoAdd(ctx, chart.repo, chart.url)
						setupErrorCheck(ctx, err, cleanup)

						_, err := helmClient.Install(ctx, chart.repo+"/"+chart.chart, chart.options)
						setupErrorCheck(ctx, err, cleanup)
					}

					// and finally any crds
					for _, directory := range b.crdDirectories {
						_, err := internaltesting.KubectlApply(ctx, directory, b.testingOpts.KubectlOptions)
						setupErrorCheck(ctx, err, cleanup)
					}
				})
				suiteContext.AfterSuite(func() {
					cancel := func() {}
					cleanupTimeout := b.testingOpts.CleanupTimeout
					if cleanupTimeout != 0 {
						ctx, cancel = context.WithTimeout(ctx, cleanupTimeout)
					}
					defer cancel()

					if tracker.SuiteFailed() && b.testingOpts.RetainOnFailure {
						fmt.Println("skipping cleanup due to test failure and retain flag being set")
						return
					}
					cleanup(ctx)
				})
			},
			ScenarioInitializer: func(ctx *godog.ScenarioContext) {
				ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
					ctx, err := tracker.Scenario(ctx, sc)
					if err != nil {
						return nil, err
					}
					for _, fn := range b.injectors {
						ctx = fn(ctx)
					}
					return ctx, nil
				})
				ctx.After(func(ctx context.Context, sc *godog.Scenario, _ error) (context.Context, error) {
					tracker.ScenarioFinished(ctx, sc)
					return ctx, nil
				})

				getSteps(ctx)
			},
			Options: &opts,
		},
		options:               b.testingOpts,
		exitOnCleanupFailures: b.exitOnCleanupFailures,
	}, nil
}

type Suite struct {
	output                string
	suite                 *godog.TestSuite
	options               *internaltesting.TestingOptions
	exitOnCleanupFailures bool
}

func (s *Suite) RunM(m *testing.M) {
	var testLog bytes.Buffer
	if s.output != "" {
		s.suite.Options.Output = &testLog
		s.suite.Options.NoColors = true
	}

	if s.exitOnCleanupFailures {
		s.options.ExitBehavior = internaltesting.ExitBehaviorTerminateProgram
	}

	status := s.suite.Run()

	if st := m.Run(); st > status {
		status = st
	}

	if s.output != "" {
		writeTestLog(testLog, s.output)
	}

	os.Exit(status)
}

func (s *Suite) RunT(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var testLog bytes.Buffer
	if s.output != "" {
		s.suite.Options.Output = &testLog
		s.suite.Options.NoColors = true
	}

	var errMessage internaltesting.TerminationError
	done := make(chan struct{})
	if s.exitOnCleanupFailures {
		s.options.ExitBehavior = internaltesting.ExitBehaviorTestFail
		go func() {
			defer close(done)
			select {
			case <-ctx.Done():
				return
			case errMessage = <-internaltesting.TerminationChan:
			}
		}()
	} else {
		close(done)
	}

	s.suite.Options.TestingT = t

	s.suite.Run()
	cancel()
	<-done

	if s.output != "" {
		writeTestLog(testLog, s.output)
	}

	if errMessage.Message != "" {
		if errMessage.NeedsLog {
			fmt.Print(errMessage.Message)
		}
		t.Fail()
	}
}

func writeTestLog(buffer bytes.Buffer, path string) {
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		fmt.Println("Error writing test output log to disk, writing to stdout")
		fmt.Println(buffer.String())
	}
}
