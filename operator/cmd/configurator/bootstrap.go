// Copyright 2025 Redpanda Data, Inc.
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
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

// TemplateBootstrapYaml expands the bootstrap file
// This takes an input template, resolves any remaining external references, then writes out the resulting bootstrap file
func TemplateBootstrapYaml(ctx context.Context, cloudExpander *pkgsecrets.CloudExpander, inFile, outFile, fixups string) error {
	var bootstrap map[string]string
	buf, err := os.ReadFile(inFile)
	if err != nil {
		return fmt.Errorf("cannot load bootstrap template file: %w", err)
	}
	if err := json.Unmarshal(buf, &bootstrap); err != nil {
		return fmt.Errorf("cannot parse bootstrap template file: %w", err)
	}

	// Perform optional fixups if required
	bootstrap, err = applyFixups(ctx, bootstrap, fixups, cloudExpander)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to apply fixups to .bootstrap.yaml: %w", err))
	}

	var config []string
	keys := slices.Sorted(maps.Keys(bootstrap))
	for _, k := range keys {
		// Append the final representation to the bootstrap file
		config = append(config, fmt.Sprintf("%s: %s", k, bootstrap[k]))
	}

	output := strings.Join(config, "\n")
	return os.WriteFile(outFile, []byte(output), 0o644)
}
