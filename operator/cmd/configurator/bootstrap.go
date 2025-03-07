package configurator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
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
		repr, err := resolveValue(v)
		if err != nil {
			return fmt.Errorf("cannot resolve value %s: %w", k, err)
		}
		config = append(config, fmt.Sprintf("%s: %s", k, repr))
	}

	output := strings.Join(config, "\n")
	return os.WriteFile(outFile, []byte(output), 0o644)
}

// TODO: put this code somewhere central
func resolveValue(v vectorizedv1alpha1.ClusterConfigValue) (vectorizedv1alpha1.YamlRepresentation, error) {
	switch {
	case v.Representation != nil:
		return *v.Representation, nil
	case v.ConfigMapKeyRef != nil:
		return "", fmt.Errorf("don't know how to resolve config map references")
	case v.SecretKeyRef != nil:
		return "", fmt.Errorf("don't know how to resolve secret references")
	case v.EnvVarRef != nil:
		if value, exist := os.LookupEnv(*v.EnvVarRef); exist {
			return vectorizedv1alpha1.YamlRepresentation(value), nil
		}
		return "", fmt.Errorf("referenced environment variable is unset: %s", *v.EnvVarRef)
	case v.ExternalSecretRef != nil:
		// TODO: grab the external secret, yaml-serialise it [optional, depending on where the responsibility for this lies], return it
		return "", fmt.Errorf("don't know how to resolve external secret references")
	default:
		return "", fmt.Errorf("unspecified template value")
	}
}
