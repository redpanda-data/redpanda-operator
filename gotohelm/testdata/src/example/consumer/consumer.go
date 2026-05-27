// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

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
