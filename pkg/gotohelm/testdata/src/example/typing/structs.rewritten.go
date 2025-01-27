//go:build rewrites
// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:all
package typing

type NestedEmbeds struct {
	WithEmbed
}

type Object struct {
	Key     string
	WithTag int `json:"with_tag"`
}

type WithEmbed struct {
	Object
	Exclude string  `json:"-"`
	Omit    *string `json:"Omit,omitempty"`
	Nilable *int
}

type JSONKeys struct {
	Value    string      `json:"val,omitempty"`
	Children []*JSONKeys `json:"childs,omitempty"`
}

func zeros() []any {
	var number *int
	var str *string
	var stru *Object

	return []any{
		Object{},
		WithEmbed{},
		number,
		str,
		stru,
	}
}

func nestedFieldAccess() string {
	x := JSONKeys{
		Children: []*JSONKeys{
			{
				Children: []*JSONKeys{
					{Value: "Hello!"},
				},
			},
		},
	}

	return x.Children[0].Children[0].Value
}

func settingFields() []string {
	var out NestedEmbeds

	out.WithEmbed = WithEmbed{Object: Object{Key: "foo"}}
	out.Object = Object{Key: "bar"}
	out.Key = "quux"

	return []string{
		out.Key,
		out.Object.Key,
		out.WithEmbed.Key,
	}
}

func compileMe() Object {
	return Object{
		Key: "foo",
	}
}

func alsoMe() WithEmbed {
	return WithEmbed{
		Object: Object{
			Key: "Foo",
		},
		Exclude: "Exclude",
	}
}
