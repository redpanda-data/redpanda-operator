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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

{{ range $status := $.Statuses -}}{{/* Type Declarations */}}
{{- range $condition := $status.Conditions }}
{{ $condition.ConditionComment }}
type {{ $condition.GoConditionName }} string
{{- end }}
{{- end }}{{/* Type Declarations */}}

const (
{{- range $status := $.Statuses -}}
{{- range $i, $condition := $status.Conditions }}
    {{- if ne $i 0}}
    {{/* break */}}
    {{- end }}
    {{ $condition.Comment }}
	{{ $condition.GoName }} = "{{ $condition.Name }}"
	{{- range $reason := $condition.Reasons }}
    {{ $reason.Comment }}
	{{ $reason.GoName }} {{ $condition.GoConditionName }} = "{{ $reason.Name }}"
    {{- end }}
{{- end }}
{{- end }}
)

{{ range $status := $.Statuses -}}{{/* Status Structure */}}
{{ $status.Comment }}
type {{ $status.GoName }} struct {
    generation int64
    conditions []metav1.Condition
    hasTerminalError bool
	{{- range $condition := $status.ManualConditions }}
	is{{ $condition.Name }}Set bool
    {{- if $condition.HasTransientError }}
	is{{ $condition.Name }}TransientError bool
    {{- end }}
    {{- end }}
}

// New{{ $status.Kind }}() returns a new {{ $status.GoName }}
func New{{ $status.Kind }}(generation int64) *{{ $status.GoName }} {
	return &{{ $status.GoName }}{
        generation: generation,
    }
}

// Conditions returns the aggregated status conditions of the {{ $status.GoName }}.
func (s *{{ $status.GoName }}) Conditions() []metav1.Condition {
    conditions := append([]metav1.Condition{}, s.conditions...)

    {{- range $condition:= $status.FinalConditions }}
    conditions = append(conditions, s.get{{ $condition.Name }}())
    {{- end }}

    {{- range $condition:= $status.RollupConditions }}
    conditions = append(conditions, s.get{{ $condition.Name }}(conditions))
    {{- end }}

	return conditions
}

{{- range $condition := $status.ManualConditions }}{{/* ManualConditions */}}
// Set{{ $condition.Name }} sets the underlying condition to the given reason.
func (s *{{ $status.GoName }}) Set{{ $condition.Name }}(reason {{ $condition.GoConditionName }}, messages ...string) {
    if s.is{{ $condition.Name }}Set {
        panic("you should only ever set a condition once, doing so more than once is a programming error")
    }

    var status metav1.ConditionStatus

    s.is{{ $condition.Name }}Set = true
    message := strings.Join(messages, "; ")

    switch reason {
    {{- range $i, $reason := $condition.Reasons }}
	    case {{ $reason.GoName }}:
        {{- if $reason.IsTransientError }}
            s.is{{ $condition.Name }}TransientError = true
        {{- end }}
        {{- if $reason.IsTerminalError }}
            s.hasTerminalError = true
        {{- end }}
        {{- if ne $reason.Message "" }}
            if message == "" {
                message = "{{ $reason.Message }}"
            }
        {{- end }}
            status = metav1.Condition{{ if eq $i 0 }}True{{ else }}False{{ end }}
    {{- end }}
        default:
            panic("unhandled reason type")
    }

    if message == "" {
        panic("message must be set")
    }

    s.conditions = append(s.conditions, metav1.Condition{
        Type:               {{ $condition.GoName }},
        Status:             status,
        Reason:             string(reason),
        Message:            message,
        ObservedGeneration: s.generation,
    })
}
{{- end }}{{/* Manual Conditions */}}

{{- range $condition := $status.FinalConditions }}{{/* FinalConditions */}}

func (s *{{ $status.GoName }}) get{{ $condition.Name }}() metav1.Condition {
    {{- if $status.HasTransientError }}
    transientErrorConditionsSet := {{ range $i, $condition := $status.TransientErrorConditions }}{{ if ne $i 0 }}|| {{ end }}s.is{{ $condition.Name }}TransientError{{ end }}
    {{- end }}
    allConditionsSet := {{ range $i, $condition := $status.ManualConditions }}{{ if ne $i 0 }}&& {{ end }}s.is{{ $condition.Name }}Set{{ end }}

    if (allConditionsSet || s.hasTerminalError) {{ if $status.HasTransientError }}&& !transientErrorConditionsSet {{end}}{
        return metav1.Condition{
            Type:               {{ $condition.GoName }},
            Status:             metav1.ConditionTrue,
            Reason:             string({{ (index $condition.Reasons 0).GoName }}),
            Message:            "{{ (index $condition.Reasons 0).Message }}",
            ObservedGeneration: s.generation,
        }
    }

    return metav1.Condition{
        Type:               {{ $condition.GoName }},
        Status:             metav1.ConditionFalse,
        Reason:             string({{ (index $condition.Reasons 1).GoName }}),
        Message:            "{{ (index $condition.Reasons 1).Message }}",
        ObservedGeneration: s.generation,
    }
}
{{- end }}{{/* Final Conditions */}}

{{- range $condition := $status.RollupConditions }}{{/* RollupConditions */}}

func (s *{{ $status.GoName }}) get{{ $condition.Name }}(conditions []metav1.Condition) metav1.Condition {
    allConditionsFoundAndTrue := true
    for _, condition := range []string{ {{ range $i, $condition := $condition.RollupConditions }}{{ if ne $i 0 }}, {{ end }}{{ $condition.GoName }}{{ end }} } {
        conditionFoundAndTrue := false
        for _, setCondition := range conditions {
            if setCondition.Type == condition {
                conditionFoundAndTrue = setCondition.Status == metav1.ConditionTrue 
                break
            }
        }
        if !conditionFoundAndTrue {
            allConditionsFoundAndTrue = false
            break
        }
    }

    if allConditionsFoundAndTrue {
        return metav1.Condition{
            Type:               {{ $condition.GoName }},
            Status:             metav1.ConditionTrue,
            Reason:             string({{ (index $condition.Reasons 0).GoName }}),
            Message:            "{{ (index $condition.Reasons 0).Message }}",
            ObservedGeneration: s.generation,
        }
    }

    return metav1.Condition{
        Type:               {{ $condition.GoName }},
        Status:             metav1.ConditionFalse,
        Reason:             string({{ (index $condition.Reasons 1).GoName }}),
        Message:            "{{ (index $condition.Reasons 1).Message }}",
        ObservedGeneration: s.generation,
    }
}
{{- end }}{{/* Rollup Conditions */}}
{{ end }}{{/* Status Structure */}}