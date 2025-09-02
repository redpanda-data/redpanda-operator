{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/configmap.go" */ -}}

{{- define "console.ConfigMap" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.configmap.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $data := (dict "config.yaml" (printf "# from .Values.config\n%s\n" (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list (toYaml $state.Values.config)))))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "ConfigMap")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace)) "data" $data))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

