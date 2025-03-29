// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build generate
// +build generate

package main

/*
This file generates k8s object statuses to facillitate status construction
*/

import (
	"bytes"
	"os"
	"strings"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:generate sh -c "go run generator.go && go fmt zz_generated_status.go zz_generated_status_test.go"

type status struct {
	Kind        string
	Description string
	Types       []conditionType
}

func (s status) normalize() status {
	for i, conditionType := range s.Types {
		s.Types[i] = conditionType.normalize()
	}
	return s
}

type statusOverride struct {
	Unknown bool
}

type reasonType struct {
	Name        string
	Description string
	Message     string
	Status      statusOverride
	String      bool
}

type conditionType struct {
	Name        string
	Description string
	Ignore      bool
	Base        reasonType
	Errors      []reasonType
}

func (c conditionType) normalize() conditionType {
	if c.Base.Name == "" {
		c.Base.Name = c.Name
	}
	if c.Base.Message == "" {
		c.Base.Message = c.Base.Name
	}
	for i, err := range c.Errors {
		c.Errors[i] = err
	}
	return c
}

func mustDecodeYAML(name string, into interface{}) {
	file, err := os.OpenFile(name, os.O_RDONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		panic(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(file, int(stat.Size()))
	err = decoder.Decode(into)
	if err != nil {
		panic(err)
	}
}

func init() {
	mustDecodeYAML("statuses.yaml", &statuses)

	for i, status := range statuses {
		statuses[i] = status.normalize()
	}

	statusGenerator = template.Must(template.New("statuses").Funcs(template.FuncMap{
		"writeComment": writeComment,
		"year":         year,
	}).Parse(statusTemplate))
	statusTestGenerator = template.Must(template.New("statusTests").Funcs(template.FuncMap{
		"year": year,
	}).Parse(statusTestsTemplate))
}

var (
	statusGenerator     *template.Template
	statusTestGenerator *template.Template
	statuses            []status
)

const (
	statusTestsTemplate = `// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

// GENERATED from statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

{{ range $status := $ }}{{ range $conditionType := $status.Types -}}
func Test{{ $status.Kind }}{{ $conditionType.Name }}Status(t *testing.T) {
	t.Parallel()

	var status {{ $status.Kind }}{{ $conditionType.Name }}Status

	expected := errors.New("expected")

	status = {{ $status.Kind }}{{ $conditionType.Name }}Status{}
	assert.Equal(t, "{{ $conditionType.Base.Message }}", status.Condition(0).Message)
	assert.Equal(t, {{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $conditionType.Base.Name }}, status.Condition(0).Reason)
	{{ if not $conditionType.Ignore }}assert.False(t, status.HasError()){{ end }}

	{{ range $error := $conditionType.Errors }}
	status = {{ $status.Kind }}{{ $conditionType.Name }}Status{ {{ $error.Name }}: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, {{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $error.Name }}, status.Condition(0).Reason)
	{{ if not $conditionType.Ignore }}assert.True(t, status.HasError()){{ end }}
	{{ end }}
}

{{ end }}

func Test{{ $status.Kind }}Status(t *testing.T) {
	t.Parallel()

	status := {{ $status.Kind }}Status{}
	conditions := status.Conditions(0)

	var conditionType string
	var reason string

	{{ range $index, $conditionType := $status.Types }}
	conditionType = {{ $status.Kind }}{{ $conditionType.Name }}Condition
	reason = {{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $conditionType.Base.Name }} 
	assert.Equal(t, conditionType, conditions[{{ $index }}].Type)
	assert.Equal(t, reason, conditions[{{ $index }}].Reason)
	{{ end }}
}

{{ range $index, $conditionType := $status.Types }}
func Test{{ $status.Kind }}{{ $conditionType.Name }}StatusMarshaling(t *testing.T) {
	t.Parallel()

	status := {{ $status.Kind }}{{ $conditionType.Name }}Status{
		{{- range $error := $conditionType.Errors }}
		{{ $error.Name }}: {{ if $error.String }}"{{ $error.Name }}"{{ else }}errors.New("{{ $error.Name }}"){{ end }},{{ end }}
	}

	data, err := json.Marshal(&status)
	require.NoError(t, err)

	unmarshaled := {{ $status.Kind }}{{ $conditionType.Name }}Status{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))

	{{- range $error := $conditionType.Errors }}
	{{- if $error.String }}
	assert.Equal(t, status.{{ $error.Name }}, unmarshaled.{{ $error.Name }})
	{{ else }}
	assert.Equal(t, status.{{ $error.Name }}.Error(), unmarshaled.{{ $error.Name }}.Error())
	{{- end }}
	{{- end }}
}
{{ end }}

{{ end }}
`
	statusTemplate = `// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package status

// GENERATED from statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"encoding/json"
	"errors"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

{{ range $status := $ }}{{ range $conditionType := $status.Types -}}
{{- if (ne $conditionType.Description "") }}{{ writeComment (print $status.Kind $conditionType.Name "Status") $conditionType.Description }}{{ end }}
type {{ $status.Kind }}{{ $conditionType.Name }}Status struct {
	{{- range $error := $conditionType.Errors }}
	{{ if (ne $error.Description "") }}{{ writeComment "" $error.Description }}{{ end }}
	{{ $error.Name }} {{ if $error.String }}string{{ else }}error{{ end }}{{ end }}
}

const (
	{{ if (ne $conditionType.Description "") }}{{ writeComment (print $status.Kind $conditionType.Name "Condition") $conditionType.Description }}{{ end }}
	{{ $status.Kind }}{{ $conditionType.Name }}Condition = "{{ $conditionType.Name }}"
	{{ if (ne $conditionType.Base.Description "") }}{{ writeComment (print $status.Kind "ConditionReason" $conditionType.Base.Name) $conditionType.Base.Description }}{{ end }}
	{{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $conditionType.Base.Name }} = "{{ $conditionType.Base.Name }}"
	{{- range $error := $conditionType.Errors }}
	{{ if (ne $error.Description "") }}{{ writeComment (print $status.Kind $conditionType.Name "ConditionReason" $error.Name) $error.Description }}{{ end }}
	{{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $error.Name }} = "{{ $error.Name }}"{{ end }}
)

{{ writeComment "" (print "Condition returns the status condition of the " $status.Kind $conditionType.Name "Status based off of the underlying errors that are set.") }}
func (s {{ $status.Kind}}{{ $conditionType.Name }}Status) Condition(generation int64) meta.Condition {
	{{- range $error := $conditionType.Errors }}
	if s.{{ $error.Name }} != {{ if $error.String }}""{{ else }}nil{{ end }} {
		return meta.Condition{
			Type:               {{ $status.Kind }}{{ $conditionType.Name }}Condition,
			Status:             {{ if $error.Status.Unknown }}meta.ConditionUnknown{{ else }}meta.ConditionFalse{{ end }},
			Reason:             {{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $error.Name }},
			Message:            {{ if $error.String }}s.{{ $error.Name }}{{ else }}s.{{ $error.Name }}.Error(){{ end }},
			ObservedGeneration: generation,
			LastTransitionTime: meta.Now(),
		}
	}
	{{ end }}
	return meta.Condition{
		Type:               {{ $status.Kind }}{{ $conditionType.Name }}Condition,
		Status:             meta.ConditionTrue,
		Reason:             {{ $status.Kind }}{{ $conditionType.Name }}ConditionReason{{ $conditionType.Base.Name }},
		Message:            "{{ $conditionType.Base.Message }}",
		ObservedGeneration: generation,
		LastTransitionTime: meta.Now(),
	}
}

// MarshalJSON marshals a {{ $status.Kind}}{{ $conditionType.Name }}Status value to JSON
func (s {{ $status.Kind}}{{ $conditionType.Name }}Status) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	{{- range $error := $conditionType.Errors }}
	{{ if $error.String }}
	data["{{ $error.Name }}"] = s.{{ $error.Name }}
	{{ else }}
	if s.{{ $error.Name }} != nil {
		data["{{ $error.Name }}"] = s.{{ $error.Name }}.Error()
	}
	{{ end }}
	{{- end }}
	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a {{ $status.Kind}}{{ $conditionType.Name }}Status from JSON
func (s *{{ $status.Kind}}{{ $conditionType.Name }}Status) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	{{- range $error := $conditionType.Errors }}
	{{ if $error.String }}
	s.{{ $error.Name }} = data["{{ $error.Name }}"]
	{{ else }}
	if err, ok := data["{{ $error.Name }}"]; ok {
		s.{{ $error.Name }} = errors.New(err)
	}
	{{ end }}
	{{- end }}
	return nil
}

{{ if not $conditionType.Ignore -}}
{{ writeComment "" (print "HasError returns whether any of the " $status.Kind $conditionType.Name "Status errors are set.") }}
func (s {{ $status.Kind}}{{ $conditionType.Name }}Status) HasError() bool {
	return {{ range $index, $error := $conditionType.Errors }}{{ if (ne $index 0) }} || {{ end }}s.{{$error.Name}} != {{ if $error.String }}""{{ else }}nil{{ end }}{{ end }}
}
{{ end }}
{{ end }}
{{- if (ne $status.Description "") }}{{ writeComment (print $status.Kind "Status") $status.Description }}{{ end }}
type {{ $status.Kind}}Status struct {
	{{- range $conditionType := $status.Types }}
	{{ if (ne $conditionType.Description "") }}{{ writeComment "" $conditionType.Description }}{{ end }}
	{{ $conditionType.Name }} {{ $status.Kind}}{{ $conditionType.Name }}Status{{ end }}
}

{{ writeComment "" (print "Conditions returns the aggregated status conditions of the " $status.Kind "Status.") }}
func (s {{ $status.Kind}}Status) Conditions(generation int64) []meta.Condition {
	return []meta.Condition{
		{{- range $conditionType := $status.Types }}
		s.{{ $conditionType.Name }}.Condition(generation),{{ end }}
	}
}

{{ end }}
`
)

const (
	lineLength = 77
)

func wrapLine(line string) []string {
	if len(line) <= lineLength {
		return []string{line}
	}
	tokens := strings.Split(line, " ")
	lines := []string{}
	currentLine := ""
	for _, token := range tokens {
		appendedLength := len(token)
		if currentLine != "" {
			appendedLength++
		}
		newLength := appendedLength + len(currentLine)
		if newLength > lineLength {
			lines = append(lines, currentLine)
			currentLine = ""
		}
		if currentLine == "" {
			currentLine = token
			continue
		}
		currentLine = currentLine + " " + token
	}
	return append(lines, currentLine)
}

func year() string {
	return time.Now().Format("2006")
}

func writeComment(name, comment string) string {
	comment = strings.TrimSpace(comment)
	lines := strings.Split(comment, "\n")
	wrappedLines := []string{}
	for i, line := range lines {
		if i == 0 && name != "" {
			line = name + " - " + line
		}
		if i != 0 {
			wrappedLines = append(wrappedLines, "")
		}
		wrappedLines = append(wrappedLines, wrapLine(line)...)
	}
	for i, line := range wrappedLines {
		wrappedLines[i] = "// " + line
	}
	return strings.Join(wrappedLines, "\n")
}

func main() {
	var buffer bytes.Buffer
	if err := statusGenerator.Execute(&buffer, statuses); err != nil {
		panic(err)
	}
	if err := os.WriteFile("zz_generated_status.go", buffer.Bytes(), 0644); err != nil {
		panic(err)
	}

	buffer.Reset()

	if err := statusTestGenerator.Execute(&buffer, statuses); err != nil {
		panic(err)
	}
	if err := os.WriteFile("zz_generated_status_test.go", buffer.Bytes(), 0644); err != nil {
		panic(err)
	}
}
