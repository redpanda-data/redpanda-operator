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
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

{{ range $status := $.Statuses -}}
{{- range $conditionType := $status.Types }}

func Test{{ $conditionType.GoStructName }}(t *testing.T) {
	t.Parallel()
	expected := errors.New("expected")

	status := &{{ $conditionType.GoStructName }}{}
	assert.Equal(t, "{{ $conditionType.Base.Message }}", status.Condition(0).Message)
	assert.Equal(t, {{ $conditionType.Base.GoName }}, status.Condition(0).Reason)
	{{ if not $conditionType.Ignore }}assert.False(t, status.HasError()){{ end }}
	{{- range $reason := $conditionType.Reasons }}

	status = &{{ $conditionType.GoStructName }}{ {{ $reason.Name }}: expected}
	assert.Equal(t, "expected", status.Condition(0).Message)
	assert.Equal(t, {{ $reason.GoName }}, status.Condition(0).Reason)
	{{ if not $conditionType.Ignore }}assert.True(t, status.HasError()){{ end }}
	{{- end }}
}
{{- end }}

func Test{{ $status.GoName }}(t *testing.T) {
	t.Parallel()
	status := New{{ $status.Kind }}()
	conditions := status.Conditions(0)
	var conditionType string
	var reason string
	{{- range $index, $conditionType := $status.Types }}

	conditionType = {{ $conditionType.GoConditionName }}
	reason = {{ $conditionType.Base.GoName }} 
	assert.Equal(t, conditionType, conditions[{{ $index }}].Type)
	assert.Equal(t, reason, conditions[{{ $index }}].Reason)
	{{- end }}
}

{{- range $conditionType := $status.Types }}

func Test{{ $conditionType.GoStructName }}Marshaling(t *testing.T) {
	t.Parallel()
	status := &{{ $conditionType.GoStructName }}{
		{{- range $reason := $conditionType.Reasons }}
		{{ $reason.Name }}: errors.New("{{ $reason.Name }}"),
        {{- end }}
	}
	data, err := json.Marshal(status)
	require.NoError(t, err)
	unmarshaled := &{{ $conditionType.GoStructName }}{}
	require.NoError(t, json.Unmarshal(data, &unmarshaled))
	{{- range $reason := $conditionType.Reasons }}
	assert.Equal(t, status.{{ $reason.Name }}.Error(), unmarshaled.{{ $reason.Name }}.Error())
	{{- end }}
}
{{- end }}
{{ end }}