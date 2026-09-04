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
	"bytes"
	"fmt"
	"strings"

	gciconfig "github.com/daixiang0/gci/pkg/config"
	"github.com/daixiang0/gci/pkg/gci"
	gcilog "github.com/daixiang0/gci/pkg/log"
	"golang.org/x/tools/go/analysis"
	"mvdan.cc/gofumpt/format"
)

// The formatters block of .golangci.yml. Each is a check that reports an
// unformatted file and carries the formatted text as its fix, so
// `houselint -fix -gofumpt -gci` is `task fmt`.

func newGofumpt() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "gofumpt",
		Doc:  "Checks that files are formatted with gofumpt",
		Run: func(pass *analysis.Pass) (any, error) {
			opts := format.Options{}
			if pass.Module != nil {
				opts.LangVersion = "go" + strings.TrimPrefix(pass.Module.GoVersion, "go")
				opts.ModulePath = pass.Module.Path
			}

			return checkFormatting(pass, "gofumpt", func(_ string, src []byte) ([]byte, error) {
				return format.Source(src, opts)
			})
		},
	}
}

func newGci() *analysis.Analyzer {
	// gci formats through a package-level logger that only its own command
	// initialises; without this it dereferences nil on the first file.
	gcilog.InitLogger()

	var s struct {
		Sections         []string `json:"sections"`
		CustomOrder      bool     `json:"custom-order"`
		NoInlineComments bool     `json:"no-inline-comments"`
		NoPrefixComments bool     `json:"no-prefix-comments"`
	}
	settings("gci", &s)

	// Parsed the way gci's own CLI does, so the section separators get their defaults.
	conf, err := gciconfig.YamlConfig{
		Cfg:            gciconfig.BoolConfig{CustomOrder: s.CustomOrder, NoInlineComments: s.NoInlineComments, NoPrefixComments: s.NoPrefixComments},
		SectionStrings: s.Sections,
	}.Parse()
	if err != nil {
		fatalf("settings for gci: %v", err)
	}

	return &analysis.Analyzer{
		Name: "gci",
		Doc:  "Checks that imports are grouped and ordered per gci",
		Run: func(pass *analysis.Pass) (any, error) {
			return checkFormatting(pass, "gci", func(name string, src []byte) ([]byte, error) {
				_, formatted, err := gci.LoadFormat(src, name, *conf)

				return formatted, err
			})
		},
	}
}

// checkFormatting reports each file whose formatted text differs from its
// source. The fix replaces the whole file, which is what a formatter does.
func checkFormatting(pass *analysis.Pass, tool string, formatFn func(name string, src []byte) ([]byte, error)) (any, error) {
	for _, f := range pass.Files {
		file := pass.Fset.File(f.FileStart)

		src, err := pass.ReadFile(file.Name())
		if err != nil {
			return nil, err
		}

		formatted, err := formatFn(file.Name(), src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name(), err)
		}

		if !bytes.Equal(src, formatted) {
			pass.Report(analysis.Diagnostic{
				Pos:     f.FileStart,
				End:     f.FileEnd,
				Message: "file is not formatted with " + tool,
				SuggestedFixes: []analysis.SuggestedFix{{
					Message:   "format with " + tool,
					TextEdits: []analysis.TextEdit{{Pos: file.Pos(0), End: file.Pos(file.Size()), NewText: formatted}},
				}},
			})
		}
	}

	return nil, nil
}
