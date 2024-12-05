// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build rewrites

package changing_inputs

import (
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func ChangingInputs(dot *helmette.Dot) map[string]any {
	for k, v := range dot.Values {
		tmp_tuple_1 := helmette.Compact2(helmette.TypeTest[string](v))
		ok_1 := tmp_tuple_1.T2
		if ok_1 {
			dot.Values[k] = "change that"
		}
	}
	return nil
}
