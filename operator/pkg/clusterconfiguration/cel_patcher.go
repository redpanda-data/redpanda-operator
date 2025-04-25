package clusterconfiguration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

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
		field := findField(reflect.ValueOf(t.Content), fieldPath)
		if !field.IsValid() {
			errs = append(errs, fmt.Errorf("field path %q is not valid", f.Field))
			continue
		}
		if !field.CanSet() && field.parent.Type().Kind() != reflect.Map {
			errs = append(errs, fmt.Errorf("field %q is not settable", f.Field))
			continue
		}
		v := field.Interface()
		// Apply the fixup
		env, err := engine(field.Value)
		if err != nil {
			errs = append(errs, fmt.Errorf("problem readying CEL engine for field %q of type %T: %w", f.Field, v, err))
			continue
		}
		ast, issues := env.Compile(f.CEL)
		if issues != nil {
			errs = append(errs, fmt.Errorf("problem compiling CEL for field %q: %w", f.Field, issues.Err()))
			continue
		}
		prog, err := env.Program(ast)
		if err != nil {
			errs = append(errs, fmt.Errorf("problem readying CEL program from field %q: %w", f.Field, err))
			continue
		}
		result, _, err := prog.ContextEval(context.Background(), map[string]any{"it": v})
		if err != nil {
			var w Warning
			if errors.As(err, &w) {
				t.Warnings = append(t.Warnings, err)
				continue
			}
			errs = append(errs, fmt.Errorf("problem running CEL expression for field %q: %w", f.Field, err))
			continue
		}
		r := result.Value()
		if err := assign(field, r); err != nil {
			errs = append(errs, fmt.Errorf("cannot assign field %q: %w", f.Field, err))
			continue
		}
	}
	return errors.Join(errs...)
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
	return fmt.Errorf("result of type %s is not compatible with field", val.Type())
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
	return fmt.Errorf("result of type %s is not compatible with map", val.Type())
}

func fieldByNameOrJSON(v reflect.Value, name string) (reflect.Value, *reflect.Value) {
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
				return v.Field(i), nil
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
				return v.Field(i), nil
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
				return v.Field(i), nil
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
			return entry, &v
		}
		// Make a new zero entry
		zero := reflect.New(v.Type().Elem())
		return zero.Elem(), &v
	default:
		panic(fmt.Errorf("unhandled default case %q", v.Kind()))
	}
	return reflect.Value{}, nil
}

func findField(v reflect.Value, path []string) fieldAssigner {
	var parent *reflect.Value
	for i, step := range path {
		v, parent = fieldByNameOrJSON(v, step)
		if !v.IsValid() {
			return fieldAssigner{Value: v}
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
		}
	}
	return fieldAssigner{Value: v}
}
