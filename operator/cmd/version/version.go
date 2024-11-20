// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package version

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// these variables are set via ldflags in our goreleaser build.

	version   string
	commit    string
	buildDate string
)

func init() {
	buildInfoGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "redpanda_operator",
		Name:      "build_info",
		Help:      "A gauge always set to 1 that provides build information as labels.",
		ConstLabels: prometheus.Labels{
			"version":    version,
			"commit":     commit,
			"go_version": runtime.Version(),
		},
	})

	buildInfoGauge.Set(1)

	metrics.Registry.MustRegister(buildInfoGauge)
}

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print build information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("Version: %s\n", version)
			cmd.Printf("Commit: %s\n", commit)
			cmd.Printf("Go Version: %s\n", runtime.Version())
			cmd.Printf("Build Date: %s\n", buildDate)
		},
	}
}
