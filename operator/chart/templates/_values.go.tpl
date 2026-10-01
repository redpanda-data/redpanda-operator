{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/values.go" */ -}}

{{- define "operator.PodTemplateSpec.asPodTemplateSpec" -}}
{{- $p := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $p) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "labels" (default (mustMergeOverwrite (dict) (dict)) $p.metadata).labels "annotations" (default (mustMergeOverwrite (dict) (dict)) $p.metadata).annotations)) "spec" (default (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict)) $p.spec)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

