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

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

// Template out the bootstrap file
// This takes an input template, resolves any remaining external references, then writes out the resulting bootstrap file
func templateBootstrapYaml(ctx context.Context, cloudExpander *pkgsecrets.CloudExpander, inFile, outFile, fixups string) error {
	var template map[string]clusterconfiguration.ClusterConfigTemplateValue
	buf, err := os.ReadFile(inFile)
	if err != nil {
		return fmt.Errorf("cannot load bootstrap template file: %w", err)
	}
	if err := json.Unmarshal(buf, &template); err != nil {
		return fmt.Errorf("cannot parse bootstrap template file: %w", err)
	}

	// Perform the initial template expansion
	bootstrap := make(map[string]vectorizedv1alpha1.YAMLRepresentation)
	for k, v := range template {
		// Work out what the value should be and add it to the bootstrap.
		repr, err := clusterconfiguration.ExpandValueForTemplate(ctx, cloudExpander, v)
		if err != nil {
			return fmt.Errorf("cannot resolve value %s: %w", k, err)
		}
		bootstrap[k] = repr
	}

	// Perform optional fixups if required
	bootstrap, err = applyFixups(bootstrap, fixups)
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
