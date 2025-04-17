// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

import (
	"bytes"
	_ "embed"
	"go/format"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	//go:embed templates/state.tpl
	statusStateTemplate string
	//go:embed templates/status_test.tpl
	statusTestsTemplate string
	//go:embed templates/status.tpl
	statusTemplate       string
	statusGenerator      *template.Template
	statusTestGenerator  *template.Template
	statusStateGenerator *template.Template

	helpers = map[string]any{
		"year": func() string {
			return time.Now().Format("2006")
		},
		"title": func(s string) string {
			return cases.Title(language.English).String(s)
		},
	}
)

func init() {
	statusGenerator = template.Must(template.New("statuses").Funcs(helpers).Parse(statusTemplate))
	statusStateGenerator = template.Must(template.New("statusState").Funcs(helpers).Parse(statusStateTemplate))
	statusTestGenerator = template.Must(template.New("statusTests").Funcs(helpers).Parse(statusTestsTemplate))
}

type StatusConfig struct {
	StatusesFile string
	Package      string
	Outputs      StatusConfigOutputs
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
		status.normalize()
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
	if err := renderAndWrite(outputs.Directory, outputs.StateFile, info, renderStateFile); err != nil {
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

func renderStateFile(info *templateInfo) ([]byte, error) {
	var buffer bytes.Buffer
	if err := statusStateGenerator.Execute(&buffer, info); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
