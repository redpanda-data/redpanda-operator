// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	//go:embed templates/status_test.tpl
	statusTestsTemplate string
	//go:embed templates/status.tpl
	statusTemplate      string
	statusGenerator     *template.Template
	statusTestGenerator *template.Template

	tombstoneMarker = "Z"
	matchTombstone  = regexp.MustCompile("^" + tombstoneMarker + "*$")

	helpers = map[string]any{
		"year": func() string {
			return time.Now().Format("2006")
		},
		"mergeImports": func(statuses []*status) []string {
			merged := []string{}
			for _, status := range statuses {
				merged = append(merged, status.Imports()...)
			}
			return slices.Compact(sort.StringSlice(merged))
		},
	}
)

func init() {
	statusGenerator = template.Must(template.New("statuses").Funcs(helpers).Parse(statusTemplate))
	statusTestGenerator = template.Must(template.New("statusTests").Funcs(helpers).Parse(statusTestsTemplate))
}

type StatusConfig struct {
	StatusesFile    string
	Package         string
	BasePackage     string
	APIDirectory    string
	RewriteComments bool
	Outputs         StatusConfigOutputs
}

type StatusConfigOutputs struct {
	Directory      string
	StatusFile     string
	StatusTestFile string
	StateFile      string
}

type templateInfo struct {
	Package  string
	File     string
	Statuses []*status
}

func Render(config StatusConfig) error {
	statuses, err := decode(config.StatusesFile)
	if err != nil {
		return err
	}

	for _, status := range statuses {
		status.normalize(config.BasePackage + "/" + config.APIDirectory)
	}

	if config.RewriteComments {
		if err := rewriteAPIComments(statuses, config); err != nil {
			return err
		}
	}

	return render(&templateInfo{
		Package:  config.Package,
		File:     config.StatusesFile,
		Statuses: statuses,
	}, config.Outputs)
}

func decode(name string) ([]*status, error) {
	var into []*status

	file, err := os.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewYAMLOrJSONDecoder(file, int(stat.Size()))
	if err = decoder.Decode(&into); err != nil {
		return nil, err
	}

	return into, nil
}

func render(info *templateInfo, outputs StatusConfigOutputs) error {
	if err := renderAndWrite(outputs.Directory, outputs.StatusFile, info, renderStatusFile); err != nil {
		return err
	}
	if err := renderAndWrite(outputs.Directory, outputs.StatusTestFile, info, renderStatusTestFile); err != nil {
		return err
	}

	return nil
}

func renderAndWrite(directory, file string, info *templateInfo, renderFn func(info *templateInfo) ([]byte, error)) error {
	data, err := renderFn(info)
	if err != nil {
		return err
	}
	formatted, err := format.Source(data)
	if err != nil {
		if contextualErrors := contextualizeFormatErrors(data, err); contextualErrors != "" {
			fmt.Println(contextualErrors)
		}
		return err
	}

	// ensure that the parent path exists
	//nolint:gosec // this is in a generator and this is the default mode for folders
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(directory, file), formatted, 0o644); err != nil {
		return err
	}
	return nil
}

func renderStatusFile(info *templateInfo) ([]byte, error) {
	var buffer bytes.Buffer
	if err := statusGenerator.Execute(&buffer, info); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func renderStatusTestFile(info *templateInfo) ([]byte, error) {
	var buffer bytes.Buffer
	if err := statusTestGenerator.Execute(&buffer, info); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func rewriteAPIComments(statuses []*status, config StatusConfig) error {
	collectedStructs := map[string]map[string]*status{}
	for i, s := range statuses {
		for _, structPath := range s.AppliesTo {
			path := filepath.Dir(structPath)
			name := filepath.Base(structPath)
			pathStructs := collectedStructs[path]
			if pathStructs == nil {
				pathStructs = map[string]*status{}
			}
			pathStructs[name] = statuses[i]
			collectedStructs[path] = pathStructs
		}
	}

	for path, structs := range collectedStructs {
		path := filepath.Join(config.APIDirectory, path)
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(path, entry.Name()), nil, parser.ParseComments)
			if err != nil {
				return err
			}

			modified := false
			ast.Inspect(file, func(node ast.Node) bool {
				if tombstoneComments(node, structs) {
					modified = true
				}

				return true
			})

			if modified {
				// first render out the AST again
				var buffer bytes.Buffer
				if err := printer.Fprint(&buffer, fset, file); err != nil {
					return err
				}

				// now remove all tombstones and replace with printer comments
				data, err := replaceTombstones(&buffer, structs)
				if err != nil {
					return err
				}

				if err := os.WriteFile(filepath.Join(path, entry.Name()), data, 0o644); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func replaceTombstones(r io.Reader, structs map[string]*status) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	var fileContents bytes.Buffer
	lastStructure := ""
	for scanner.Scan() {
		line := scanner.Bytes()
		lineString := strings.TrimSpace(string(line))
		if strings.HasPrefix(lineString, "//") {
			lineString = strings.TrimPrefix(lineString, "//")
			lineString = strings.TrimSpace(lineString)
			// just to make sure we're not having a valid comment
			minimumLine := 9
			if len(lineString) > minimumLine && matchTombstone.MatchString(lineString) {
				continue
			}
		}

		structureLine := false
		for name, status := range structs {
			// append all of our comments just before writing the line
			if strings.HasPrefix(lineString, fmt.Sprintf("type %s struct {", name)) {
				for _, condition := range status.Conditions {
					for _, column := range condition.PrinterColumns {
						if _, err := fileContents.WriteString(column.Comment() + "\n"); err != nil {
							return nil, err
						}
					}
				}
				// now make sure we know that we need to replace any Status comments
				lastStructure = name
				structureLine = true
				break
			}
		}

		if !structureLine && lastStructure != "" {
			if strings.HasPrefix(lineString, "Status") {
				status := structs[lastStructure]
				if _, err := fileContents.WriteString("\t" + status.DefaultStatusComment() + "\n"); err != nil {
					return nil, err
				}
				// now reset so we don't modify anything else
				lastStructure = ""
			}
		}

		if _, err := fileContents.Write(append(line, '\n')); err != nil {
			return nil, err
		}
	}

	data, err := format.Source(fileContents.Bytes())
	if err != nil {
		if contextualErrors := contextualizeFormatErrors(fileContents.Bytes(), err); contextualErrors != "" {
			fmt.Println(contextualErrors)
		}
	}

	return data, err
}

func tombstoneComments(node ast.Node, structs map[string]*status) bool {
	genDecl, ok := node.(*ast.GenDecl)
	if !ok || genDecl.Tok != token.TYPE {
		return false
	}

	for _, spec := range genDecl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			continue
		}

		if _, ok := structs[typeSpec.Name.Name]; !ok {
			return false
		}

		for _, field := range structType.Fields.List {
			isStatus := false
			for _, name := range field.Names {
				if name.Name == "Status" {
					isStatus = true
					break
				}
			}

			if !isStatus {
				continue
			}

			if field.Doc == nil {
				field.Doc = &ast.CommentGroup{}
			}
			for _, doc := range field.Doc.List {
				if strings.HasPrefix(doc.Text, "// +kubebuilder:default") {
					doc.Text = "//" + strings.Repeat(tombstoneMarker, len(doc.Text)-2)
					break
				}
			}
		}

		if genDecl.Doc == nil {
			genDecl.Doc = &ast.CommentGroup{}
		}

		for _, doc := range genDecl.Doc.List {
			if strings.HasPrefix(doc.Text, "// +kubebuilder:printcolumn:name") && strings.Contains(doc.Text, ".status.conditions") {
				doc.Text = "//" + strings.Repeat(tombstoneMarker, len(doc.Text)-2)
			}
		}

		return true
	}

	return false
}

func contextualizeFormatErrors(data []byte, err error) string {
	var serr scanner.ErrorList
	if errors.As(err, &serr) {
		errContext := []string{}
		lines := strings.Split(string(data), "\n")

		for i, err := range serr {
			line := err.Pos.Line

			lineContext := []string{"[ERROR " + strconv.Itoa(i+1) + "]:\n"}
			if line-2 >= 0 {
				lineContext = append(lineContext, lines[line-2])
			}
			lineContext = append(lineContext, lines[line-1])
			if line < len(lines) {
				lineContext = append(lineContext, lines[line])
			}
			errContext = append(errContext, strings.Join(lineContext, "\n"))
		}
		return strings.Join(errContext, "\n\n")
	}

	return ""
}
