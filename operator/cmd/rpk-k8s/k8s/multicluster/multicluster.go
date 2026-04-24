// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "multicluster",
		Short: "Manage Redpanda multicluster deployments on Kubernetes",
		Long:  "Commands for bootstrapping and managing Redpanda multicluster deployments across multiple Kubernetes clusters.",
	}

	cmd.AddCommand(
		bootstrapCommand(),
		statusCommand(),
		checkCommand(),
	)

	return cmd
}
