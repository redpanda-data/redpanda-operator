package properties

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

func UnmarshalProperties(values map[string]any, dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("dst must be a non-nil pointer to a struct")
	}

	if values == nil {
		return nil
	}

	return unmarshalPropertyValues(v.Elem(), values)
}

func MarshalProperties(v any) map[string]any {
	if v == nil {
		return nil
	}

	result := make(map[string]any)
	aliases := make(map[string]map[int]any)
	marshalPropertyValue(reflect.ValueOf(v), result, aliases)

	for key, aliasValues := range aliases {
		if _, ok := result[key]; ok {
			// we only merge in aliases when their values haven'T
			// already been set
			continue
		}

		// find the highest precedence value from the aliases
		// and use it
		firstValue := -1
		for n := range aliasValues {
			if firstValue == -1 || n < firstValue {
				firstValue = n
			}
		}
		result[key] = aliasValues[firstValue]
	}

	return result
}

func unmarshalPropertyValues(v reflect.Value, data map[string]any) error {
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("dst must point to a struct")
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		tag := fieldType.Tag.Get("property")
		if tag == "" {
			continue
		}

		if len(strings.Split(tag, ",")) > 1 {
			// this is an alias, skip it
			continue
		}

		raw, ok := data[tag]
		if !ok {
			// key not found
			continue
		}

		if err := assignValue(field, raw); err != nil {
			return fmt.Errorf("failed to assign field %s: %w", fieldType.Name, err)
		}
	}

	return nil
}

func assignValue(dst reflect.Value, src any) error {
	if src == nil {
		return nil
	}

	dstType := dst.Type()

	// If the field is a pointer, create it and set Elem
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dstType.Elem()))
		}
		return assignValue(dst.Elem(), src)
	}

	switch dst.Kind() {

	case reflect.Struct:
		// expect src to be map[string]any
		m, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for struct, got %T", src)
		}
		return unmarshalPropertyValues(dst, m)

	case reflect.Slice:
		srcVal := reflect.ValueOf(src)

		if srcVal.Kind() != reflect.Slice && srcVal.Kind() != reflect.Array {
			return fmt.Errorf("expected slice/array for field, got %T", src)
		}

		length := srcVal.Len()
		slice := reflect.MakeSlice(dstType, length, length)

		for i := range length {
			if err := assignValue(slice.Index(i), srcVal.Index(i).Interface()); err != nil {
				return err
			}
		}

		dst.Set(slice)
		return nil

	case reflect.Map:
		m, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map for map field, got %T", src)
		}

		outMap := reflect.MakeMap(dst.Type())
		for k, v := range m {
			keyVal := reflect.ValueOf(k)
			elemVal := reflect.New(dstType.Elem()).Elem()

			if err := assignValue(elemVal, v); err != nil {
				return err
			}

			outMap.SetMapIndex(keyVal, elemVal)
		}
		dst.Set(outMap)
		return nil

	default:
		srcVal := reflect.ValueOf(src)
		if srcVal.Type().AssignableTo(dstType) {
			dst.Set(srcVal)
			return nil
		}

		if srcVal.Type().ConvertibleTo(dstType) {
			dst.Set(srcVal.Convert(dstType))
			return nil
		}

		return fmt.Errorf("cannot assign %T to %s", src, dstType)
	}
}

func marshalPropertyValue(v reflect.Value, out map[string]any, aliases map[string]map[int]any) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return
	}

	t := v.Type()

	processField := func(tag string, value any, isAlias bool, aliasOrder int) {
		if !isAlias {
			out[tag] = value
			return
		}

		existing, ok := aliases[tag]
		if !ok {
			existing = make(map[int]any)
		}
		existing[aliasOrder] = value
		aliases[tag] = existing
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if fieldType.PkgPath != "" {
			continue
		}

		tag := fieldType.Tag.Get("property")
		if tag == "" {
			continue
		}

		tokens := strings.Split(tag, ",")
		tag = tokens[0]
		isAlias := false
		aliasOrder := 0
		if len(tokens) > 1 {
			isAlias = true
			tokens := strings.Split(tokens[1], ":")
			if len(tokens) == 1 {
				// invalid alias token, something is wrong with the generator
				continue
			}
			order, err := strconv.Atoi(tokens[1])
			if err != nil {
				// invalid alias token, something is wrong with the generator
				continue
			}
			aliasOrder = order
		}

		// Handle nested structs
		switch field.Kind() {

		case reflect.Struct:
			nested := make(map[string]any)
			marshalPropertyValue(field, nested, aliases)
			if len(nested) > 0 {
				processField(tag, nested, isAlias, aliasOrder)
			}

		case reflect.Pointer:
			if field.IsNil() {
				// don't marshal nils
				continue
			}

			switch field.Elem().Kind() {
			case reflect.Struct:
				nested := make(map[string]any)
				marshalPropertyValue(field.Elem(), nested, aliases)
				if len(nested) > 0 {
					processField(tag, nested, isAlias, aliasOrder)
				}
			case reflect.Slice, reflect.Array:
				var sliceOut []any
				for j := 0; j < field.Elem().Len(); j++ {
					item := field.Elem().Index(j)
					if item.Kind() == reflect.Pointer {
						if item.IsNil() {
							sliceOut = append(sliceOut, nil)
							continue
						}
						item = item.Elem()
					}

					if item.Kind() == reflect.Struct {
						nested := make(map[string]any)
						marshalPropertyValue(item, nested, aliases)
						sliceOut = append(sliceOut, nested)
					} else {
						sliceOut = append(sliceOut, item.Interface())
					}
				}
				if len(sliceOut) > 0 {
					processField(tag, sliceOut, isAlias, aliasOrder)
				}
			default:
				processField(tag, field.Elem().Interface(), isAlias, aliasOrder)
			}

		default:
			processField(tag, field.Interface(), isAlias, aliasOrder)
		}
	}
}
