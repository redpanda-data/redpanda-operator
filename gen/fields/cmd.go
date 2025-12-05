// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package fields

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
)

var (
	fieldsGenerator *template.Template
	//go:embed templates/fields.tpl
	fieldsTemplate string

	fieldsTestGenerator *template.Template
	//go:embed templates/fields_test.tpl
	fieldsTestTemplate string
)

func init() {
	helpers := map[string]any{
		"year": func() string {
			return time.Now().Format("2006")
		},
		"add": func(a int, b int) int {
			return a + b
		},
		"jsonAlias": func(a string) string {
			name := goName(a)
			return strings.ToLower(string(name[0])) + name[1:]
		},
		"goName": goName,
	}
	fieldsGenerator = template.Must(template.New("fields").Funcs(helpers).Parse(fieldsTemplate))
	fieldsTestGenerator = template.Must(template.New("fields_test").Funcs(helpers).Parse(fieldsTestTemplate))
}

type FieldsConfig struct {
	Version     string
	PackageName string
	OutputFile  string
	Verbose     bool
}

// Debugf emits debug output when the config's Verbose flag is set.
func (c FieldsConfig) Debugf(format string, a ...interface{}) {
	if !c.Verbose {
		return
	}
	fmt.Fprintf(os.Stderr, format, a...)
}

func Cmd() *cobra.Command {
	var config FieldsConfig

	cmd := &cobra.Command{
		Use:     "fields",
		Short:   "Generate tests for deprecated API fields",
		Example: "gen fields --version 25.3.1 --output zz_generated.fields.go",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Render(config)
		},
	}

	cmd.Flags().StringVar(&config.Version, "version", "", "The version of the Redpanda config to generate fields from")
	cmd.Flags().StringVar(&config.PackageName, "package", "v1alpha2", "The name of the package")
	cmd.Flags().StringVar(&config.OutputFile, "output", "zz_generated.fields.go", "The path of the file to output")
	cmd.Flags().BoolVarP(&config.Verbose, "verbose", "v", false, "Enable debug output.")

	return cmd
}

func Render(config FieldsConfig) error {
	if config.Version == "" {
		return errors.New("a version must be specified")
	}

	generator := NewGenerator()

	output, testOutput, err := generator.Generate(config.PackageName, config.Version)
	if err != nil {
		return err
	}

	file := path.Base(config.OutputFile)
	testFile := strings.TrimSuffix(file, ".go") + "_test.go"

	directory := path.Dir(config.OutputFile)

	//nolint:gosec // this is in a generator and this is the default mode for folders
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(directory, file), output, 0o644); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(directory, testFile), testOutput, 0o644); err != nil {
		return err
	}

	config.Debugf(string(output))

	return nil
}
