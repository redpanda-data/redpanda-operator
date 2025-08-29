{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/chart/chart.go" */ -}}

{{- define "chart.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.Render" (dict "a" (list $dot)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

