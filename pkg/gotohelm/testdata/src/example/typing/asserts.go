// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package typing

import (
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func typeTesting(dot *helmette.Dot) string {
	t := dot.Values["t"]

	if _, ok := t.(string); ok {
		return "it's a string!"
	} else if _, ok := helmette.AsIntegral[int](t); ok {
		return "it's an int!"
	} else if _, ok := helmette.AsNumeric(t); ok {
		return "it's a float!"
	}

	return "it's something else!"
}

func typeAssertions(dot *helmette.Dot) string {
	return "Not yet supported"
	// _ = dot.Values["no-such-key"].(int)
	// return "Didn't panic!"
}

func typeSwitching(dot *helmette.Dot) string {
	return "Not yet supported"
	// switch dot.Values["t"].(type) {
	// case int:
	// 	return "it's an int!"
	// case string:
	// 	return "it's a string!"
	// case float64:
	// 	return "it's a float64!"
	// case bool:
	// 	return "it's a bool!"
	// default:
	// 	return "it's something else"
	// }
}
