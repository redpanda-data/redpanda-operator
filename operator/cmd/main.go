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

	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/configurator"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/envsubst"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/run"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
)

var (
	rootCmd = cobra.Command{
		Use: "redpanda-operator",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Configure logging consistently for all sub-commands.
			// NB: If a subcommand relies on outputting to stdout, logging may
			// cause issues as it's default output it stdout.
			ctrl.SetLogger(logger.NewLogger(logOptions))
		},
	}

	logOptions logger.Options
)

func init() {
	rootCmd.AddCommand(
		configurator.Command(),
		envsubst.Command(),
		run.Command(),
		syncclusterconfig.Command(),
		version.Command(),
	)

	logOptions.BindFlags(rootCmd.PersistentFlags())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}
