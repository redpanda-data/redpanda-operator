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
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"

	"github.com/redpanda-data/redpanda-operator/gotohelm"
)

func main() {
	var out string
	var bundled []string

	cmd := cobra.Command{
		Use:  "gotohelm <package to transpile> [--bundle package/to/bundle] [subcharts...]",
		Args: cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cwd, _ := os.Getwd()

			pkgs, err := gotohelm.LoadPackages(&packages.Config{
				Dir: cwd,
			}, append(bundled, args[0])...)
			if err != nil {
				panic(err)
			}

			chart, err := gotohelm.Transpile(pkgs, args[1:]...)
			if err != nil {
				fmt.Printf("Failed to transpile %q: %s\n", args[0], err)
				os.Exit(1)
			}

			if out == "-" {
				writeToStdout(chart)
			} else {
				if err := writeToDir(chart, out); err != nil {
					panic(err)
				}
			}
		},
	}

	cmd.Flags().StringArrayVarP(&bundled, "bundle", "b", []string{}, "dependency packages that will be bundled into the final output")
	cmd.Flags().StringVarP(&out, "write", "w", "-", "The directory to write the transpiled templates to or - to write them to standard out")

	if err := cmd.Execute(); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
}

func writeToStdout(chart *gotohelm.Chart) {
	for _, f := range chart.Files {
		fmt.Printf("%s\n", f.Name)
		f.Write(os.Stdout)
		fmt.Printf("\n\n")
	}
}

func writeToDir(chart *gotohelm.Chart, dir string) error {
	for _, f := range chart.Files {
		file, err := os.OpenFile(path.Join(dir, f.Name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec
		if err != nil {
			return err
		}

		f.Write(file)

		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}
