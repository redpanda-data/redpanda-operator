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
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
    {{- range $i, $pkg := (mergeImports $.Statuses) }}
    {{- if eq $i 0 }}
    {{/* br */}}
    {{- end }}
    {{ $pkg }}
    {{- end }}
    "github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
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
func New{{ $status.Kind }}() *{{ $status.GoName }} {
	return &{{ $status.GoName }}{}
}

// UpdateConditions updates any conditions for the passed in object that need to be updated.
func (s *{{ $status.GoName }}) UpdateConditions(o client.Object) bool {
    var conditions *[]metav1.Condition
    switch kind := o.(type) {
        {{- range $kind := $status.ImportedKinds }}
    case {{ $kind }}:
        conditions = &kind.Status.Conditions
        {{- end }}
    default:
        panic("unsupported kind")
    }

    updated := false
    for _, condition := range s.getConditions(o.GetGeneration()) {
        if setStatusCondition(conditions, condition) {
            updated = true
        }
    }

    return updated
}

// StatusConditionConfigs returns a set of configurations that can be used with Server Side Apply.
func (s *{{ $status.GoName }}) StatusConditionConfigs(o client.Object) []*applymetav1.ConditionApplyConfiguration {
    var conditions []metav1.Condition
    switch kind := o.(type) {
        {{- range $kind := $status.ImportedKinds }}
    case {{ $kind }}:
        conditions = kind.Status.Conditions
        {{- end }}
    default:
        panic("unsupported kind")
    }

    return utils.StatusConditionConfigs(conditions, o.GetGeneration(), s.getConditions(o.GetGeneration()))
}

// conditions returns the aggregated status conditions of the {{ $status.GoName }}.
func (s *{{ $status.GoName }}) getConditions(generation int64) []metav1.Condition {
    conditions := append([]metav1.Condition{}, s.conditions...)

    {{- range $condition:= $status.FinalConditions }}
    conditions = append(conditions, s.get{{ $condition.Name }}())
    {{- end }}

    {{- range $condition:= $status.RollupConditions }}
    conditions = append(conditions, s.get{{ $condition.Name }}(conditions))
    {{- end }}

    for i, condition := range conditions {
        condition.ObservedGeneration = generation
        conditions[i] = condition
    }

	return conditions
}

{{- range $condition := $status.ManualConditions }}{{/* ManualConditions */}}
// Set{{ $condition.Name }}FromCurrent sets the underlying condition based on an existing object.
func (s *{{ $status.GoName }}) Set{{ $condition.Name }}FromCurrent(o client.Object) {
    condition := apimeta.FindStatusCondition(GetConditions(o), {{ $condition.GoName }})
    if condition == nil {
        return
    }

    s.Set{{ $condition.Name }}({{ $condition.GoConditionName }}(condition.Reason), condition.Message)
}

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
        }
    }

    return metav1.Condition{
        Type:               {{ $condition.GoName }},
        Status:             metav1.ConditionFalse,
        Reason:             string({{ (index $condition.Reasons 1).GoName }}),
        Message:            "{{ (index $condition.Reasons 1).Message }}",
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
        }
    }

    return metav1.Condition{
        Type:               {{ $condition.GoName }},
        Status:             metav1.ConditionFalse,
        Reason:             string({{ (index $condition.Reasons 1).GoName }}),
        Message:            "{{ (index $condition.Reasons 1).Message }}",
    }
}
{{- end }}{{/* Rollup Conditions */}}
{{ end }}{{/* Status Structure */}}

// HasRecentCondition returns whether or not an object has a given condition with the given value that is up-to-date and set
// within the given time period.
func HasRecentCondition[T ~string](o client.Object, conditionType T, value metav1.ConditionStatus, period time.Duration) bool {
    condition := apimeta.FindStatusCondition(GetConditions(o), string(conditionType))
    if condition == nil {
        return false
    }

	recent := time.Since(condition.LastTransitionTime.Time) > period
    matchedCondition := condition.Status == value
    generationChanged := condition.ObservedGeneration != 0 && condition.ObservedGeneration < o.GetGeneration()

    return matchedCondition && !(generationChanged || recent)
}

// GetConditions returns the conditions for a given object.
func GetConditions(o client.Object) []metav1.Condition {
    switch kind := o.(type) {
        {{- range $status := $.Statuses -}}
        {{- range $kind := $status.ImportedKinds }}
    case {{ $kind }}:
        return kind.Status.Conditions
        {{- end }}
        {{- end }}
    default:
        panic("unsupported kind")
    }
}

// setStatusCondition is a copy of the apimeta.SetStatusCondition with one primary change. Rather
// than only change the .LastTransitionTime if the .Status field of the condition changes, it
// sets it if .Status, .Reason, .Message, or .ObservedGeneration changes, which works nicely with our recent check leveraged
// for rate limiting above. It also normalizes this to be the same as what utils.StatusConditionConfigs does
func setStatusCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) (changed bool) {
	if conditions == nil {
		return false
	}
	existingCondition := apimeta.FindStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		if newCondition.LastTransitionTime.IsZero() {
			newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
		*conditions = append(*conditions, newCondition)
		return true
	}

    setTransitionTime := func() {
		if !newCondition.LastTransitionTime.IsZero() {
			existingCondition.LastTransitionTime = newCondition.LastTransitionTime
		} else {
			existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		}
    }
    
	if existingCondition.Status != newCondition.Status {
		existingCondition.Status = newCondition.Status
		setTransitionTime()
		changed = true
	}

	if existingCondition.Reason != newCondition.Reason {
		existingCondition.Reason = newCondition.Reason
		setTransitionTime()
		changed = true
	}
	if existingCondition.Message != newCondition.Message {
		existingCondition.Message = newCondition.Message
		setTransitionTime()
		changed = true
	}
	if existingCondition.ObservedGeneration != newCondition.ObservedGeneration {
		existingCondition.ObservedGeneration = newCondition.ObservedGeneration
		setTransitionTime()
		changed = true
	}

	return changed
}
