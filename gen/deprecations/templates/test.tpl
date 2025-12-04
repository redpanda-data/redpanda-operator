// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ .Pkg }}

{{ .TODOComment }}

import (
	"testing"

  "github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/deprecations"
)

func TestDeprecatedFieldWarnings(t *testing.T) {
	tests := []struct {
		name string
		obj client.Object
		wantWarnings []string
	}{
	{{- range .Objs }}
		{
			name: "{{ .Name }}",
			obj: {{ .Literal }},
			wantWarnings: []string{
		{{- range .Warnings }}
				"{{ . }}",
		{{- end }}
			},
		},
	{{- end }}
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			warnings, err := deprecations.FindDeprecatedFieldWarnings(tc.obj)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.wantWarnings, warnings)
		})
	}
}