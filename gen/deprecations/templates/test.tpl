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