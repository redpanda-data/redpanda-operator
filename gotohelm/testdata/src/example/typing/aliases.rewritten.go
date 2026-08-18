//go:build rewrites

// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package typing

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Since go 1.23 (gotypesalias=1) go/types materializes aliases as
// *types.Alias rather than resolving them away. The transpiler has to unwrap
// them itself; see typeAliases in typing.go for the cases that worked even
// before it did.

// Bytes aliases a basic type. `omitempty` must be respected through the alias
// exactly as it is for int64 itself, or the zero value the transpiler emits
// gains a key that go's json.Marshal omits.
type Bytes = int64

// Stamp aliases a type that zeroOf special cases by name. The special case is
// keyed off the type's string, so it only still matches if the alias is
// resolved first.
type Stamp = metav1.Time

// Meta aliases a struct that's then embedded (and JSON inlined) into
// AliasEmbedder. Flattening the embed has to look through the alias to reach
// the underlying struct.
type Meta = Object

type AliasEmbedder struct {
	Meta `json:",inline"`
	Size Bytes `json:"size,omitempty"`
}

// Describe is declared on an alias, which makes it a method on Object.
func (m Meta) Describe() string {
	return m.Key
}

func aliases() []any {
	var size Bytes
	var stamp Stamp

	described := AliasEmbedder{Meta: Meta{Key: "described"}}

	return []any{
		size,
		stamp,
		AliasEmbedder{},
		AliasEmbedder{
			Meta: Meta{Key: "aliased"},
			Size: Bytes(64),
		},
		described.Describe(),
	}
}
