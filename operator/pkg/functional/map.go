// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package functional

func MapFn[T any, U any](fn func(T) U, a []T) []U {
	s := make([]U, len(a))
	for i := 0; i < len(a); i++ {
		s[i] = fn(a[i])
	}
	return s
}

func deepCopyMap(m map[string]any) map[string]any {
	copied := make(map[string]any)
	for k, v := range m {
		switch cast := v.(type) {
		case map[string]any:
			copied[k] = deepCopyMap(cast)
		case []any:
			copied[k] = deepCopyElements(cast)
		default:
			copied[k] = v
		}
	}
	return copied
}

func deepCopy(v any) any {
	switch cast := v.(type) {
	case map[string]any:
		return deepCopyMap(cast)
	case []any:
		return deepCopyElements(cast)
	default:
		return v
	}
}

func deepCopyElements(v []any) []any {
	copied := make([]any, len(v))
	for _, value := range v {
		switch cast := value.(type) {
		case map[string]any:
			copied = append(copied, deepCopyMap(cast))
		case []any:
			copied = append(copied, deepCopyElements(cast))
		default:
			return v
		}
	}

	return copied
}

func MergeMaps(first, second map[string]any) map[string]any {
	merged := deepCopyMap(first)
	for k, v := range second {
		if original, ok := merged[k]; ok {
			switch cast := original.(type) {
			case map[string]any:
				// the types must match, otherwise we can't merge them
				if vmap, ok := v.(map[string]any); ok {
					merged[k] = MergeMaps(cast, deepCopyMap(vmap))
				}
			case []any:
				// the types must match, otherwise we can't merge them
				if varray, ok := v.([]any); ok {
					merged[k] = append(cast, deepCopyElements(varray))
				}
			}
		}

		merged[k] = deepCopy(v)
	}

	return merged
}
