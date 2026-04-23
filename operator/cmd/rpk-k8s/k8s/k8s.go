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
	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "k8s",
		Short: "Interact with Redpanda clusters running on Kubernetes",
		Long:  "Commands for managing and interacting with Redpanda clusters deployed on Kubernetes.",
	}

	cmd.AddCommand(
		multicluster.Command(),
		versionCommand(),
	)

	return cmd
}
