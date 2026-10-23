// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"encoding/json"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// sortedMapKeys returns the keys of a map sorted alphabetically.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// structuredTpl recurses through all fields of T and expands
// any string fields containing template delimiters with [tpl].
func structuredTpl[T any](state *RenderState, in T) (T, error) {
	return tplutil.StructuredTpl(in, state.tplData())
}

// forEachEnabledExternal iterates over enabled external listeners in sorted key order,
// skipping nil or disabled entries.
func forEachEnabledExternal(externals map[string]*redpandav1alpha2.StretchExternalListener, fn func(name string, ext *redpandav1alpha2.StretchExternalListener)) {
	for _, name := range sortedMapKeys(externals) {
		if ext := externals[name]; ext != nil && ext.IsEnabled() {
			fn(name, ext)
		}
	}
}

// mergeRawExtension unmarshals a RawExtension into the target map, merging its
// keys. Existing keys in dst are overwritten. If raw is nil or cannot be
// unmarshalled, dst is left unchanged.
func mergeRawExtension(dst map[string]any, raw *runtime.RawExtension) {
	if raw == nil || raw.Raw == nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(raw.Raw, &m); err != nil {
		return
	}
	for k, v := range m {
		dst[k] = v
	}
}

// setPtr sets dst[key] = *val if val is non-nil.
func setPtr[T any](dst map[string]any, key string, val *T) {
	if val != nil {
		dst[key] = *val
	}
}
