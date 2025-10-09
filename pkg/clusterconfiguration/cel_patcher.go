// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package clusterconfiguration

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/cel-go/cel"
)

// Template represents a templatable file.
// The contents may be structured [eg, a RedpandaYaml document] or unstructured;
// the latter is useful for the bootstrap.yaml template, because no schemata are
// available to determine field types at the time the file's created.
// Instead, all entries are single-line YAML representations of values.
// We provide custom CEL functions to support both of these.
type Template[T any] struct {
	Content  T       `json:"content,omitempty" yaml:"content,omitempty"`
	Fixups   []Fixup `json:"fixups,omitempty"`
	Warnings []error
}

type Fixup struct {
	Field string `json:"field"`
	CEL   string `json:"cel"`
}

// Warning is an error type that will not be fatal.
type Warning struct {
	cause error
}

func (w Warning) Error() string {
	return fmt.Sprintf("warning: %s", w.cause.Error())
}

func (w Warning) Unwrap() error {
	return w.cause
}

type CelFactory func(reflect.Value) (*cel.Env, error)

func (t *Template[T]) Fixup(engine func(reflect.Value) (*cel.Env, error)) error {
	var errs []error
	for _, f := range t.Fixups {
		// Locate the field
		fieldPath := strings.Split(f.Field, ".")
		field, err := findField(reflect.ValueOf(t.Content), fieldPath)
		if !field.IsValid() {
			errs = append(errs, errors.Newf("field path %q is not valid", f.Field))
			continue
		}
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "error finding field for %q path", f.Field))
		}
		if !field.CanSet() && field.parent.Type().Kind() != reflect.Map {
			errs = append(errs, errors.Newf("field %q is not settable", f.Field))
			continue
		}
		v := field.Interface()
		// Apply the fixup
		env, err := engine(field.Value)
		if err != nil {
			errs = append(errs, errors.Newf("problem readying CEL engine for field %q of type %T: %w", f.Field, v, err))
			continue
		}
		ast, issues := env.Compile(f.CEL)
		if issues != nil {
			errs = append(errs, errors.Newf("problem compiling CEL for field %q: %w", f.Field, issues.Err()))
			continue
		}
		prog, err := env.Program(ast)
		if err != nil {
			errs = append(errs, errors.Newf("problem readying CEL program from field %q: %w", f.Field, err))
			continue
		}
		result, _, err := prog.ContextEval(context.Background(), map[string]any{"it": v})
		if err != nil {
			var w Warning
			if errors.As(err, &w) {
				t.Warnings = append(t.Warnings, err)
				continue
			}
			errs = append(errs, errors.Newf("problem running CEL expression for field %q: %w", f.Field, err))
			continue
		}
		r := result.Value()
		if err := assign(field, r); err != nil {
			errs = append(errs, errors.Newf("cannot assign field %q: %w", f.Field, err))
			continue
		}
	}
	return stderrors.Join(errs...)
}

type fieldAssigner struct {
	reflect.Value
	parent *reflect.Value
	key    string
}

func assign(f fieldAssigner, r any) error {
	if f.key != "" {
		// We have a map entry
		return assignMap(f, r)
	}
	val := reflect.ValueOf(r)
	if val.Type().AssignableTo(f.Type()) {
		f.Set(val)
		return nil
	}
	if f.Type().Kind() == reflect.Pointer && val.Type().AssignableTo(f.Type().Elem()) {
		tmp := reflect.New(val.Type()) // create zero box of same type as val
		tmp.Elem().Set(val)            // copy the actual value
		f.Set(tmp)
		return nil
	}
	return errors.Newf("result of type %s is not compatible with field", val.Type())
}

func assignMap(f fieldAssigner, r any) error {
	val := reflect.ValueOf(r)
	if val.Type().AssignableTo(f.parent.Type().Elem()) {
		f.parent.SetMapIndex(reflect.ValueOf(f.key), val)
		return nil
	}
	if f.parent.Type().Elem().Kind() == reflect.Pointer && val.Type().AssignableTo(f.parent.Type().Elem().Elem()) {
		tmp := reflect.New(val.Type()) // create zero box of same type as val
		tmp.Elem().Set(val)            // copy the actual value
		f.parent.SetMapIndex(reflect.ValueOf(f.key), tmp)
		return nil
	}
	return errors.Newf("result of type %s is not compatible with map", val.Type())
}

func fieldByNameOrJSON(v reflect.Value, name string) (reflect.Value, *reflect.Value, error) {
	// If we end up with a nil pointer to structure, instantiate it
	// so that we can navigate to sub-fields.
	var unnamed reflect.Value

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		n := v.Type().NumField()
		for i := range n {
			f := t.Field(i)
			if f.Name == name {
				return v.Field(i), nil, nil
			}
			if jsonTag, ok := f.Tag.Lookup("json"); ok {
				jsonValues := strings.Split(jsonTag, ",")
				if len(jsonValues) == 0 {
					continue
				}
				if jsonValues[0] == "" {
					unnamed = v.Field(i)
					continue
				}
				if jsonValues[0] != name {
					continue
				}
				return v.Field(i), nil, nil
			} else if yamlTag, ok := f.Tag.Lookup("yaml"); ok {
				yamlValues := strings.Split(yamlTag, ",")
				if len(yamlValues) == 0 {
					continue
				}
				if yamlValues[0] == "" {
					unnamed = v.Field(i)
					continue
				}
				if yamlValues[0] != name {
					continue
				}
				return v.Field(i), nil, nil
			}
		}
		if unnamed.IsValid() {
			// Try searching down an inlined json
			return fieldByNameOrJSON(unnamed, name)
		}
	case reflect.Pointer:
		return fieldByNameOrJSON(v.Elem(), name)
	case reflect.Map:
		entry := v.MapIndex(reflect.ValueOf(name))
		if entry.IsValid() {
			return entry, &v, nil
		}
		// Make a new zero entry
		zero := reflect.New(v.Type().Elem())
		return zero.Elem(), &v, nil
	case reflect.Slice, reflect.Array:
		idx, err := strconv.ParseInt(name, 10, 64)
		if err != nil {
			return reflect.Value{}, &v, errors.Wrap(err, "")
		}
		return v.Index(int(idx)), nil, nil
	case reflect.Interface:
		// Work out what the unterlying thing is and use that
		return fieldByNameOrJSON(v.Elem(), name)
	default:
		// The user's specified a path to a structure element we can't handle.
		// Common causes here include using `redpanda.` prefixes on clusterConfiguration.
		return reflect.Value{}, nil, nil
	}
	return reflect.Value{}, nil, nil
}

func findField(v reflect.Value, path []string) (fieldAssigner, error) {
	var parent *reflect.Value
	var err error
	for i, step := range path {
		v, parent, err = fieldByNameOrJSON(v, step)
		if err != nil {
			return fieldAssigner{}, errors.Wrap(err, "field finder failed")
		}
		if !v.IsValid() {
			return fieldAssigner{Value: v}, nil
		}
		if i < len(path)-1 && v.Type().Kind() == reflect.Pointer && v.Type().Elem().Kind() == reflect.Struct && v.IsNil() {
			// We want to step into this
			st := reflect.New(v.Type().Elem())
			v.Set(st)
		}
	}
	if parent != nil {
		return fieldAssigner{
			Value:  v,
			parent: parent,
			key:    path[len(path)-1],
		}, nil
	}
	return fieldAssigner{Value: v}, nil
}
