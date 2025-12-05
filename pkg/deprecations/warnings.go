// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package deprecations

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DeprecatedPrefix = "Deprecated"

// FindDeprecatedFieldWarnings inspects an arbitrary `client.Object` and returns a
// deprecation warning messages for any deeply nested struct fields that have a field
// prefixed with "Deprecated" and whose value is not the zero value. The name shown in the
// warning is taken from the field's full json path from the root of the CRD.
func FindDeprecatedFieldWarnings(obj client.Object) []string {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			// nothing to do
			return nil
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		// if we don't have a struct, then it can't have fields
		return nil
	}

	// Only inspect the Spec field and its children
	spec, ok := v.Type().FieldByName("Spec")
	if !ok {
		// we only warn on user-supplied input, which comes from
		// Spec
		return nil
	}

	deprecations := deprecatedFields(v.FieldByName("Spec"), spec.Type, "spec", make(map[uintptr]struct{}))

	slices.Sort(deprecations)

	return deprecations
}

func deprecatedFields(value reflect.Value, reflectType reflect.Type, path string, visited map[uintptr]struct{}) []string {
	out := make([]string, 0)

	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return out
		}
		// protect against cycles
		if value.Kind() == reflect.Pointer {
			ptr := value.Pointer()
			if _, ok := visited[ptr]; ok {
				return out
			}
			visited[ptr] = struct{}{}
		}

		// use concrete types
		value = value.Elem()
		reflectType = value.Type()
	}

	if value.Kind() == reflect.Struct {
		for i := 0; i < value.NumField(); i++ {
			structField := reflectType.Field(i)

			// Skip unexported fields.
			if structField.PkgPath != "" {
				continue
			}

			fieldValue := value.Field(i)

			nextPath := getNextPath(structField, path)
			if warning := checkFieldUsage(structField, fieldValue, path); warning != "" {
				out = append(out, warning)
			}

			switch fieldValue.Kind() {
			case reflect.Struct:
				out = append(out, deprecatedFields(fieldValue, fieldValue.Type(), nextPath, visited)...)
			case reflect.Pointer, reflect.Interface:
				if fieldValue.IsNil() {
					// the pointer is nil, so we don't need to traverse since that's the 0 value
				} else {
					out = append(out, deprecatedFields(fieldValue, fieldValue.Type(), nextPath, visited)...)
				}
			case reflect.Slice, reflect.Array:
				elemKind := fieldValue.Type().Elem().Kind()
				if elemKind == reflect.Struct || elemKind == reflect.Pointer || elemKind == reflect.Interface {
					for j := 0; j < fieldValue.Len(); j++ {
						value := fieldValue.Index(j)
						out = append(out, deprecatedFields(value, value.Type(), nextPath, visited)...)
					}
				}
			case reflect.Map:
				elemKind := fieldValue.Type().Elem().Kind()
				if elemKind == reflect.Struct || elemKind == reflect.Pointer || elemKind == reflect.Interface {
					for _, key := range fieldValue.MapKeys() {
						value := fieldValue.MapIndex(key)
						out = append(out, deprecatedFields(value, value.Type(), nextPath, visited)...)
					}
				}
			}
		}
	}

	return out
}

func checkFieldUsage(field reflect.StructField, value reflect.Value, path string) string {
	// If this field's name starts with Deprecated and it's not a
	// zero value, emit a warning using the full json path.
	if strings.HasPrefix(field.Name, DeprecatedPrefix) {
		// Only check values we can interface with.
		if value.IsValid() && value.CanInterface() {
			isZero := value.IsZero()
			if !isZero {
				fullPath := getJSONName(field)
				if path != "" {
					fullPath = path + "." + fullPath
				}
				return fmt.Sprintf("field '%s' is deprecated and set", fullPath)
			}
		}
	}

	return ""
}

func getJSONName(field reflect.StructField) string {
	fieldTag := field.Tag.Get("json")
	if fieldTag == "" {
		return field.Name
	}
	parts := strings.Split(fieldTag, ",")
	if parts[0] == "-" || parts[0] == "" {
		return field.Name
	}
	return parts[0]
}

func getNextPath(field reflect.StructField, path string) string {
	tag := field.Tag.Get("json")
	inline := strings.Contains(tag, ",inline")

	nextPath := func(name string) string {
		if inline {
			return path
		}

		if path == "" {
			return name
		}
		return path + "." + name
	}

	if tag == "" {
		return nextPath(field.Name)
	}

	parts := strings.Split(tag, ",")
	tag = parts[0]

	switch tag {
	case "-":
		return nextPath(field.Name)
	case "":
		return nextPath(tag)
	default:
		return nextPath(tag)
	}
}
