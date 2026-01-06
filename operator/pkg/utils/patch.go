// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/cisco-open/k8s-objectmatcher/patch"
	"github.com/cockroachdb/errors"
	jsoniter "github.com/json-iterator/go"
)

// IgnoreAnnotation is a banzaicloud k8s-objectmatcher plugin that allows ignoring a specific annotation when computing a patch
// between two objects.
func IgnoreAnnotation(name string) patch.CalculateOption {
	return func(current, modified []byte) ([]byte, []byte, error) {
		var currentResource map[string]interface{}
		if err := json.Unmarshal(current, &currentResource); err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not unmarshal byte sequence for current: %w", err)
		}

		var modifiedResource map[string]interface{}
		if err := json.Unmarshal(modified, &modifiedResource); err != nil {
			return []byte{}, []byte{}, fmt.Errorf("could not unmarshal byte sequence for modified: %w", err)
		}

		if removeElement(currentResource, "metadata", "annotations", name) ||
			removeElement(currentResource, "spec", "template", "metadata", "annotations", name) {
			marsh, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(currentResource)
			if err != nil {
				return current, modified, fmt.Errorf("could not marshal current resource: %w", err)
			}
			current = marsh
		}

		if removeElement(modifiedResource, "metadata", "annotations", name) ||
			removeElement(modifiedResource, "spec", "template", "metadata", "annotations", name) {
			marsh, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(modifiedResource)
			if err != nil {
				return current, modified, fmt.Errorf("could not marshal modified resource: %w", err)
			}
			modified = marsh
		}

		return current, modified, nil
	}
}

func removeElement(m map[string]interface{}, path ...string) bool {
	if len(path) == 0 {
		return false
	}
	cur := m
	for i := 0; i < len(path)-1; i++ {
		if child, ok := cur[path[i]]; ok {
			childMap, convOk := child.(map[string]interface{})
			if !convOk {
				return false
			}
			cur = childMap
		}
	}
	lastKey := path[len(path)-1]
	_, exists := cur[lastKey]
	delete(cur, lastKey)
	return exists
}

// taken from github.com/cisco-open/k8s-objectmatcher/patch but with some modifications to work
// with newer typed PDBs
func IgnorePDBSelector() patch.CalculateOption {
	return func(current, modified []byte) ([]byte, []byte, error) {
		currentResource := map[string]any{}
		if err := jsoniter.Unmarshal(current, &currentResource); err != nil {
			return []byte{}, []byte{}, errors.Wrap(err, "could not unmarshal byte sequence for current")
		}

		modifiedResource := map[string]any{}
		if err := jsoniter.Unmarshal(modified, &modifiedResource); err != nil {
			return []byte{}, []byte{}, errors.Wrap(err, "could not unmarshal byte sequence for modified")
		}

		if reflect.DeepEqual(getPDBSelector(currentResource), getPDBSelector(modifiedResource)) {
			var err error
			current, err = deletePDBSelector(currentResource)
			if err != nil {
				return nil, nil, errors.Wrap(err, "delete pdb selector from current")
			}
			modified, err = deletePDBSelector(modifiedResource)
			if err != nil {
				return nil, nil, errors.Wrap(err, "delete pdb selector from modified")
			}
		}

		return current, modified, nil
	}
}

func getPDBSelector(resource map[string]interface{}) interface{} {
	if spec, ok := resource["spec"]; ok {
		if spec, ok := spec.(map[string]any); ok {
			if selector, ok := spec["selector"]; ok {
				return selector
			}
		}
	}
	return nil
}

func deletePDBSelector(resource map[string]interface{}) ([]byte, error) {
	if spec, ok := resource["spec"]; ok {
		if spec, ok := spec.(map[string]any); ok {
			delete(spec, "selector")
		}
	}

	obj, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(resource)
	if err != nil {
		return []byte{}, errors.Wrap(err, "could not marshal byte sequence")
	}

	return obj, nil
}
