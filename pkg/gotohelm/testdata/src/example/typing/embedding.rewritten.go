//go:build rewrites
package typing

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
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
