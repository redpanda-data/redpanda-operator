package configurator

import (
	"fmt"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
)

func applyFixups[T any](config T, fn string) (T, error) {
	var zero T
	// It's harmless if the fixup file does not exist
	fixupContent, err := os.ReadFile(fn)
	var pe *os.PathError
	if errors.As(err, &pe) {
		return config, nil
	}
	if err != nil {
		return zero, fmt.Errorf("cannot load fixup file %q: %w", fn, err)
	}
	var fs []clusterconfiguration.Fixup
	tpl := clusterconfiguration.Template[T]{
		Content: config,
	}
	if err := yaml.Unmarshal(fixupContent, &fs); err != nil {
		return zero, fmt.Errorf("cannot unmarshal fixup file %q: %w", fn, err)
	}
	// Load the environment
	env := make(map[string]string)
	for _, ev := range os.Environ() {
		parse := strings.SplitN(ev, "=", 2)
		if len(parse) == 2 {
			env[parse[0]] = parse[1]
		}
	}
	factory := clusterconfiguration.StdLibFactory(env)
	err = tpl.Fixup(factory)
	return tpl.Content, err
}
