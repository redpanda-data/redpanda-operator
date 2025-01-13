// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helm

import (
	"sigs.k8s.io/yaml"
)

// MergeYAMLValues merges a collection of YAML values in accordance with helm's
// merging logic.
func MergeYAMLValues(vs ...[]byte) (map[string]any, error) {
	out := map[string]any{}

	for _, valuesBytes := range vs {
		var values map[string]any
		if err := yaml.Unmarshal(valuesBytes, &values); err != nil {
			return nil, err
		}

		out = mergeMaps(out, values)
	}

	return out, nil
}

// mergeMaps is ripped directly from helm's values package as it's not publicly
// exposed and the only way to access it is to write YAML files to disk.
func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}
