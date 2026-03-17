// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package tplutil provides generic template execution, serialization, and
// reflection utilities for rendering Kubernetes manifests.
package tplutil

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/imdario/mergo"
	"sigs.k8s.io/yaml"
)

const alphanumChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// CleanForK8s truncates a string to 63 characters (the K8s name limit)
// and trims any trailing hyphen.
func CleanForK8s(in string) string {
	return strings.TrimSuffix(Trunc(63, in), "-")
}

// Trunc truncates a string to the given length.
func Trunc(length int, in string) string {
	if len(in) < length {
		return in
	}
	return in[:length]
}

// ToYaml marshals a value to a YAML string.
func ToYaml(value any) string {
	marshalled, err := yaml.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(marshalled), "\n")
}

// ToJSON marshals a value to a JSON string.
func ToJSON(value any) string {
	// Round-trip through any to normalize types.
	normalized, err := RoundTrip[any](value)
	if err != nil {
		return ""
	}
	marshalled, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(marshalled)
}

// FromJSON unmarshals a JSON string to an arbitrary value.
func FromJSON(data string) any {
	var out any
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return ""
	}
	return out
}

// Quote wraps values in double quotes (Go %q format).
func Quote(vs ...any) string {
	result := make([]string, 0, len(vs))
	for _, v := range vs {
		result = append(result, fmt.Sprintf("%q", fmt.Sprint(v)))
	}
	return strings.Join(result, " ")
}

// SQuote wraps values in single quotes.
func SQuote(vs ...any) string {
	result := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != nil {
			result = append(result, fmt.Sprintf("'%v'", v))
		}
	}
	return strings.Join(result, " ")
}

// RandAlphaNum generates a random alphanumeric string of the given length.
func RandAlphaNum(length int) string {
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphanumChars))))
		if err != nil {
			// Fallback to zero index on error (should not happen with crypto/rand).
			n = big.NewInt(0)
		}
		b[i] = alphanumChars[n.Int64()]
	}
	return string(b)
}

// Tpl executes a Go template string with the given data context.
// Returns the expanded string or an error if template parsing/execution fails.
func Tpl(templateStr string, data any) (string, error) {
	fns := sprig.TxtFuncMap()
	fns["toYaml"] = ToYaml
	fns["toJson"] = ToJSON
	fns["fromJson"] = FromJSON

	tmpl, err := template.New("").Funcs(fns).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}
	return b.String(), nil
}

// UnmarshalInto converts an arbitrary value to type T via JSON round-trip.
func UnmarshalInto[T any](value any) (T, error) {
	var output T
	data, err := json.Marshal(value)
	if err != nil {
		return output, err
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return output, err
	}
	return output, nil
}

// MergeTo merges multiple sources into type T using mergo and JSON round-trip.
func MergeTo[T any](sources ...any) (T, error) {
	var zero T
	dst := map[string]any{}
	for _, src := range sources {
		normalized, err := RoundTrip[any](src)
		if err != nil {
			return zero, fmt.Errorf("cannot round trip element: %w", err)
		}
		asMap, ok := normalized.(map[string]any)
		if !ok {
			return zero, fmt.Errorf("cannot convert element to map: %T", normalized)
		}
		if err := mergo.Merge(&dst, asMap); err != nil {
			return zero, fmt.Errorf("cannot merge element: %w", err)
		}
	}
	result, err := UnmarshalInto[T](dst)
	if err != nil {
		return zero, fmt.Errorf("cannot recast merged structure: %w", err)
	}
	return result, nil
}

// RoundTrip converts a value to type T via JSON marshaling/unmarshaling.
func RoundTrip[T any](value any) (T, error) {
	var output T
	data, err := json.Marshal(value)
	if err != nil {
		return output, err
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return output, err
	}
	return output, nil
}

// GetField retrieves a field from a struct or map by key using reflection.
func GetField[T any](value any, key string) (T, bool) {
	var zero T
	v := reflect.ValueOf(value)

	if v.Type().Kind() == reflect.Map {
		item := v.MapIndex(reflect.ValueOf(key))
		if !item.IsValid() || item.IsZero() {
			return zero, false
		}
		result, ok := item.Interface().(T)
		return result, ok
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)

		fieldName := field.Name
		if jsonTag, ok := field.Tag.Lookup("json"); ok {
			fieldName = strings.Split(jsonTag, ",")[0]
		}

		if fieldName != key {
			continue
		}

		fieldValue := v.Field(i)

		switch value := fieldValue.Interface().(type) {
		case *T:
			if value == nil {
				return zero, false
			}
			return *value, true
		case T:
			return value, true
		default:
			return zero, false
		}
	}

	return zero, false
}

// KindOf returns the reflect kind of a value as a string.
func KindOf(v any) string {
	return reflect.TypeOf(v).Kind().String()
}

// RecursiveTpl walks a value recursively and expands any string fields
// containing Go template delimiters ("{{") using the provided template data.
func RecursiveTpl(data any, tplData any) (any, error) {
	k := KindOf(data)

	if k == "map" {
		m := data.(map[string]any)
		for key, value := range m {
			expanded, err := RecursiveTpl(value, tplData)
			if err != nil {
				return nil, err
			}
			m[key] = expanded
		}
		return m, nil
	} else if k == "slice" {
		s := data.([]any)
		var out []any
		for i := range s {
			expanded, err := RecursiveTpl(s[i], tplData)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded)
		}
		return out, nil
	} else if k == "string" && strings.Contains(data.(string), "{{") {
		result, err := Tpl(data.(string), tplData)
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	return data, nil
}

// StructuredTpl recurses through all fields of T and expands any string fields
// containing template delimiters with [Tpl]. The tplData parameter provides the
// template execution context.
func StructuredTpl[T any](in T, tplData any) (T, error) {
	var zero T
	untyped, err := UnmarshalInto[map[string]any](in)
	if err != nil {
		return zero, err
	}
	expanded, err := RecursiveTpl(untyped, tplData)
	if err != nil {
		return zero, err
	}
	return MergeTo[T](expanded)
}

// MergeSliceBy merges two slices by matching elements on a key field.
// Elements in original that match an override (by mergeKey) are merged via mergeFunc.
// Override elements not present in original are appended via MergeTo.
func MergeSliceBy[Original any, Overrides any](
	original []Original,
	override []Overrides,
	mergeKey string,
	mergeFunc func(Original, Overrides) Original,
) []Original {
	originalKeys := map[string]bool{}
	overrideByKey := map[string]Overrides{}

	for _, el := range override {
		key, ok := GetField[string](el, mergeKey)
		if !ok {
			continue
		}
		overrideByKey[key] = el
	}

	var merged []Original
	for _, el := range original {
		key, _ := GetField[string](el, mergeKey)
		originalKeys[key] = true

		if elOverride, ok := overrideByKey[key]; ok {
			merged = append(merged, mergeFunc(el, elOverride))
		} else {
			merged = append(merged, el)
		}
	}

	for _, el := range override {
		key, ok := GetField[string](el, mergeKey)
		if !ok {
			continue
		}
		if _, ok := originalKeys[key]; ok {
			continue
		}
		// Best-effort merge for new entries; ignore errors.
		if result, err := MergeTo[Original](el); err == nil {
			merged = append(merged, result)
		}
	}

	return merged
}
