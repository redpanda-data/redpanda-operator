// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package k8s

import (
	"runtime"

	"github.com/spf13/cobra"
)

// These variables are populated via -ldflags at build time from the
// OPERATOR_VERSION taskfile variable.
var (
	Version   string
	commit    string
	buildDate string
)

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("Version: %s\n", Version)
			cmd.Printf("Commit: %s\n", commit)
			cmd.Printf("Go Version: %s\n", runtime.Version())
			cmd.Printf("Build Date: %s\n", buildDate)
		},
	}
}
