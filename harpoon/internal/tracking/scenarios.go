// Copyright 2025 Redpanda Data, Inc.
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
	"sync"

	"github.com/cucumber/godog"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
)

type scenario struct {
	*internaltesting.Cleaner

	t      *internaltesting.TestingT
	onFail func()
}

type scenarioHookTracker struct {
	registry *internaltesting.TagRegistry
	opts     *internaltesting.TestingOptions

	onScenarios []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)
	scenarios   map[string]*scenario
	mutex       sync.Mutex
}

func newScenarioHookTracker(registry *internaltesting.TagRegistry, opts *internaltesting.TestingOptions, onScenarios []func(context.Context, *internaltesting.TestingT, []internaltesting.ParsedTag)) *scenarioHookTracker {
	return &scenarioHookTracker{
		registry:    registry,
		opts:        opts,
		onScenarios: onScenarios,
		scenarios:   make(map[string]*scenario),
	}
}

func (s *scenarioHookTracker) start(ctx context.Context, isFirst bool, sc *godog.Scenario, feature *feature, onFailure func()) (context.Context, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	tags := tagsForScenario(feature.tags, sc.Tags)
	// make a copy of the current feature options
	opts := feature.options()

	cleaner := internaltesting.NewCleaner(godog.T(ctx), opts)
	t := internaltesting.NewTesting(ctx, opts, cleaner)

	feature.t.PropagateError(isFirst, t)

	parsed := s.registry.Handlers(tags.flatten())

	// we process the configured hooks first and then tags
	for _, fn := range s.onScenarios {
		t.SetMessagePrefix("Scenario Hook Failure: ")
		internaltesting.WrapWithPanicHandler(true, internaltesting.ExitBehaviorNone, fn)(ctx, t, internaltesting.ParsedTags(parsed))
	}

	for _, fn := range parsed {
		// iteratively inject tag handler context
		t.SetMessagePrefix("Scenario Tag Failure: ")
		ctx = internaltesting.WrapWithPanicHandler(true, internaltesting.ExitBehaviorNone, fn.Handler)(ctx, t, fn.Arguments)
	}

	s.scenarios[sc.Id] = &scenario{
		Cleaner: cleaner,
		onFail:  onFailure,
		// hold a reference to t so we can track step
		// failures
		t: t,
	}

	return t.IntoContext(ctx), nil
}

func (s *scenarioHookTracker) finish(ctx context.Context, scenario *godog.Scenario) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	scene := s.scenarios[scenario.Id]
	if scene == nil {
		return
	}

	failure := scene.t.IsFailure()

	// and then clean up the scenario hooks themselves
	scene.t.SetMessagePrefix(fmt.Sprintf("Scenario (%s) Cleanup Failure: ", scenario.Name))
	internaltesting.WrapWithPanicHandler(true, s.opts.ExitBehavior, scene.DoCleanup)(ctx, failure)
	scene.t.SetMessagePrefix("")
	if failure {
		scene.onFail()
	}

	delete(s.scenarios, scenario.Id)
}
