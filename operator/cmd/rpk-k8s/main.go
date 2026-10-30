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
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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

// pluginHelp is one entry of the --help-autocomplete output rpk reads.
// Use is the command's cobra Use line, positional arguments included, so
// rpk can print them in the command's usage. Older rpk ignores it.
type pluginHelp struct {
	Path    string       `json:"path"`
	Short   string       `json:"short"`
	Long    string       `json:"long"`
	Example string       `json:"example"`
	Args    []string     `json:"args"`
	Use     string       `json:"use"`
	Flags   []pluginFlag `json:"flags"`
}

// pluginFlag matches the shape rpk prints for its own flags in
// --print-tree, so rpk can document plugin flags the same way.
type pluginFlag struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default"`
	Required    bool   `json:"required"`
	Deprecated  string `json:"deprecated,omitempty"`
}

func collectHelps(prefix string, cmd *cobra.Command) []pluginHelp {
	var helps []pluginHelp

	helps = append(helps, pluginHelp{
		Path:    prefix,
		Short:   cmd.Short,
		Long:    cmd.Long,
		Example: cmd.Example,
		Args:    pluginHelpArgs(cmd),
		Use:     cmd.Use,
		Flags:   pluginHelpFlags(cmd),
	})

	for _, sub := range cmd.Commands() {
		// Cobra only adds its help and completion commands at Execute
		// time, but skip them in case that ever changes.
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		childPrefix := prefix + "_" + sub.Name()
		helps = append(helps, collectHelps(childPrefix, sub)...)
	}

	return helps
}

// pluginHelpArgs returns the flag names rpk offers for completion: every
// non-hidden local and inherited flag, as "--name".
func pluginHelpArgs(cmd *cobra.Command) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "--" + f.Name
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	cmd.LocalFlags().VisitAll(add)
	cmd.InheritedFlags().VisitAll(add)
	return out
}

// pluginHelpFlags returns the flags defined on cmd itself, skipping hidden
// flags and help. Inherited flags are reported on the command that defines
// them.
func pluginHelpFlags(cmd *cobra.Command) []pluginFlag {
	// rpk expects an array, never null.
	flags := []pluginFlag{}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		flags = append(flags, pluginFlag{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Type:        f.Value.Type(),
			Description: f.Usage,
			Default:     parseDefault(f),
			Required:    slices.Contains(f.Annotations[cobra.BashCompOneRequiredFlag], "true"),
			Deprecated:  f.Deprecated,
		})
	})
	return flags
}

// parseDefault is copied from rpk's printtree.go so plugin defaults are
// typed the same way as rpk's own.
func parseDefault(f *pflag.Flag) any {
	t := f.Value.Type()
	if strings.HasSuffix(t, "Slice") || strings.HasSuffix(t, "Array") {
		if f.DefValue == "" || f.DefValue == "[]" {
			return []any{}
		}
		return f.DefValue
	}
	if f.DefValue == "" {
		if t == "string" {
			return ""
		}
		return nil
	}
	switch t {
	case "bool":
		if b, err := strconv.ParseBool(f.DefValue); err == nil {
			return b
		}
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "count":
		if n, err := strconv.ParseInt(f.DefValue, 10, 64); err == nil {
			return n
		}
	case "float32", "float64":
		if x, err := strconv.ParseFloat(f.DefValue, 64); err == nil {
			return x
		}
	}
	return f.DefValue
}
