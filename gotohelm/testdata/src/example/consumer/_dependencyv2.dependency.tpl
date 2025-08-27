{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/dependency/v2/dependency.go" */ -}}

{{- define "dependencyv2.RenderThing" -}}
{{- $name := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" $name))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

