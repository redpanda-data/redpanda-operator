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
	"sigs.k8s.io/yaml"
)

// ParseRepresentation still has to handle the situation where non-string values are passed
// through to configuration in string form.
// We try to strip a single layer of quotes if parsing the raw representation fails.
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
		repr = strings.Trim(repr, "\n")
		val, err := strconv.ParseFloat(repr, 64)
		if err == nil {
			return val, err
		}
		return strconv.ParseFloat(unquote(repr), 64)
	case "integer":
		repr = strings.Trim(repr, "\n")
		val, err := strconv.ParseInt(repr, 10, 64)
		if err == nil {
			return val, err
		}
		return strconv.ParseInt(unquote(repr), 10, 64)
	case "boolean":
		repr = strings.Trim(repr, "\n")
		val, err := strconv.ParseBool(repr)
		if err == nil {
			return val, err
		}
		return strconv.ParseBool(unquote(repr))
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

func unquote(repr string) string {
	return strings.TrimPrefix(strings.TrimSuffix(repr, `"`), `"`)
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
