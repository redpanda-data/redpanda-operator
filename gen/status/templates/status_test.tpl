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

func assertConditionStatusReason(t *testing.T, name string, status metav1.ConditionStatus, reason string, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			assert.Equal(t, status, condition.Status, "%s should be %v but was %v", name, status, condition.Status)
			assert.Equal(t, reason, condition.Reason, "%s should have reason %v but was %v", name, reason, condition.Reason)
			return
		}
	}

	t.Errorf("did not find condition with the name %q", name)
}

func assertNoCondition(t *testing.T, name string, conditions []metav1.Condition) {
	t.Helper()

	for _, condition := range conditions {
		if condition.Type == name {
			t.Errorf("found condition %q with reason %q and status %v when there should be none", name, condition.Reason, condition.Status)
			return
		}
	}
}

{{- range $status := $.Statuses }}

type set{{ $status.Kind }}Func func(status *{{ $status.Kind }}Status)

func Test{{ $status.Kind }}(t *testing.T) {
	// regular condition tests
	for name, tt := range map[string]struct{
		condition string
		reason string
		expected metav1.ConditionStatus
		setFn set{{ $status.Kind }}Func
	}{
	{{- range $condition := $status.ManualConditions }}
	{{- range $i, $reason := $condition.Reasons }}
		"{{ $condition.Name }}/{{ $reason.Name }}": {
			condition: {{ $condition.GoName }},
			reason: string({{ $reason.GoName }}),
			expected: metav1.Condition{{ if eq $i 0 }}True{{ else }}False{{ end }},
			setFn: func(status *{{ $status.Kind }}Status) { status.Set{{ $condition.Name }}({{$reason.GoName}}, "reason") },
		},
	{{- end }}
	{{- end }}
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)

			assertNoCondition(t, tt.condition, status.Conditions())
			tt.setFn(status)
			assertConditionStatusReason(t, tt.condition, tt.expected, tt.reason, status.Conditions())
		})
	}

    {{- if $status.HasFinalConditions }}

	// final conditions tests
	for name, conditionReason := range map[string]struct{
		condition string
		trueReason string
		falseReason string
	}{
	{{- range $finalCondition := $status.FinalConditions }}
		"{{ $finalCondition.Name }}": {
			condition: {{ $finalCondition.GoName }},
			trueReason: string({{ (index $finalCondition.Reasons 0).GoName }}),
			falseReason: string({{ (index $finalCondition.Reasons 1).GoName }}),
		},
	{{- end }}
	} {
		conditionReason := conditionReason
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)

			// attempt to set all conditions one by one until they are all set
			{{ range $condition := $status.ManualConditions -}}			
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionFalse, conditionReason.falseReason, status.Conditions())

			status.Set{{ $condition.Name }}({{ (index $condition.Reasons 0).GoName }}, "reason")
			{{ end -}}
			assertConditionStatusReason(t, conditionReason.condition, metav1.ConditionTrue, conditionReason.trueReason, status.Conditions())
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
			assertConditionStatusReason(t, {{ $condition.GoName }}, metav1.ConditionFalse, string({{ (index $condition.Reasons 1).GoName }}), status.Conditions())
			{{- end }}

			tt.setTransientErrFn(status)
			for _, setFn := range tt.setConditionReasons {
				setFn(status)
			}

			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatusReason(t, {{ $condition.GoName }}, metav1.ConditionFalse, string({{ (index $condition.Reasons 1).GoName }}), status.Conditions())
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
			assertConditionStatusReason(t, {{ $condition.GoName }}, metav1.ConditionFalse, string({{ (index $condition.Reasons 1).GoName }}), status.Conditions())
			{{- end }}

			setFn(status)

			{{ range $condition := $status.FinalConditions -}}
			assertConditionStatusReason(t, {{ $condition.GoName }}, metav1.ConditionTrue, string({{ (index $condition.Reasons 0).GoName }}), status.Conditions())
			{{- end }}
		})
	}
	{{- end }}

	{{- if $status.HasRollupConditions }}

	// rollup conditions tests
	for name, tt := range map[string]struct {
		condition string
		trueReason string
		falseReason string
		falseCondition set{{ $status.Kind }}Func
		trueConditions []set{{ $status.Kind }}Func
	} {
		{{- range $rollup := $status.RollupConditions }}
		"Rollup Conditions: {{ $rollup.Name }}, All True": {
			condition: {{ $rollup.GoName }},
			trueReason: string({{ (index $rollup.Reasons 0).GoName }}),
			falseReason: string({{ (index $rollup.Reasons 1).GoName }}),
			trueConditions: []set{{ $status.Kind }}Func{
				{{- range $condition := $status.ManualConditions }}
				func(status *{{$status.Kind}}Status) { status.Set{{ $condition.Name }}({{ (index $condition.Reasons 0).GoName }}, "reason") },
				{{- end }}
			},
		},
		{{- range $i, $condition := $rollup.RollupConditions }}{{- if not $condition.IsCalculated }}
		"Rollup Conditions: {{ $rollup.Name }}, False Condition: {{ $condition.Name }}": {
			condition: {{ $rollup.GoName }},
			trueReason: string({{ (index $rollup.Reasons 0).GoName }}),
			falseReason: string({{ (index $rollup.Reasons 1).GoName }}),
			falseCondition: func(status *{{$status.Kind}}Status) { status.Set{{ $condition.Name }}({{ $condition.TerminalErrorReasonNamed "TerminalError" }}, "reason") },
			trueConditions: []set{{ $status.Kind }}Func{
				{{- range $otherCondition := $status.ManualConditions }}{{- if (ne $otherCondition.Name $condition.Name) }}
				func(status *{{$status.Kind}}Status) { status.Set{{ $otherCondition.Name }}({{ (index $otherCondition.Reasons 0).GoName }}, "reason") },
				{{- end }}{{- end }}
			},
		},
		{{- end }}{{- end }}{{- end }}
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := New{{ $status.Kind }}(0)
			
			assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.Conditions())

			if tt.falseCondition != nil {
				tt.falseCondition(status)
			}
			for _, setFn := range tt.trueConditions {
				setFn(status)
			}

			if tt.falseCondition != nil {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionFalse, tt.falseReason, status.Conditions())
			} else {
				assertConditionStatusReason(t, tt.condition, metav1.ConditionTrue, tt.trueReason, status.Conditions())
			}
		})
	}
	{{- end }}
}
{{- end }}