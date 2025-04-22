// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

func Cmd() *cobra.Command {
	var config StatusConfig

	cmd := &cobra.Command{
		Use:     "status",
		Example: "status [--statuses-file ./path/to/statuses.yaml]",
		Run: func(cmd *cobra.Command, args []string) {
			if err := Render(config); err != nil {
				log.Fatal(err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&config.StatusesFile, "statuses-file", "statuses.yaml", "The location of the statuses file to read.")
	cmd.Flags().StringVar(&config.Package, "output-package", "statuses", "The go package name.")
	cmd.Flags().StringVar(&config.Outputs.Directory, "output-directory", ".", "The output directory.")
	cmd.Flags().StringVar(&config.Outputs.StatusFile, "output-status-file", "zz_generated_status.go", "The output file for statuses.")
	cmd.Flags().StringVar(&config.Outputs.StatusTestFile, "output-status-test-file", "zz_generated_status_test.go", "The output file for status tests.")
	cmd.Flags().StringVar(&config.Outputs.StateFile, "output-state-file", "zz_generated_state.go", "The output file for state machines.")
	cmd.Flags().StringVar(&config.APIDirectory, "api-directory", "api", "The directory where our api definitions are held.")
	cmd.Flags().StringVar(&config.BasePackage, "base-package", "github.com/redpanda-data/redpanda-operator/operator", "The base package name.")
	cmd.Flags().BoolVar(&config.RewriteComments, "rewrite-comments", false, "If specified, rewrite comments for structures.")

	return cmd
}
