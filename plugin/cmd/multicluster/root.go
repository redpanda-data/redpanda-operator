// Copyright 2025 Redpanda Data, Inc.
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

type MulticlusterOptions struct{}

func (o *MulticlusterOptions) BindFlags(cmd *cobra.Command) {}

func (o *MulticlusterOptions) Validate() error {
	return nil
}

func Command() *cobra.Command {
	var options MulticlusterOptions

	cmd := &cobra.Command{
		Use: "multicluster",
	}

	options.BindFlags(cmd)

	cmd.AddCommand(CheckCommand(&options))
	cmd.AddCommand(RotateCertificatesCommand(&options))

	return cmd
}
