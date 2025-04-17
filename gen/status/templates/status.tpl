// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ $.Package }}

// GENERATED from {{ $.File }}, DO NOT EDIT DIRECTLY

import (
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

{{ range $status := $.Statuses -}}

{{ $status.Comment }}
type {{ $status.GoName }} struct {
	{{- range $conditionType := $status.Types }}
	{{ $conditionType.StructComment }}
	{{ $conditionType.Name }} *{{ $conditionType.GoStructName }}{{ end }}
}

// New{{ $status.Kind }}() returns a new {{ $status.GoName }}
func New{{ $status.Kind }}() *{{ $status.GoName }} {
	return &{{ $status.GoName }}{
    {{- range $conditionType := $status.Types }}
	    &{{ $conditionType.GoStructName }}{},
    {{- end }}
    }
}

// Conditions returns the aggregated status conditions of the {{ $status.GoName }}.
func (s *{{ $status.GoName }}) Conditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{{- range $conditionType := $status.Types }}
		s.{{ $conditionType.Name }}.Condition(generation),{{ end }}
	}
}
{{- range $conditionType := $status.Types }}{{/* Conditions */}}

{{ $conditionType.StructComment }}
type {{ $conditionType.GoStructName }} struct {
	{{- range $reason := $conditionType.Reasons }}
	{{ $reason.Comment }}
	{{ $reason.Name }} error
    {{- end }}
}

const (
    {{ $conditionType.ConditionComment }}
	{{ $conditionType.GoConditionName }} = "{{ $conditionType.Name }}"
	{{ $conditionType.Base.Comment }}
	{{ $conditionType.Base.GoName }} = "{{ $conditionType.Base.Name }}"
	{{- range $reason := $conditionType.Reasons }}
	{{ $reason.Comment }}
	{{ $reason.GoName }} = "{{ $reason.Name }}"
    {{- end }}
)

{{ $conditionType.ConditionFuncComment }}
func (s *{{ $conditionType.GoStructName }}) Condition(generation int64) metav1.Condition {
	{{- range $reason := $conditionType.Reasons }}
	if s.{{ $reason.Name }} != nil {
		return metav1.Condition{
			Type:               {{ $conditionType.GoConditionName }},
			Status:             metav1.Condition{{ $reason.DefaultValue }},
			Reason:             {{ $reason.GoName}},
			Message:            s.{{ $reason.Name }}.Error(),
			ObservedGeneration: generation,
			LastTransitionTime: metav1.Now(),
		}
	}
	{{- end }}

	return metav1.Condition{
		Type:               {{ $conditionType.GoConditionName }},
		Status:             metav1.ConditionTrue,
		Reason:             {{ $conditionType.Base.GoName }},
		Message:            "{{ $conditionType.Base.Message }}",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}

// MarshalJSON marshals a {{ $conditionType.GoStructName }} value to JSON
func (s *{{ $conditionType.GoStructName }}) MarshalJSON() ([]byte, error) {
	data := map[string]string{}
	{{- range $reason := $conditionType.Reasons }}
	if s.{{ $reason.Name }} != nil {
		data["{{ $reason.Name }}"] = s.{{ $reason.Name }}.Error()
	}
	{{- end }}

	return json.Marshal(data)
}

// UnmarshalJSON unmarshals a {{ $conditionType.GoStructName }} from JSON
func (s *{{ $conditionType.GoStructName }}) UnmarshalJSON(b []byte) error {
	data := map[string]string{}
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	{{- range $reason := $conditionType.Reasons }}
	if err, ok := data["{{ $reason.Name }}"]; ok {
		s.{{ $reason.Name }} = errors.New(err)
	}
	{{- end }}

	return nil
}

{{ if not $conditionType.Ignore -}}
// HasError returns whether any of the underlying errors for the given condition are set.
func (s *{{ $conditionType.GoStructName }}) HasError() bool {
	return {{ range $index, $reason := $conditionType.Reasons }}{{ if (ne $index 0) }} || {{ end }}s.{{$reason.Name}} != nil{{ end }}
}

// MaybeError returns an underlying error for the given condition if one has been set.
func (s *{{ $conditionType.GoStructName }}) MaybeError() error {
	{{- range $index, $reason := $conditionType.Reasons }}
	if s.{{$reason.Name}} != nil {
		return s.{{$reason.Name}}
	}
	{{- end }}

	return nil
}
{{- end }}
{{- end }}{{/* END Conditions */}}
{{ end }}