// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
package files

import "github.com/redpanda-data/redpanda-operator/gotohelm/helmette"

func Files(dot *helmette.Dot) []any {
	return []any{
		dot.Files.Get("hello.txt"),
		dot.Files.GetBytes("hello.txt"),
		dot.Files.Lines("something.yaml"),
		dot.Files.Get("doesntexist"),
		dot.Files.GetBytes("doesntexist"),
		dot.Files.Lines("doesntexist"),
	}
}
