// Copyright 2025 Redpanda Data, Inc.
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
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/bootstrap"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/configurator"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/envsubst"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/ready"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/run"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/sidecar"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
	"github.com/redpanda-data/redpanda-operator/operator/internal/timing"
)

var (
	outputTimingsOnly bool
	rootCmd           = cobra.Command{
		Use: "redpanda-operator",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if outputTimingsOnly {
				timing.SetupTimingOnlyLogger()
				return
			}

			// Configure logging consistently for all sub-commands.
			// NB: If a subcommand relies on outputting to stdout, logging may
			// cause issues as it's default output it stdout.
			log := logger.NewLogger(logOptions)
			klog.SetLogger(log)
			ctrl.SetLogger(log)
		},
	}

	logOptions logger.Options
)

func init() {
	rootCmd.AddCommand(
		configurator.Command(),
		bootstrap.Command(),
		envsubst.Command(),
		run.Command(),
		syncclusterconfig.Command(),
		version.Command(),
		sidecar.Command(),
		ready.Command(),
	)

	logOptions.BindFlags(rootCmd.PersistentFlags())
	rootCmd.PersistentFlags().BoolVar(&outputTimingsOnly, "output-timings-only", false, "Set this flag to only log instrumentation timings")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}
