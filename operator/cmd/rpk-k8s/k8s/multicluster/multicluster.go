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
		Short: "Manage the multicluster operators that run a Redpanda Stretch Cluster",
		Long: `Commands for bootstrapping and managing the multicluster operators that run
a Redpanda Stretch Cluster: a single logical Redpanda cluster distributed
across multiple Kubernetes clusters. One operator runs in each Kubernetes
cluster, and the operators coordinate through Raft consensus to manage the
Stretch Cluster as a single unit.`,
	}

	cmd.AddCommand(
		bootstrapCommand(),
		bundleCommand(),
		statusCommand(),
		checkCommand(),
	)

	return cmd
}
