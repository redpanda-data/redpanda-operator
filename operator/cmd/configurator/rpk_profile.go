package configurator

import (
	"context"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// TemplateRPKProfileYaml expands the rpk profile file
// This takes an input template, resolves any remaining external references, then writes out the resulting rpk profile file
func TemplateRPKProfileYaml(ctx context.Context, inFile, outFile, fixups string) error {
	if inFile == "" {
		return fmt.Errorf("rpkProfile input path can not be empty")
	}
	var rpkProfile map[string]any
	buf, err := os.ReadFile(inFile)
	if err != nil {
		return fmt.Errorf("cannot load rpkProfile template file: %w", err)
	}
	if err := yaml.Unmarshal(buf, &rpkProfile); err != nil {
		return fmt.Errorf("cannot parse rpkProfile template file: %w", err)
	}

	// Perform optional fixups if required
	rpkProfile, err = applyFixups(ctx, rpkProfile, fixups, nil)
	if err != nil {
		return fmt.Errorf("unable to apply fixups to .profile.yaml: %w", err)
	}

	output, err := yaml.Marshal(rpkProfile)
	if err != nil {
		return fmt.Errorf("cannot marshal rpkProfile: %w", err)
	}

	return os.WriteFile(outFile, output, 0o644)
}
