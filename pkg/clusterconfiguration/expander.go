// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

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
	"gopkg.in/yaml.v3" //nolint:depguard // this is necessary due to differences in how the yaml tagging mechanisms work and the fact that some structs on config.RedpandaYaml are missing inline annotations
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
		switch metadata.Items.Type {
		case "string":
			ss, err := convertStringToArray[string](repr)
			if err != nil {
				return nil, err
			}
			// Historically all string arrays have been sorted. It's likely this is
			// NOT required and was an artifact of the operator and config-watcher
			// "fighting" about the ordering of the superusers settings, which has
			// since been fixed.
			// However, there's no trail or test that would indicate why this is
			// present so we've preserved it out of paranoia.
			// DO NOT RELY ON THIS BEHAVIOR.
			sort.Strings(ss)
			return ss, nil

		// Fallback to any for all other types.
		// TODO: make this method recursive rather than special casing arrays like
		// this.
		default:
			return convertStringToArray[any](repr)
		}
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

func convertStringToArray[T any](value string) ([]T, error) {
	a := make([]T, 0)
	err := yaml.Unmarshal([]byte(value), &a)

	if len(a) == 1 {
		// it is possible this was not comma separated, so let's make it so and retry unmarshalling
		b := make([]T, 0)
		errB := yaml.Unmarshal([]byte(strings.ReplaceAll(value, " ", ",")), &b)
		if errB == nil && len(b) > len(a) {
			return b, errB
		}
	}
	return a, err
}
