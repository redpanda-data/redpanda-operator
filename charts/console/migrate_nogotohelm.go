// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build !gotohelm

package console

import (
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/itchyny/gojq"
)

// ConfigFromV2 transforms a Console v2 configuration into a v3 configuration.
// It additionally returns a string slice containing human readable
// descriptions of fields that could not be migrated.
//
// Unknown or invalid data is left in place for Console itself handle (by crashing / logging).
//
// V2 Config:
// - https://github.com/redpanda-data/console/blob/v2.8.10/backend/pkg/config/config.go
// - https://github.com/redpanda-data/console-enterprise/blob/v2.8.10/backend/pkg/config/config.go
//
// V3 Config:
// - https://github.com/redpanda-data/console/blob/v3.2.2/backend/pkg/config/config.go
// - https://github.com/redpanda-data/console-enterprise/blob/v3.2.2/backend/pkg/config/config.go
func ConfigFromV2(v2 map[string]any) (map[string]any, []string, error) {
	v3 := make(map[string]any)

	for _, m := range mappings {
		val, ok, err := execQueryScalar[any](v2, m.code)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to execute query for path %v", m.dst)
		}
		if !ok {
			continue
		}
		if err := setPath(v3, m.dst, val); err != nil {
			return nil, nil, errors.Wrapf(err, "failed to set path %v", m.dst)
		}
	}

	// Generate warnings
	var warnings []string
	for _, code := range warningQueries {
		results, err := execQuery[string](v2, code)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to generate warnings")
		}
		warnings = append(warnings, results...)
	}

	return v3, warnings, nil
}

type mappingSpec struct {
	dst   string
	query string
}

type mapping struct {
	dst  []string
	code *gojq.Code
}

// mappings is a slice of destination path to JQ query that together migrate a
// Console v2 config to a Console v3 config. Its behavior is exactly the same
// as the converter in our public docs (Thanks Jake!)
// https://github.com/redpanda-data/docs-ui/blob/d55545f392e0a9aaa0dbd193606a2b629d779699/console-config-migrator/main.go#L25
// Due to module dependency conflicts and the quasi closed source nature of
// console typing the configurations was deemed a non-option. JQ was elected as
// it's relatively well known and substantially easier to comprehend than
// map[string]any manipulations.
var mappings = compileMappings([]mappingSpec{
	// Authentication
	{"authentication.jwtSigningKey", ".login.jwtSecret"},
	{"authentication.useSecureCookies", ".login.useSecureCookies"},
	{"authentication.basic", ".login.plain"},
	{"authentication.oidc", `.login // {} | to_entries | sort_by(.key) | map(select(.value.enabled?) | .value) | first | del(.realm, .directory)`},

	// Schema Registry
	{"schemaRegistry.authentication.basic.username", ".kafka.schemaRegistry.username"},
	{"schemaRegistry.authentication.basic.password", ".kafka.schemaRegistry.password"},
	{"schemaRegistry.authentication.bearerToken", ".kafka.schemaRegistry.bearerToken"},
	{"schemaRegistry.authentication.impersonateUser", `.kafka.schemaRegistry | select(.) | .username == null`},
	{"schemaRegistry", `.kafka.schemaRegistry | del(.username, .password, .bearerToken)`},

	// Kafka
	{"kafka.sasl.enabled", "true"},
	{"kafka.sasl.impersonateUser", "true"},
	{"kafka", `.kafka | del(.schemaRegistry, .protobuf, .cbor, .messagePack)`},

	// Serde
	{"serde.protobuf", ".kafka.protobuf"},
	{"serde.cbor", ".kafka.cbor"},
	{"serde.messagePack", ".kafka.messagePack"},
	{"serde.maxDeserializationPayloadSize", ".console.maxDeserializationPayloadSize"},

	// Connect rename
	{"kafkaConnect", ".connect"},

	// Redpanda adminApi
	{"redpanda", `.redpanda | del(.adminApi.username, .adminApi.password)`},
	{"redpanda.adminApi.authentication.basic.username", ".redpanda.adminApi.username"},
	{"redpanda.adminApi.authentication.basic.password", ".redpanda.adminApi.password"},
	{"redpanda.adminApi.authentication.impersonateUser", `.redpanda.adminApi | select(.) | .username == null`},

	// RoleBindings
	{"authorization.roleBindings", `.roleBindings | map({
		roleName: .roleName,
		users: [.subjects[] | select(.kind == "user" or .kind == null) | {
			name: .name,
			loginType: (if .provider == "Plain" then "basic" else "oidc" end)
		}]
	})?`},

	// Copy remaining top-level fields (empty dst means merge into root)
	{"", "del(.connect, .console, .enterprise, .kafka, .login, .roleBindings, .redpanda)"},
})

// warningQueries is a slice of JQ queries that produce warnings about
// configurations that can not be migrate to Console V3.
var warningQueries = compileWarnings([]string{
	`.login // {} | to_entries | sort_by(.key) | map(select(.value.enabled?)) | select(length > 1) | "Elected '\(.[0].key)' as OIDC provider out of \(map(.key)). Only one provider is supported in v3."`,
	`.login // {} | to_entries | sort_by(.key) | .[] | select(.value.enabled? and .value.realm? != null) |  "Removed 'realm' from '\(.key)'. OIDC groups are not supported in v3. Create Roles in Redpanda instead."`,
	`.login // {} | to_entries | sort_by(.key) | .[] | select(.value.enabled? and .value.directory? != null) | select(length > 1) | "Removed 'directory' from '\(.key)'. OIDC groups are not supported in v3. Create Roles in Redpanda instead."`,
	`.roleBindings.[]? | . as $binding | .subjects.[]? | select(.kind != "user") | "Removed group subject from role binding '\($binding.roleName)'. Groups are not supported in v3."`,
})

func compileMappings(specs []mappingSpec) []mapping {
	mappings := make([]mapping, len(specs))
	for i, spec := range specs {
		var dst []string
		if spec.dst != "" {
			dst = strings.Split(spec.dst, ".")
		}
		mappings[i] = mapping{
			dst:  dst,
			code: mustCompile(spec.query),
		}
	}
	return mappings
}

func compileWarnings(queries []string) []*gojq.Code {
	compiled := make([]*gojq.Code, len(queries))
	for i, query := range queries {
		compiled[i] = mustCompile(query)
	}
	return compiled
}

func execQuery[T any](data map[string]any, code *gojq.Code) ([]T, error) {
	iter := code.Run(data)

	var results []T
	for {
		result, ok := iter.Next()
		if !ok {
			break
		}

		// gojq can produce some strange results:
		// - errors are returned through Next and must be checked
		// -
		switch result := result.(type) {
		// Errors are returned through .Next and need to be checked.
		case error:
			return nil, errors.WithStack(result)
		// nil (untyped) can be returned directly. We don't want to emit nils, so skip.
		case nil:
			continue
		// nil maps in an any box ( any(map[string]any(nil)) ) can also be
		// returned. They need to be unboxed and explicitly checked.
		case map[string]any:
			if result == nil {
				continue
			}
		}

		results = append(results, result.(T))
	}

	return results, nil
}

func execQueryScalar[T comparable](data map[string]any, code *gojq.Code) (T, bool, error) {
	var zero T
	out, err := execQuery[T](data, code)
	if err != nil {
		return zero, false, err
	}
	if len(out) == 1 {
		return out[0], out[0] != zero, nil
	}
	return zero, false, nil
}

func mustCompile(expr string) *gojq.Code {
	query, err := gojq.Parse(expr)
	if err != nil {
		panic(err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		panic(err)
	}
	return code
}

// setPath sets a value in a nested map structure using a slice of keys.
func setPath(m map[string]any, path []string, value any) error {
	// Special case, if path is empty merge value into m.
	if len(path) == 0 {
		valueMap, ok := value.(map[string]any)
		if !ok {
			return errors.Newf("cannot merge non-map value into root: %T", value)
		}
		mergeMaps(m, valueMap)
		return nil
	}

	curr := m
	for i := 0; i < len(path)-1; i++ {
		key := path[i]
		if _, exists := curr[key]; !exists {
			curr[key] = make(map[string]any)
		}

		next, ok := curr[key].(map[string]any)
		if !ok {
			return errors.Newf("cannot traverse through non-map at key %q: %T", key, curr[key])
		}
		curr = next
	}

	lastKey := path[len(path)-1]
	existing, exists := curr[lastKey]
	if !exists {
		curr[lastKey] = value
		return nil
	}

	// If both existing and new values are maps, merge them
	existingMap, existingIsMap := existing.(map[string]any)
	valueMap, valueIsMap := value.(map[string]any)
	if existingIsMap && valueIsMap {
		mergeMaps(existingMap, valueMap)
	} else {
		// Otherwise overwrite
		curr[lastKey] = value
	}

	return nil
}

func mergeMaps(dst, src map[string]any) {
	for k, v := range src {
		existing, exists := dst[k]
		if !exists {
			dst[k] = v
			continue
		}

		// If both are maps, merge recursively
		dstMap, dstIsMap := existing.(map[string]any)
		srcMap, srcIsMap := v.(map[string]any)
		if dstIsMap && srcIsMap {
			mergeMaps(dstMap, srcMap)
		} else {
			// Otherwise overwrite
			dst[k] = v
		}
	}
}
