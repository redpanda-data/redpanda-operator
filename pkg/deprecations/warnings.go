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
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DeprecatedPrefix = "Deprecated"

// FindDeprecatedFieldWarnings inspects an arbitrary Kubernetes object (a
// `client.Object`) and returns a slice of deprecation warning messages for any
// struct fields (including nested structs, pointer-to-structs and slices of
// structs) that have a field whose name is prefixed with "Deprecated" and
// whose value is not the zero value. The name shown in the warning is taken
// from the field's `json` tag when present, otherwise the Go field name is
// used.
//
// The function returns an error if the provided object is not a struct (or a
// pointer to a struct) that can be reflected into.
func FindDeprecatedFieldWarnings(obj client.Object) ([]string, error) {
	v := reflect.ValueOf(obj)
	// We expect a pointer to a struct (typical for Kubernetes objects).
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("object is a nil pointer")
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("object must be a struct or pointer to struct")
	}

	warnings := make([]string, 0)
	visited := make(map[uintptr]struct{})

	var collect func(rv reflect.Value, rt reflect.Type, path string)
	collect = func(rv reflect.Value, rt reflect.Type, path string) {
		// Work with the concrete value (dereference pointers/interfaces)
		for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
			if rv.IsNil() {
				return
			}
			// protect against cycles
			if rv.Kind() == reflect.Pointer {
				ptr := rv.Pointer()
				if _, ok := visited[ptr]; ok {
					return
				}
				visited[ptr] = struct{}{}
			}
			rv = rv.Elem()
			rt = rv.Type()
		}

		if rv.Kind() == reflect.Struct {
			for i := 0; i < rv.NumField(); i++ {
				sf := rt.Field(i)

				// Skip unexported fields.
				if sf.PkgPath != "" {
					continue
				}

				fv := rv.Field(i)

				// Determine json name and whether this field is inlined.
				tag := sf.Tag.Get("json")
				inline := strings.Contains(tag, "inline")
				jsonName := ""
				if tag == "" {
					jsonName = sf.Name
				} else {
					parts := strings.Split(tag, ",")
					tag := parts[0]
					switch tag {
					case "-":
						jsonName = sf.Name
					case "":
						// tag like `json:\",inline\"` -> treat as inline
						jsonName = sf.Name
					default:
						jsonName = tag
					}
				}

				// compute next path unless this is an inline field
				nextPath := path
				if !inline {
					if nextPath == "" {
						nextPath = jsonName
					} else {
						nextPath = nextPath + "." + jsonName
					}
				}

				// If this field's name starts with Deprecated and it's not a
				// zero value, emit a warning using the full json path.
				if strings.HasPrefix(sf.Name, DeprecatedPrefix) {
					// Only check values we can interface with.
					if fv.IsValid() && fv.CanInterface() {
						isZero := fv.IsZero()
						if !isZero {
							// For deprecated fields we want the json name of the field
							// itself. It may differ from the computed jsonName above
							// (which is the container field's name). So compute the
							// field's own json name.
							fieldTag := sf.Tag.Get("json")
							fieldJSON := ""
							if fieldTag == "" {
								fieldJSON = sf.Name
							} else {
								parts := strings.Split(fieldTag, ",")
								if parts[0] == "-" || parts[0] == "" {
									fieldJSON = sf.Name
								} else {
									fieldJSON = parts[0]
								}
							}

							// assemble full path
							fullPath := fieldJSON
							if path != "" {
								fullPath = path + "." + fieldJSON
							}

							warnings = append(warnings, fmt.Sprintf("field '%s' is deprecated and set", fullPath))
						}
					}
				}

				// Recurse into supported container types to find nested Deprecated fields.
				switch fv.Kind() {
				case reflect.Struct:
					collect(fv, fv.Type(), nextPath)
				case reflect.Pointer, reflect.Interface:
					if fv.IsNil() {
						// nothing to do
					} else {
						collect(fv, fv.Type(), nextPath)
					}
				case reflect.Slice, reflect.Array:
					elemKind := fv.Type().Elem().Kind()
					if elemKind == reflect.Struct || elemKind == reflect.Pointer || elemKind == reflect.Interface {
						for j := 0; j < fv.Len(); j++ {
							el := fv.Index(j)
							collect(el, el.Type(), nextPath)
						}
					}
				case reflect.Map:
					// If the map has struct or pointer-to-struct values, inspect them.
					elemKind := fv.Type().Elem().Kind()
					if elemKind == reflect.Struct || elemKind == reflect.Pointer || elemKind == reflect.Interface {
						for _, key := range fv.MapKeys() {
							val := fv.MapIndex(key)
							collect(val, val.Type(), nextPath)
						}
					}
				}
			}
		}
	}

	collect(v, v.Type(), "")

	return warnings, nil
}
