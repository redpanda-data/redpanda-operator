//go:build rewrites
package consumer

import (
	dependencyv2 "example.com/example/dependency/v2"
	dependencyv3 "example.com/example/dependency/v3"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func Render(dot *helmette.Dot) []any {
	return []any{
		dependencyv2.RenderThing("eat your heart out"),
		dependencyv3.RenderThing("subcharts"),
	}
}
