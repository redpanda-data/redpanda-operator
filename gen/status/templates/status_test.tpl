// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ $.Package }}

// GENERATED from statuses.yaml, DO NOT EDIT DIRECTLY

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertConditionStatus(t *testing.T, name string, status metav1.ConditionStatus, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			assert.Equal(t, status, condition.Status)
			return
		}
	}

	t.Errorf("did not find condition with the name %q", name)
}

{{- range $status := $.Statuses }}

type set{{ $status.Kind }}Func func(status *{{ $status.Kind }}Status)

func Test{{ $status.Kind }}(t *testing.T) {
    {{- if $status.HasFinalConditions }}

	// final conditions tests
	for name, condition := range map[string]string{
	{{- range $finalCondition := $status.FinalConditions }}
		"{{ $finalCondition.Name }}": {{ $finalCondition.GoName }},
	{{- end }}
	} {
		condition := condition
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)

			// attempt to set all conditions one by one until they are all set
			{{ range $condition := $status.ManualConditions -}}			
			assertConditionStatus(t, condition, metav1.ConditionFalse, status.Conditions())

			status.Set{{ $condition.Name }}({{ (index $condition.Reasons 0).GoName }}, "reason")
			{{ end -}}
			assertConditionStatus(t, condition, metav1.ConditionTrue, status.Conditions())
		})
	}
	{{- end}}

    {{- if $status.HasTransientError }}

	// transient error tests
	for name, tt := range map[string]struct {
		setTransientErrFn set{{ $status.Kind }}Func
		setConditionReasons []set{{ $status.Kind }}Func
	}{
		{{- range $err := $status.TransientErrors }}{{- range $i, $condition := $status.TransientErrorConditions }}
		"Transient Error: {{ $err.Name }}, Condition: {{ $condition.Name }}": {
			setTransientErrFn: func(status *{{$status.Kind}}Status) { status.Set{{ $condition.Name }}({{ $condition.TransientErrorReasonNamed $err.Name }}, "reason") },
			setConditionReasons: []set{{ $status.Kind }}Func{
			{{- range $j, $otherCondition := $status.ManualConditions }}{{- if ne $i $j }}
				func(status *{{$status.Kind}}Status) { status.Set{{ $otherCondition.Name }}({{ (index $otherCondition.Reasons 0).GoName }}, "reason") },
			{{- end }}{{- end }}
			},
		},
		{{- end }}{{- end }}
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)
			
			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatus(t, {{ $condition.GoName }}, metav1.ConditionFalse, status.Conditions())
			{{- end }}

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatus(t, {{ $condition.GoName }}, metav1.ConditionFalse, status.Conditions())
			{{- end }}
		})
	}
	{{- end }}

	{{- if $status.HasTerminalError }}

	// terminal error tests
	for name, setFn := range map[string]set{{ $status.Kind }}Func {
		{{- range $err := $status.TerminalErrors }}{{- range $i, $condition := $status.TerminalErrorConditions }}
		"Terminal Error: {{ $err.Name }}, Condition: {{ $condition.Name }}": func(status *{{$status.Kind}}Status) { status.Set{{ $condition.Name }}({{ $condition.TerminalErrorReasonNamed $err.Name }}, "reason") },
		{{- end }}{{- end }}
	} {
		setFn := setFn
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)
			
			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatus(t, {{ $condition.GoName }}, metav1.ConditionFalse, status.Conditions())
			{{- end }}

			setFn(status)

			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatus(t, {{ $condition.GoName }}, metav1.ConditionTrue, status.Conditions())
			{{- end }}
		})
	}
	{{- end }}
}
{{- end }}