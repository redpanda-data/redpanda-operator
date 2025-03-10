package configurator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/configuration"
)

// Template out the bootstrap file
// This takes an input template, resolves any remaining external references, then writes out the resulting bootstrap file
func templateBootstrapYaml(inFile, outFile string) error {
	var template map[string]vectorizedv1alpha1.ClusterConfigValue
	buf, err := os.ReadFile(inFile)
	if err != nil {
		return fmt.Errorf("cannot load bootstrap template file: %w", err)
	}
	if err := json.Unmarshal(buf, &template); err != nil {
		return fmt.Errorf("cannot parse bootstrap template file: %w", err)
	}

	var config []string
	for k, v := range template {
		// Work out what the value should be and add it to the output.
		repr, err := configuration.ExpandValueForTemplate(v)
		if err != nil {
			return fmt.Errorf("cannot resolve value %s: %w", k, err)
		}
		config = append(config, fmt.Sprintf("%s: %s", k, repr))
	}

	output := strings.Join(config, "\n")
	return os.WriteFile(outFile, []byte(output), 0o644)
}
