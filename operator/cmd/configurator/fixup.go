// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configurator

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

func applyFixups[T any](ctx context.Context, config T, fn string, cloudExpander *secrets.CloudExpander) (T, error) {
	var zero T
	// It's harmless if the fixup file does not exist
	fixupContent, err := os.ReadFile(fn)
	var pe *os.PathError
	if errors.As(err, &pe) {
		return config, nil
	}
	if err != nil {
		return zero, fmt.Errorf("cannot load fixups file %q: %w", fn, err)
	}
	var fs []clusterconfiguration.Fixup
	if err := yaml.Unmarshal(fixupContent, &fs); err != nil {
		return zero, fmt.Errorf("cannot unmarshal fixups file %q: %w", fn, err)
	}
	tpl := clusterconfiguration.Template[T]{
		Content: config,
		Fixups:  fs,
	}
	// Load the environment
	env := make(map[string]string)
	for _, ev := range os.Environ() {
		name, val, found := strings.Cut(ev, "=")
		if found {
			env[name] = strings.Trim(val, "\n")
		}
	}
	factory := clusterconfiguration.StdLibFactory(ctx, env, cloudExpander)
	if err = tpl.Fixup(factory); err != nil {
		return zero, err
	}
	return tpl.Content, nil
}
