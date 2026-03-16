{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/dependency/v3/dependency.go" */ -}}

{{- define "dependencyv3.RenderThing" -}}
{{- $name := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict)) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $name))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

