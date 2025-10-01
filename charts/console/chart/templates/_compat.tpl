{{- define "console.Name" -}}
{{- (dict "r" (get (fromJson (include "chart.Name" .)) "r")) | toJson -}}
{{- end -}}

{{- define "console.Fullname" -}}
{{- (dict "r" (get (fromJson (include "chart.Fullname" .)) "r")) | toJson -}}
{{- end -}}

