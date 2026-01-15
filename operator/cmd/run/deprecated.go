// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import "github.com/spf13/cobra"

var (
	deprecatedStringFlags = []string{
		"events-addr", "helm-repository-url",
	}
	deprecatedBoolFlags = []string{
		"debug", "enable-helm-controllers", "force-defluxed-mode",
		"allow-pvc-deletion", "operator-mode", "enable-shadowlinks",
	}
)

func addDeprecatedFlags(cmd *cobra.Command) {
	for _, f := range deprecatedStringFlags {
		_ = cmd.Flags().String(f, "", "A deprecated and unused flag")
		_ = cmd.Flags().MarkHidden(f)
	}
	for _, f := range deprecatedBoolFlags {
		_ = cmd.Flags().Bool(f, false, "A deprecated and unused flag")
		_ = cmd.Flags().MarkHidden(f)
	}
}

func checkDeprecatedFlags(cmd *cobra.Command) []string {
	inUse := []string{}
	for _, f := range deprecatedStringFlags {
		if v, err := cmd.Flags().GetString(f); err == nil && v != "" {
			inUse = append(inUse, f)
		}
	}
	for _, f := range deprecatedBoolFlags {
		if v, err := cmd.Flags().GetBool(f); err == nil && v {
			inUse = append(inUse, f)
		}
	}
	return inUse
}
