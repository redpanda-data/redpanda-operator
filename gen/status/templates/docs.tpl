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

{{ range $status := $.Statuses -}}{{/* Type Declarations */}}
{{- range $condition := $status.Conditions }}
{{ $condition.ConditionComment }}
// +statusType
type {{ $condition.GoConditionName }} string
{{- end }}
{{- end }}{{/* Type Declarations */}}

const (
{{- range $status := $.Statuses -}}
{{- range $i, $condition := $status.Conditions }}
    {{- if ne $i 0}}
    {{/* break */}}
    {{- end }}
    {{ $condition.NamelessComment }}
	{{ $condition.GoName }} = "{{ $condition.Name }}"
	{{- range $reason := $condition.Reasons }}
    {{ $reason.NamelessComment }}
	{{ $reason.GoName }} {{ $condition.GoConditionName }} = "{{ $reason.Name }}"
    {{- end }}
{{- end }}
{{- end }}
)
