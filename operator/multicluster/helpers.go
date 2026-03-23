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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
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

// namedAPIListeners returns a slice of (prefix, listener) pairs for the four
// API listeners (Admin, Kafka, HTTP, SchemaRegistry), skipping any that are nil.
// This eliminates repeated if-nil-check blocks when iterating over listener types.
func namedAPIListeners(l *redpandav1alpha2.StretchListeners) []struct {
	Prefix   string
	Listener *redpandav1alpha2.StretchAPIListener
} {
	if l == nil {
		return nil
	}
	type entry = struct {
		Prefix   string
		Listener *redpandav1alpha2.StretchAPIListener
	}
	var out []entry
	for _, e := range []entry{
		{"admin", l.Admin},
		{"kafka", l.Kafka},
		{"http", l.HTTP},
		{"schema", l.SchemaRegistry},
	} {
		if e.Listener != nil {
			out = append(out, e)
		}
	}
	return out
}

// externalServicePorts collects external listener ServicePorts across all API listeners.
// When includeNodePort is true, the first advertised port (if any) is set as NodePort.
func externalServicePorts(l *redpandav1alpha2.StretchListeners, includeNodePort bool) []corev1.ServicePort {
	var ports []corev1.ServicePort
	for _, entry := range namedAPIListeners(l) {
		forEachEnabledExternal(entry.Listener.External, func(name string, ext *redpandav1alpha2.StretchExternalListener) {
			port := ext.GetPort(0)
			if port == 0 {
				return
			}
			sp := corev1.ServicePort{
				Name:     fmt.Sprintf("%s-%s", entry.Prefix, name),
				Protocol: corev1.ProtocolTCP,
				Port:     port,
			}
			if includeNodePort {
				sp.NodePort = port
				if len(ext.AdvertisedPorts) > 0 {
					sp.NodePort = ext.AdvertisedPorts[0]
				}
			}
			ports = append(ports, sp)
		})
	}
	return ports
}

// setPtr sets dst[key] = *val if val is non-nil.
func setPtr[T any](dst map[string]any, key string, val *T) {
	if val != nil {
		dst[key] = *val
	}
}

// certServerVolumeName returns the volume name for a server cert.
func certServerVolumeName(certName string) string {
	return fmt.Sprintf("redpanda-%s-cert", certName)
}

// certClientVolumeName returns the volume name for a client cert.
func certClientVolumeName(certName string) string {
	return fmt.Sprintf("redpanda-%s-client-cert", certName)
}

// certServerMountPoint returns the mount path for a server cert.
func certServerMountPoint(certName string) string {
	return fmt.Sprintf("/etc/tls/certs/%s", certName)
}

// certClientMountPoint returns the mount path for a client cert.
func certClientMountPoint(certName string) string {
	return fmt.Sprintf("/etc/tls/certs/%s-client", certName)
}
