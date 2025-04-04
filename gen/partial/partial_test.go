// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//lint:file-ignore ST1019 duplicate imports are on purpose
package partial

import (
	//nolint:stylecheck
	"bytes"
	//nolint:stylecheck
	alias2 "bytes"
	alias1 "os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

type (
	IntAlias          int
	MapAlias          map[string]int
	MapGeneric[T any] map[string]T
)

type ExampleStruct struct {
	// Generics
	A1 MapGeneric[int]
	A2 MapGeneric[NestedStruct]
	A3 MapGeneric[*NestedStruct]
	A4 MapGeneric[IntAlias]
	A5 MapGeneric[MapGeneric[IntAlias]]
	A6 MapGeneric[MapGeneric[alias1.File]]
	A7 MapGeneric[MapGeneric[alias1.FileInfo]]
	A8 GenericStruct[string]
	A9 GenericStruct[GenericStruct[*int]]

	// BasicTypes
	B1 int
	B2 *int

	// Inline structs
	C1 struct {
		Any any
		Int int
	}
	C2 *struct{}

	// Structs
	NestedStruct
	D1 NestedStruct
	D2 *NestedStruct

	// Slices
	E1 []any
	E2 []int
	E3 []*int
	E4 []map[string]struct{ Foo string }
	E5 []map[string]NestedStruct
	E6 []map[string]GenericStruct[NestedStruct]

	// Tags
	F1 []*int `json:"L"`
	F2 string `yaml:"M"`
	F3 IntAlias

	// Struct from another package
	G1 bytes.Buffer
	G2 alias1.File
	G3 alias2.Reader

	// Maps
	H1 map[string]any
	H2 map[string]NestedStruct
	H3 map[string]GenericStruct[int]
	H4 map[string]GenericStruct[NestedStruct]
	H5 MapAlias
}

type NestedStruct struct {
	Map map[string]string
}

type GenericStruct[T any] struct {
	Foo T
}

func TestGenerateParital(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode:       mode,
		BuildFlags: []string{"-tags=generate"},
		Tests:      true,
	}, ".")
	require.NoError(t, err)

	// Loading with tests is weird but it let's us load up the example struct
	// seen above.
	require.Len(t, pkgs, 3)
	pkg := pkgs[1]
	require.Equal(t, "partial", pkg.Name)

	require.EqualError(t, GeneratePartial(pkg, "Values", nil), `named struct not found in package "partial": "Values"`)

	var buf bytes.Buffer
	require.NoError(t, GeneratePartial(pkg, "ExampleStruct", &buf))
	testutil.AssertGolden(t, testutil.Text, "./testdata/partial.go", buf.Bytes())
}

func TestEnsureOmitEmpty(t *testing.T) {
	cases := []struct {
		In  string
		Out string
	}{
		{In: ``, Out: `json:",omitempty"`},
		{In: `yaml:"foo"`, Out: `yaml:"foo" json:",omitempty"`},
		{In: `json:"bar"`, Out: `json:"bar,omitempty"`},
		{In: `json:"baz,omitempty"`, Out: `json:"baz,omitempty"`},
		{In: `yaml:"foo" json:"baz,omitempty"`, Out: `yaml:"foo" json:"baz,omitempty"`},
		{In: `json:"baz" yaml:"bar"`, Out: `json:"baz,omitempty" yaml:"bar"`},
		{In: `json:"-"`, Out: `json:"-,omitempty"`},
		{In: `json:"-,string"`, Out: `json:"-,string,omitempty"`},
		{In: `json:"-,omitempty,string"`, Out: `json:"-,omitempty,string"`},
	}

	for _, tc := range cases {
		assert.Equal(t, EnsureOmitEmpty(tc.In), tc.Out)
	}
}
