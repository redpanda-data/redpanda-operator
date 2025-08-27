{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/consumer/consumer.go" */ -}}

{{- define "consumer.Render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "dependencyv2.RenderThing" (dict "a" (list "eat your heart out")))) "r") (get (fromJson (include "dependencyv3.RenderThing" (dict "a" (list "subcharts")))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

