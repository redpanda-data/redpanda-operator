{{- define "type" -}}
{{- $type := . -}}
{{- if and (asciidocShouldRenderType $type) (not $type.Markers.hidefromdoc) -}}

[id="{{ asciidocTypeID $type | asciidocRenderAnchorID }}"]
==== {{ $type.Name  }}

{{ if $type.IsAlias }}_Underlying type:_ _{{ asciidocRenderTypeLink $type.UnderlyingType  }}_{{ end }}

{{ $type.Doc }}

{{ if $type.Validation -}}
.Validation:
{{- range $type.Validation }}
- {{ . }}
{{- end }}
{{- end }}

{{- $references := list -}}
{{- range $type.SortedReferences -}}
{{- if (not .Markers.hidefromdoc) -}}
{{- $references = (append $references .) -}}
{{- end -}}
{{- end }}

{{ if $references -}}
.Appears In:
****
{{- range $references }}
- {{ asciidocRenderTypeLink . }}
{{- end }}
****
{{- end }}

{{ if $type.Members -}}
[cols="20a,50a,15a,15a", options="header"]
|===
| Field | Description | Default | Validation
{{ if $type.GVK -}}
| *`apiVersion`* __string__ | `{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` | |
| *`kind`* __string__ | `{{ $type.GVK.Kind }}` | |
{{ end -}}

{{ range $type.Members -}}
{{ with .Markers.hidefromdoc -}}
{{ else -}}
| *`{{ .Name  }}`* __{{ asciidocRenderType .Type }}__ | {{ template "type_members" . }} | {{ .Default }} | {{ range .Validation -}} {{ asciidocRenderValidation . }} +
{{ end -}}
{{ end }}
{{ end -}}
|===
{{ end -}}

{{- end -}}
{{- end -}}
