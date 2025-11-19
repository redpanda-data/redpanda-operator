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
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type CheckOptions struct{}

func (o *CheckOptions) BindFlags(cmd *cobra.Command) {
}

func CheckCommand(multiclusterOptions *MulticlusterOptions) *cobra.Command {
	var options CheckOptions

	cmd := &cobra.Command{
		Use: "check",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunCheck(cmd.Context(), &options, multiclusterOptions)
		},
	}

	options.BindFlags(cmd)

	return cmd
}

func RunCheck(
	ctx context.Context,
	opts *CheckOptions,
	multiclusterOpts *MulticlusterOptions,
) error {
	fmt.Println("check")
	return nil
}
