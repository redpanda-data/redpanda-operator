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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	rpkk8s "github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s"
)

func main() {
	k8s := rpkk8s.Command()

	// rpk plugin autocomplete support: when invoked with
	// --help-autocomplete, emit JSON describing all subcommands
	// so rpk can register them for shell completion.
	if len(os.Args) > 1 && os.Args[1] == "--help-autocomplete" {
		helps := collectHelps("k8s", k8s)
		out, err := json.Marshal(helps)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error marshalling help: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
		return
	}

	// Wrap k8s under a phantom "rpk" root so cobra renders usage paths as
	// "rpk k8s ..." instead of "k8s ...". Cobra derives CommandPath from
	// parent names, so the phantom parent is the only way to inject "rpk"
	// into the displayed path. We prepend "k8s" to the real args so routing
	// goes through the full phantom→k8s→... tree.
	root := &cobra.Command{
		Use:          "rpk",
		SilenceUsage: true,
	}
	root.AddCommand(k8s)
	root.SetArgs(append([]string{"k8s"}, os.Args[1:]...))

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

type pluginHelp struct {
	Path    string   `json:"path"`
	Short   string   `json:"short"`
	Long    string   `json:"long"`
	Example string   `json:"example"`
	Args    []string `json:"args"`
}

func collectHelps(prefix string, cmd *cobra.Command) []pluginHelp {
	var helps []pluginHelp

	helps = append(helps, pluginHelp{
		Path:    prefix,
		Short:   cmd.Short,
		Long:    cmd.Long,
		Example: cmd.Example,
		Args:    cmd.ValidArgs,
	})

	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		childPrefix := prefix + "_" + sub.Name()
		helps = append(helps, collectHelps(childPrefix, sub)...)
	}

	return helps
}
