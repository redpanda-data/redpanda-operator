// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"fmt"
	"os"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/cmd/configurator"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/cmd/envsubst"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/cmd/run"
	"github.com/spf13/cobra"
)

var rootCmd = cobra.Command{
	Use: "redpanda-operator",
}

func init() {
	rootCmd.AddCommand(
		configurator.Command(),
		envsubst.Command(),
		run.Command(),
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}
