// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package tracking

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/formatters"
	messages "github.com/cucumber/messages/go/v21"
	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
)

type feature struct {
	*internaltesting.Cleaner

	name           string
	opts           *internaltesting.TestingOptions
	t              *internaltesting.TestingT
	scenariosToRun int
	isRunning      bool
	hasStepFailure bool
	tags           *tagset
}

func (f *feature) options() *internaltesting.TestingOptions {
	return f.opts.Clone()
}

// FeatureHookTracker exists since godog doesn't have any before/after
// feature hooks, it acts by reference counting the number
// of times a before hook has been called and then decrementing
// a counter based on the number of scenarios that should
// be run per-feature based on the nasty hack of hooking
// into the formatter interface of godog.
type FeatureHookTracker struct {
	scenarios *scenarioHookTracker

	failedSuite bool
	registry    *internaltesting.TagRegistry
	opts        *internaltesting.TestingOptions

	onFeatures []func(context.Context, *internaltesting.TestingT)
	features   map[string]*feature
	mutex      sync.RWMutex
}

func NewFeatureHookTracker(registry *internaltesting.TagRegistry, opts *internaltesting.TestingOptions, onFeatures, onScenarios []func(context.Context, *internaltesting.TestingT)) *FeatureHookTracker {
	return &FeatureHookTracker{
		scenarios:  newScenarioHookTracker(registry, opts, onScenarios),
		onFeatures: onFeatures,
		registry:   registry,
		opts:       opts,
		features:   make(map[string]*feature),
	}
}

func (f *FeatureHookTracker) Scenario(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	features := f.features[scenario.Uri]

	isFirst := !features.isRunning
	if isFirst {
		opts := f.opts.Clone()

		cleaner := internaltesting.NewCleaner(godog.T(ctx), opts)
		t := internaltesting.NewTesting(ctx, opts, cleaner)

		features.isRunning = true
		features.opts = opts
		features.Cleaner = cleaner
		features.t = t

		// we process the configured hooks first and then tags
		for _, fn := range f.onFeatures {
			internaltesting.WrapWithPanicHandler("Feature Hook Failure: ", internaltesting.ExitBehaviorNone, fn)(ctx, t)
		}

		for _, fn := range f.registry.Handlers(features.tags.flatten()) {
			// iteratively inject tag handler context
			ctx = internaltesting.WrapWithPanicHandler("", internaltesting.ExitBehaviorNone, fn.Handler)(ctx, t, fn.Arguments)
		}

		f.features[scenario.Uri] = features
	}

	return f.scenarios.start(ctx, isFirst, scenario, features, func() {
		f.scenarioFailed(scenario)
	})
}

func (f *FeatureHookTracker) ScenarioFinished(ctx context.Context, scenario *godog.Scenario) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	features := f.features[scenario.Uri]
	features.scenariosToRun--

	f.features[scenario.Uri] = features

	f.scenarios.finish(ctx, scenario)
	if features.scenariosToRun <= 0 {
		delete(f.features, scenario.Uri)

		message := fmt.Sprintf("Feature (%s) Cleanup Failure: ", features.name)
		internaltesting.WrapWithPanicHandler(message, f.opts.ExitBehavior, features.DoCleanup)(ctx, features.hasStepFailure)
	}
}

func (f *FeatureHookTracker) SuiteFailed() bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	return f.failedSuite
}

func (f *FeatureHookTracker) scenarioFailed(scenario *godog.Scenario) {
	// this is always run with the mutex held due to being called
	// in the call stack of ScenarioFinished
	f.failedSuite = true

	features := f.features[scenario.Uri]
	features.hasStepFailure = true

	f.features[scenario.Uri] = features
}

// Feature tracks the feature when it is run.
func (f *FeatureHookTracker) Feature(doc *messages.GherkinDocument, uri string, data []byte) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	children := FilterChildren(f.opts.Provider, doc.Feature.Children)

	f.features[uri] = &feature{
		name:           doc.Feature.Name,
		scenariosToRun: len(children),
		tags:           tagsForFeature(doc.Feature.Tags),
	}
}

func (f *FeatureHookTracker) RegisterFormatter(opts godog.Options) godog.Options {
	formatters.Format("custom", "", func(suite string, out io.Writer) formatters.Formatter {
		return f
	})
	opts.Format += ",custom"
	return opts
}

// This is all boilerplate to satisfy the formatters.Formatter interface

func (f *FeatureHookTracker) TestRunStarted() {}
func (f *FeatureHookTracker) Defined(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition) {
}
func (f *FeatureHookTracker) Pickle(pickle *messages.Pickle) {
}
func (f *FeatureHookTracker) Failed(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition, error) {
}
func (f *FeatureHookTracker) Passed(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition) {
}
func (f *FeatureHookTracker) Skipped(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition) {
}
func (f *FeatureHookTracker) Undefined(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition) {
}
func (f *FeatureHookTracker) Pending(*messages.Pickle, *messages.PickleStep, *formatters.StepDefinition) {
}
func (f *FeatureHookTracker) Summary() {}
