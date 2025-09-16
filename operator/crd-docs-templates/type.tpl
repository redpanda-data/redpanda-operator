{{- define "type" -}}
{{- $type := . -}}
{{- if (or (asciidocShouldRenderType $type) ($type.Markers.statusType)) }}

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

{{ if (and $type.EnumValues $type.Markers.statusType) -}} 
[cols="20a,80a", options="header"]
|===
| Reason | Description |
{{ range $type.EnumValues -}}
| `{{ .Name }}` | {{ asciidocRenderFieldDoc .Doc }} |
{{ end }}
{{- end }}

{{ if $type.References -}}
.Appears In:
****
{{- range $type.SortedReferences }}
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
