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
package changing_inputs

import (
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func ChangingInputs(dot *helmette.Dot) map[string]any {
	for k, v := range dot.Values {
		_, ok_1 := v.(string)
		if ok_1 {
			dot.Values[k] = "change that"
		}
	}
	return nil
}
