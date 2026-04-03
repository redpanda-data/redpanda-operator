// Copyright 2026 Redpanda Data, Inc.
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
	"os/exec"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	messages "github.com/cucumber/messages/go/v21"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
	"github.com/redpanda-data/redpanda-operator/harpoon/internal/tracking"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
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
	providers             map[string]FullProvider
	defaultProvider       string
	images                []string
	injectors             []func(context.Context) context.Context
	crdDirectories        []string
	helmCharts            []helmChart
	onFeatures            []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)
	onScenarios           []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)
	afterSetup            []func(ctx context.Context, restConfig *rest.Config) error
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
	// TODO(chrisseto): This conflicts with the -retain flag defined in
	// pkg/testutil. Figure out how to rely on that or "spy" on the already defined flag.
	// flag.BoolVar(&options.RetainOnFailure, "retain", false, "retain resources when a scenario fails")
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
		providers:   make(map[string]FullProvider),
	}

	builder.RegisterTag("isolated", -1000, isolatedTag)
	builder.RegisterTag("vcluster", -2000, vclusterTag)
	builder.RegisterTag("variant", -3000, variantTag)
	builder.RegisterTag("injectVariant", -3000, injectVariantTag)

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

func (b *SuiteBuilder) RegisterProvider(name string, provider FullProvider) *SuiteBuilder {
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

func (b *SuiteBuilder) WithImportedImages(images ...string) *SuiteBuilder {
	b.images = images
	return b
}

func (b *SuiteBuilder) OnFeature(fn func(context.Context, TestingT, ...ParsedTag)) *SuiteBuilder {
	b.onFeatures = append(b.onFeatures, func(ctx context.Context, tt *internaltesting.TestingT, tags []internaltesting.ParsedTag) {
		parsed := []ParsedTag{}
		for _, tag := range tags {
			parsed = append(parsed, ParsedTag{
				Name:      tag.Name,
				Arguments: tag.Arguments,
			})
		}
		// wrap since we move into the internal implementation of the interface
		fn(ctx, tt, parsed...)
	})
	return b
}

func (b *SuiteBuilder) OnScenario(fn func(context.Context, TestingT, ...ParsedTag)) *SuiteBuilder {
	b.onScenarios = append(b.onScenarios, func(ctx context.Context, tt *internaltesting.TestingT, tags []internaltesting.ParsedTag) {
		parsed := []ParsedTag{}
		for _, tag := range tags {
			parsed = append(parsed, ParsedTag{
				Name:      tag.Name,
				Arguments: tag.Arguments,
			})
		}
		// wrap since we move into the internal implementation of the interface
		fn(ctx, tt, parsed...)
	})
	return b
}

// AfterSetup registers a callback that runs after helm charts are installed
// but before tests start. This is useful for readiness checks that require the
// cluster to be fully operational (e.g. waiting for webhooks).
func (b *SuiteBuilder) AfterSetup(fn func(ctx context.Context, restConfig *rest.Config) error) *SuiteBuilder {
	b.afterSetup = append(b.afterSetup, fn)
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

func (b *SuiteBuilder) Strict() *SuiteBuilder {
	b.opts.Strict = true
	return b
}

func setupErrorCheck(ctx context.Context, err error, cleanupFn func(ctx context.Context)) {
	if err != nil {
		fmt.Printf("setting up test suite: %v\n", err)
		cleanupFn(ctx)
		os.Exit(1)
	}
}

// featureContent holds the resolved content for a single feature, including
// whether it should be run serially (due to having the @serial tag).
type featureContent struct {
	name     string
	contents []byte
	serial   bool
}

func (b *SuiteBuilder) Build() (*Suite, error) {
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
	ctx = internaltesting.ProviderIntoContext(ctx, provider)

	// Use a temporary tracker + suite just to discover and parse features.
	tmpTracker := tracking.NewFeatureHookTracker(b.registry, b.testingOpts, b.onFeatures, b.onScenarios)
	discoveryOpts := tmpTracker.RegisterFormatter(b.opts)
	discoveryOpts.DefaultContext = ctx
	discoveryOpts.Tags = fmt.Sprintf("~@skip:%s", providerName)

	parsingSuite := &godog.TestSuite{
		Name:    "acceptance",
		Options: &discoveryOpts,
	}
	features, err := parsingSuite.RetrieveFeatures()
	if err != nil {
		return nil, err
	}

	// Resolve all feature contents, including generated variants.
	var resolved []featureContent
	for _, feature := range features {
		serial := isSerialFeature(feature.Feature.Tags)
		resolved = append(resolved, featureContent{
			name:     feature.Uri,
			contents: feature.Content,
			serial:   serial,
		})

		if variantTag := featureVariant(feature.Feature.Tags); variantTag != "" {
			content := bytes.ReplaceAll(feature.Content, []byte("Feature: "), []byte(fmt.Sprintf("Feature: %s - ", variantTag)))
			content = bytes.ReplaceAll(content, []byte("Scenario: "), []byte(fmt.Sprintf("Scenario: %s - ", variantTag)))
			content = bytes.ReplaceAll(content, []byte("Scenario Outline: "), []byte(fmt.Sprintf("Scenario Outline: %s - ", variantTag)))
			content = bytes.ReplaceAll(content, []byte(fmt.Sprintf("@variant:%s", variantTag)), []byte(fmt.Sprintf("@injectVariant:%s", variantTag)))
			resolved = append(resolved, featureContent{
				name:     path.Join(variantTag, feature.Uri),
				contents: content,
				serial:   serial,
			})
		}
	}

	return &Suite{
		output:                b.output,
		baseOpts:              b.opts,
		testingOpts:           b.testingOpts,
		registry:              b.registry,
		provider:              provider,
		providerName:          providerName,
		ctx:                   ctx,
		features:              resolved,
		injectors:             b.injectors,
		onFeatures:            b.onFeatures,
		onScenarios:           b.onScenarios,
		crdDirectories:        b.crdDirectories,
		helmCharts:            b.helmCharts,
		afterSetup:            b.afterSetup,
		images:                b.images,
		exitOnCleanupFailures: b.exitOnCleanupFailures,
	}, nil
}

type Suite struct {
	output                string
	baseOpts              godog.Options
	testingOpts           *internaltesting.TestingOptions
	registry              *internaltesting.TagRegistry
	provider              FullProvider
	providerName          string
	ctx                   context.Context
	features              []featureContent
	injectors             []func(context.Context) context.Context
	onFeatures            []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)
	onScenarios           []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)
	crdDirectories        []string
	helmCharts            []helmChart
	afterSetup            []func(ctx context.Context, restConfig *rest.Config) error
	images                []string
	exitOnCleanupFailures bool
}

// makeGodogSuite creates a godog.TestSuite for the given feature contents.
// If suiteInit is non-nil, it is used as the TestSuiteInitializer (for setup/teardown);
// otherwise no suite-level hooks are registered.
func (s *Suite) makeGodogSuite(
	name string,
	tracker *tracking.FeatureHookTracker,
	features []godog.Feature,
	suiteInit func(*godog.TestSuiteContext),
) *godog.TestSuite {
	opts := tracker.RegisterFormatter(s.baseOpts)
	opts.DefaultContext = s.ctx
	opts.Tags = fmt.Sprintf("~@skip:%s", s.providerName)
	// Only use in-memory feature contents; don't discover from disk.
	// Point Paths to a directory with no .feature files to prevent godog
	// from falling back to the default "features" directory when
	// FeatureContents is empty (godog checks len(Paths)==0 && len(FeatureContents)==0).
	opts.Paths = []string{os.TempDir()}
	opts.FeatureContents = features

	return &godog.TestSuite{
		Name:                 name,
		TestSuiteInitializer: suiteInit,
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
				ctx, err := tracker.Scenario(ctx, sc)
				if err != nil {
					return nil, err
				}
				for _, fn := range s.injectors {
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
	}
}

// suiteCleanup holds the cleanup function created during suite setup so it
// can be called later during teardown.
type suiteCleanup struct {
	fn func(ctx context.Context)
}

// suiteSetup returns a TestSuiteInitializer that runs BeforeSuite only (no
// AfterSuite). The cleanup function is stored in the provided suiteCleanup
// so that teardown can be run separately after all features complete.
func (s *Suite) suiteSetup(sc *suiteCleanup) func(*godog.TestSuiteContext) {
	ctx := s.ctx
	provider := s.provider

	return func(suiteContext *godog.TestSuiteContext) {
		var kubeOptions *internaltesting.KubectlOptions
		var helmClient *helm.Client

		cleanup := func(ctx context.Context) {
			if kubeOptions != nil {
				for _, directory := range s.crdDirectories {
					_, err := internaltesting.KubectlDelete(ctx, directory, kubeOptions)
					if err != nil {
						fmt.Printf("WARNING: error uninstalling crds: %v\n", err)
					}
				}
			}

			if helmClient != nil {
				for _, chart := range s.helmCharts {
					if err := helmClient.Uninstall(ctx, helm.Release{
						Namespace: chart.options.Namespace,
						Name:      chart.options.Name,
					}); err != nil {
						fmt.Printf("WARNING: error uninstalling helm chart: %v\n", err)
					}
				}
			}

			if err := provider.Teardown(ctx); err != nil {
				fmt.Printf("WARNING: error running provider teardown: %v\n", err)
			}
		}

		suiteContext.BeforeSuite(func() {
			err := provider.Setup(ctx)
			setupErrorCheck(ctx, err, cleanup)

			if provisioned, ok := provider.(internaltesting.ProvisionedProvider); ok {
				fmt.Printf("Using Kubernetes configuration at: %v\n", provisioned.ConfigPath())
				s.testingOpts.KubectlOptions = internaltesting.NewKubectlOptions(provisioned.ConfigPath())
			}
			kubeOptions = s.testingOpts.KubectlOptions

			restConfig, err := s.testingOpts.KubectlOptions.RestConfig()
			setupErrorCheck(ctx, err, cleanup)

			helmClient, err = helm.New(helm.Options{
				KubeConfig: restConfig,
			})
			setupErrorCheck(ctx, err, cleanup)

			err = pullImages(s.images)
			setupErrorCheck(ctx, err, cleanup)

			err = provider.LoadImages(ctx, s.images)
			setupErrorCheck(ctx, err, cleanup)

			s.testingOpts.Images = s.images

			for _, chart := range s.helmCharts {
				err = helmClient.RepoAdd(ctx, chart.repo, chart.url)
				setupErrorCheck(ctx, err, cleanup)

				_, err := helmClient.Install(ctx, chart.repo+"/"+chart.chart, chart.options)
				setupErrorCheck(ctx, err, cleanup)
			}

			for _, fn := range s.afterSetup {
				err = fn(ctx, restConfig)
				setupErrorCheck(ctx, err, cleanup)
			}

			for _, directory := range s.crdDirectories {
				_, err := internaltesting.KubectlApply(ctx, directory, s.testingOpts.KubectlOptions)
				setupErrorCheck(ctx, err, cleanup)
			}

			// Store the cleanup function for later teardown.
			sc.fn = cleanup
		})
	}
}

// suiteSetupTeardown returns a TestSuiteInitializer with both BeforeSuite and
// AfterSuite. Used by RunM which runs everything in a single godog suite.
func (s *Suite) suiteSetupTeardown(tracker *tracking.FeatureHookTracker) func(*godog.TestSuiteContext) {
	ctx := s.ctx
	sc := &suiteCleanup{}
	setupInit := s.suiteSetup(sc)

	return func(suiteContext *godog.TestSuiteContext) {
		setupInit(suiteContext)
		suiteContext.AfterSuite(func() {
			cancel := func() {}
			cleanupTimeout := s.testingOpts.CleanupTimeout
			if cleanupTimeout != 0 {
				ctx, cancel = context.WithTimeout(ctx, cleanupTimeout)
			}
			defer cancel()

			if tracker.SuiteFailed() && s.testingOpts.RetainOnFailure {
				fmt.Println("skipping cleanup due to test failure and retain flag being set")
				return
			}
			if sc.fn != nil {
				sc.fn(ctx)
			}
		})
	}
}

func (s *Suite) RunM(m *testing.M) {
	var testLog bytes.Buffer
	if s.output != "" {
		s.baseOpts.Output = &testLog
		s.baseOpts.NoColors = true
	}

	if s.exitOnCleanupFailures {
		s.testingOpts.ExitBehavior = internaltesting.ExitBehaviorTerminateProgram
	}

	// For RunM, run everything sequentially as a single suite (legacy behavior).
	tracker := tracking.NewFeatureHookTracker(s.registry, s.testingOpts, s.onFeatures, s.onScenarios)

	var allFeatures []godog.Feature
	for _, f := range s.features {
		allFeatures = append(allFeatures, godog.Feature{
			Name:     f.name,
			Contents: f.contents,
		})
	}

	suite := s.makeGodogSuite("acceptance", tracker, allFeatures, s.suiteSetupTeardown(tracker))
	status := suite.Run()

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
		s.baseOpts.Output = &testLog
		s.baseOpts.NoColors = true
	}

	if s.exitOnCleanupFailures {
		s.testingOpts.ExitBehavior = internaltesting.ExitBehaviorTestFail
	}

	// Partition features into parallel and serial groups.
	var parallelFeatures, serialFeatures []featureContent
	for _, f := range s.features {
		if f.serial {
			serialFeatures = append(serialFeatures, f)
		} else {
			parallelFeatures = append(parallelFeatures, f)
		}
	}

	// Phase 1: Run setup via a dedicated suite with no features.
	// Setup and teardown are separated so teardown runs after all features complete.
	sc := &suiteCleanup{}
	setupTracker := tracking.NewFeatureHookTracker(s.registry, s.testingOpts, s.onFeatures, s.onScenarios)
	setupSuite := s.makeGodogSuite("setup", setupTracker, []godog.Feature{}, s.suiteSetup(sc))
	setupSuite.Options.TestingT = t
	setupSuite.Run()

	// Collect termination errors from any concurrent feature suite.
	var terminationErrors []internaltesting.TerminationError
	var termMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-internaltesting.TerminationChan:
				if !ok {
					return
				}
				termMu.Lock()
				terminationErrors = append(terminationErrors, err)
				termMu.Unlock()
			}
		}
	}()

	// Track whether any feature suite reported a failure.
	var suiteFailed bool

	// Phase 2: Run parallel features concurrently.
	// Each feature runs as a parallel subtest so that Go's testing framework
	// doesn't serialize the godog t.Run calls across features.
	for _, f := range parallelFeatures {
		t.Run(strings.ReplaceAll(f.name, "/", "_"), func(t *testing.T) {
			t.Parallel()

			tracker := tracking.NewFeatureHookTracker(s.registry, s.testingOpts, s.onFeatures, s.onScenarios)
			gf := []godog.Feature{{Name: f.name, Contents: f.contents}}
			suite := s.makeGodogSuite(f.name, tracker, gf, nil)
			suite.Options.TestingT = t
			suite.Run()
			if tracker.SuiteFailed() {
				termMu.Lock()
				suiteFailed = true
				termMu.Unlock()
			}
		})
	}

	// Phase 3: Run serial features sequentially.
	if len(serialFeatures) > 0 {
		tracker := tracking.NewFeatureHookTracker(s.registry, s.testingOpts, s.onFeatures, s.onScenarios)
		var gf []godog.Feature
		for _, f := range serialFeatures {
			gf = append(gf, godog.Feature{Name: f.name, Contents: f.contents})
		}
		suite := s.makeGodogSuite("serial", tracker, gf, nil)
		suite.Options.TestingT = t
		suite.Run()
		if tracker.SuiteFailed() {
			suiteFailed = true
		}
	}

	// Phase 4: Teardown.
	if sc.fn != nil {
		teardownCtx := s.ctx
		teardownCancel := func() {}
		if s.testingOpts.CleanupTimeout != 0 {
			teardownCtx, teardownCancel = context.WithTimeout(teardownCtx, s.testingOpts.CleanupTimeout)
		}
		defer teardownCancel()

		if suiteFailed && s.testingOpts.RetainOnFailure {
			fmt.Println("skipping cleanup due to test failure and retain flag being set")
		} else {
			sc.fn(teardownCtx)
		}
	}

	cancel()
	<-done

	if s.output != "" {
		writeTestLog(testLog, s.output)
	}

	termMu.Lock()
	defer termMu.Unlock()
	for _, errMessage := range terminationErrors {
		if errMessage.Message != "" {
			if errMessage.NeedsLog {
				fmt.Print(errMessage.Message)
			}
			t.Fail()
		}
	}
}

func writeTestLog(buffer bytes.Buffer, path string) {
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		fmt.Println("Error writing test output log to disk, writing to stdout")
		fmt.Println(buffer.String())
	}
}

func pullImages(images []string) error {
	for _, image := range images {
		if !strings.HasPrefix(image, "localhost") {
			//nolint:gosec // this code is for tests
			if output, err := exec.Command("docker", "pull", image).CombinedOutput(); err != nil {
				return errors.Wrapf(err, "output: %s", output)
			}
		}
	}
	return nil
}

func isSerialFeature(tags []*messages.Tag) bool {
	for _, tag := range tags {
		if strings.TrimPrefix(tag.Name, "@") == "serial" {
			return true
		}
	}
	return false
}
