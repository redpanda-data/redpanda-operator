// Package clusterconfiguration holds types to track cluster configuration - the CRD declarations need to
// be transformed down into a representation of those values in a bootstrap template; we also supply an
// evaluator that can turn such a map of values into a map of raw concrete values.
// This is done by the process of turning non-standard config entries into "fixups" - these
// are applied against their named fields in order to produce the final result.
package clusterconfiguration

import (
	"sort"
	"strconv"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	"gopkg.in/yaml.v3"
)

func ParseRepresentation(repr string, metadata *rpadmin.ConfigPropertyMetadata) (any, error) {
	if metadata.Nullable && repr == "null" {
		return nil, nil
	}
	switch metadata.Type {
	case "string":
		var s string
		// YAML unmarshalling to "be liberal in what we accept" here.
		err := yaml.Unmarshal([]byte(repr), &s)
		return s, err
	case "number":
		return strconv.ParseFloat(strings.Trim(repr, "\n"), 64)
	case "integer":
		return strconv.ParseInt(strings.Trim(repr, "\n"), 10, 64)
	case "boolean":
		return strconv.ParseBool(strings.Trim(repr, "\n"))
	case "array":
		return convertStringToStringArray(repr)
	default:
		// TODO: It's unclear whether we should let this ride, or report an error.
		// By letting it pass, we will ultimately report an Unknown value on the condition.
		var s string
		err := yaml.Unmarshal([]byte(repr), &s)
		return s, err
		// Strict alternative:
		// return nil, fmt.Errorf("unrecognised configuration type: %s", metadata.Type)
	}
}

// convertStringToStringArray duplicates the v1 string->[string] processing
func convertStringToStringArray(value string) ([]string, error) {
	a := make([]string, 0)
	err := yaml.Unmarshal([]byte(value), &a)

	if len(a) == 1 {
		// it is possible this was not comma separated, so let's make it so and retry unmarshalling
		b := make([]string, 0)
		errB := yaml.Unmarshal([]byte(strings.ReplaceAll(value, " ", ",")), &b)
		if errB == nil && len(b) > len(a) {
			sort.Strings(b)
			return b, errB
		}
	}
	sort.Strings(a)
	return a, err
}
