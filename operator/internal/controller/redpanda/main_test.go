// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"testing"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil"
)

func TestMain(m *testing.M) {
	otelutil.TestMain(m, "controller-redpanda")
}
