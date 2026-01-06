//go:build rewrites

// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
package typing

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

type EmbedValues struct {
	Jack *Dog
	June *Cat
}

func embedding(dot *helmette.Dot) []any {
	values := helmette.Unwrap[EmbedValues](dot.Values)

	// The typings package has a lot of test cases and I won't want
	// to update all of them. If our inputs aren't present, noop this
	// test case.
	if values.June == nil || values.Jack == nil {
		return nil
	}

	ashe := &Cat{
		Pet: Pet{
			Name: "Ashe",
		},
	}

	return []any{
		ashe.Greet(),
		ashe.Pet.Greet(),

		values.June.Greet(),
		values.June.Pet.Greet(),

		values.Jack.Greet(),
		values.Jack.Pet.Greet(),
	}
}

type Pet struct {
	Name string `json:"name"`
}

func (p *Pet) Greet() string {
	return fmt.Sprintf("Hello, %s!", p.Name)
}

type Cat struct {
	Pet          `json:",inline"`
	IsLongHaired bool
}

type Dog struct {
	Pet   `json:",inline"`
	Breed string
}
