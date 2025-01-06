// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package envsubst implements a go version of the `envsubst` command.
package envsubst

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "envsubst file [--output file]",
		Short: "envsubst injects environment variables into the provided file and prints it to stdout",
		Long: `envsubst is a minimal re-implementation of envsubst[1] for usage in environments where it is not available.
It utilizes and therefore follows the semantics of os.ExpandEnv:

It replaces ${var} or $var in the string according to the values of the current
environment variables. References to undefined variables are replaced by the
empty string.

[1]: https://www.gnu.org/software/gettext/manual/html_node/envsubst-Invocation.html
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := nopCloser(cmd.OutOrStdout())

			if output != "" && output != "-" {
				var err error
				out, err = os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o775)
				if err != nil {
					return err
				}
			}

			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			expanded := os.ExpandEnv(string(data))

			if _, err := out.Write([]byte(expanded)); err != nil {
				return err
			}
			return out.Close()
		},
	}

	cmd.Flags().StringVarP(&output, "output", "p", "-", "The file to write the results to or - for stdout")

	return cmd
}

type nopWriterCloser struct {
	io.Writer
}

func (nopWriterCloser) Close() error {
	return nil
}

func nopCloser(w io.Writer) io.WriteCloser {
	return nopWriterCloser{w}
}
