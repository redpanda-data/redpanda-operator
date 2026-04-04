// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/gen/partial"
	"github.com/redpanda-data/redpanda-operator/gen/pipeline"
	"github.com/redpanda-data/redpanda-operator/gen/schema"
	"github.com/redpanda-data/redpanda-operator/gen/status"
)

func main() {
	root := cobra.Command{
		Use:  "gen",
		Long: "gen is the hub module for hosting various file generation tasks within this repo.",
	}

	root.AddCommand(
		partial.Cmd(),
		pipeline.Cmd(),
		schema.Cmd(),
		status.Cmd(),
	)

	if err := root.Execute(); err != nil {
		panic(err)
	}
}
