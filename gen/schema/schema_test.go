// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package schema

import (
	"reflect"
	"strings"
	"testing"
)

// TestLintValues crawls the structs that gen schema generates schemas for and
// asserts that they follow certain conventions for JSON tagging. In
// particular, we're aiming to address a niche case in gotohelm where go
// convention can result in a subtle bug.
//
// x := ""
// if x != "" { ... }
//
// In helm world, if x is ever nil or not present, the above if would evaluate
// to true as nil != "". The only time this can happen is through user inputs:
// values.yaml. As long as our schema is sufficiently strict, e.g. requiring
// all non-pointer values, we're largely safe from such cases. The one
// exception being --skip-schema-validation.
func TestLintValues(t *testing.T) {
	for pkg, values := range schemas {
		root := reflect.TypeOf(values).Elem()
		t.Run(pkg, func(t *testing.T) {
			walkType(root, func(typ reflect.Type, path []string) bool {
				// Only traverse types in this package and its parents. e.g.
				// charts/redpanda/chart will also include charts/redpanda.
				if !strings.HasPrefix(root.PkgPath(), typ.PkgPath()) {
					return false
				}

				// We're only auditing tagging of struct fields.
				if typ.Kind() != reflect.Struct {
					return true
				}

				for field := range typ.Fields() {
					if !field.IsExported() {
						continue
					}

					omitZero := strings.Contains(field.Tag.Get("json"), ",omitzero")
					omitEmpty := strings.Contains(field.Tag.Get("json"), ",omitempty")
					jsonschemaRequired := strings.Contains(field.Tag.Get("jsonschema"), "required")

					if omitZero {
						// omitzero is not supported in gotohelm at this time. Fail if we encounter it anywhere.
						t.Errorf("%q of %q MUST NOT be tagged with `json:\"omitZero\"`", field.Name, strings.Join(path, "."))
					}

					if jsonschemaRequired {
						// jsonschema:require was previously relied upon but resulted in
						// drift between jsonschema and go's code behavior. When enforcing
						// the usages, it's ends up being nearly the same as omitempty but
						// with a lot more typing.
						t.Errorf("%q of %q MUST NOT be tagged with `jsonschema:\"required\"`", field.Name, strings.Join(path, "."))
					}

					switch field.Type.Kind() {
					case reflect.Map, reflect.Slice:
						// No special handling required here. Range loops and len on empty and nil are equivalent.
						continue

					case reflect.Pointer:
						if !omitEmpty {
							// This check is more so for consistency and easy of mental
							// modeling. It would technically be fine to _omit_ this tag as
							// the zero value is nil which is nearly equivalent to absence
							// for our use case. Schema generation treats them as different,
							// so this is most correct, strictly speaking.
							t.Errorf("nullable field %q of %q MUST be tagged `json:\"omitempty\"`", field.Name, strings.Join(path, "."))
						}

					default:
						if omitEmpty {
							// Non-nullable fields MUST NOT be tagged as omitempty. This
							// causes the schema to mark them as required and marshalling to
							// always output the field (inline with the schema).
							t.Errorf("non-pointer %q of %q MUST NOT be tagged `\"json:omitempty\"`", field.Name, strings.Join(path, "."))
						}
					}
				}

				return true
			})
		})
	}
}

func walkType(root reflect.Type, fn func(reflect.Type, []string) bool) {
	visited := map[reflect.Type]bool{}

	var walk func(typ reflect.Type, path []string)

	walk = func(typ reflect.Type, path []string) {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}

		switch typ.Kind() {
		case reflect.Slice, reflect.Array:
			walk(typ.Elem(), append(path, "[]"))
			return
		case reflect.Map:
			walk(typ.Elem(), append(path, "*"))
			return
		case reflect.Struct:
			if typ.Name() != "" && visited[typ] {
				return
			}
			visited[typ] = true

		default:
			return
		}

		if !fn(typ, path) {
			return
		}

		// Traverse fields afterwards to make outputs friendlier.
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			walk(field.Type, append(path, field.Name))
		}
	}

	walk(root, []string{root.Name()})
}
