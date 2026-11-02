// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCollectHelpsFlags(t *testing.T) {
	root := &cobra.Command{Use: "k8s", Short: "root"}
	root.PersistentFlags().String("kubeconfig", "", "path to kubeconfig")

	sub := &cobra.Command{
		Use:       "status <cluster>",
		Short:     "status short",
		ValidArgs: []string{"positional"},
		Run:       func(*cobra.Command, []string) {},
	}
	sub.Flags().BoolP("verbose", "v", false, "verbose output")
	sub.Flags().Int("replicas", 3, "replica count")
	sub.Flags().Duration("timeout", 30*time.Second, "timeout")
	sub.Flags().StringSlice("names", nil, "names")
	sub.Flags().String("namespace", "default", "namespace")
	sub.Flags().String("secret", "", "hidden flag")
	require.NoError(t, sub.Flags().MarkHidden("secret"))
	sub.Flags().String("must", "", "required flag")
	require.NoError(t, sub.MarkFlagRequired("must"))
	// MarkDeprecated also hides the flag, so it is skipped like rpk's own.
	sub.Flags().String("gone", "", "deprecated flag")
	require.NoError(t, sub.Flags().MarkDeprecated("gone", "use --must"))
	// Set Deprecated directly to cover the field on a visible flag.
	sub.Flags().String("old", "", "old flag")
	sub.Flags().Lookup("old").Deprecated = "use --must"

	bare := &cobra.Command{Use: "bare", Run: func(*cobra.Command, []string) {}}
	hidden := &cobra.Command{Use: "secret", Hidden: true, Run: func(*cobra.Command, []string) {}}
	root.AddCommand(sub, bare, hidden)
	// Cobra normally adds these at Execute time; add them so the walk
	// proves it skips them.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpFlag()
	sub.InitDefaultHelpFlag()

	helps := collectHelps("k8s", root)

	// Round-trip through JSON so defaults are checked as rpk sees them.
	out, err := json.Marshal(helps)
	require.NoError(t, err)
	var got []map[string]any
	require.NoError(t, json.Unmarshal(out, &got))

	var paths []any
	for _, h := range got {
		paths = append(paths, h["path"])
	}
	require.Equal(t, []any{"k8s", "k8s_bare", "k8s_status"}, paths, "help, completion and hidden commands must be skipped")
	require.Equal(t, "k8s", got[0]["use"])
	require.Equal(t, "bare", got[1]["use"])
	require.Equal(t, "status <cluster>", got[2]["use"], "use keeps the positional arguments; path uses the name")
	require.Equal(t, []any{}, got[1]["flags"], "flags must be [] rather than null")
	require.Equal(t, []any{"--kubeconfig"}, got[1]["args"])

	// The persistent flag appears once, on the command that defines it.
	require.Equal(t, []any{
		map[string]any{"name": "kubeconfig", "type": "string", "description": "path to kubeconfig", "default": "", "required": false},
	}, got[0]["flags"])
	require.Equal(t, []any{"--kubeconfig"}, got[0]["args"])

	require.Equal(t, []any{
		map[string]any{"name": "must", "type": "string", "description": "required flag", "default": "", "required": true},
		map[string]any{"name": "names", "type": "stringSlice", "description": "names", "default": []any{}, "required": false},
		map[string]any{"name": "namespace", "type": "string", "description": "namespace", "default": "default", "required": false},
		map[string]any{"name": "old", "type": "string", "description": "old flag", "default": "", "required": false, "deprecated": "use --must"},
		map[string]any{"name": "replicas", "type": "int", "description": "replica count", "default": float64(3), "required": false},
		map[string]any{"name": "timeout", "type": "duration", "description": "timeout", "default": "30s", "required": false},
		map[string]any{"name": "verbose", "shorthand": "v", "type": "bool", "description": "verbose output", "default": false, "required": false},
	}, got[2]["flags"])

	// args lists flag names for completion, local then inherited, not
	// ValidArgs.
	require.Equal(t, []any{
		"--must", "--names", "--namespace", "--old", "--replicas", "--timeout", "--verbose", "--kubeconfig",
	}, got[2]["args"])
}
