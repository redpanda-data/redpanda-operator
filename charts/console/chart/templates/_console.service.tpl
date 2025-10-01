{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/service.go" */ -}}

{{- define "console.Service" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $port := (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "http" "port" (($state.Values.service.port | int) | int) "protocol" "TCP")) -}}
{{- if (ne (toJson $state.Values.service.targetPort) "null") -}}
{{- $_ := (set $port "targetPort" $state.Values.service.targetPort) -}}
{{- end -}}
{{- if (and (contains "NodePort" (toString $state.Values.service.type)) (ne (toJson $state.Values.service.nodePort) "null")) -}}
{{- $_ := (set $port "nodePort" $state.Values.service.nodePort) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "annotations" $state.Values.service.annotations)) "spec" (mustMergeOverwrite (dict) (dict "type" $state.Values.service.type "selector" (get (fromJson (include "console.RenderState.SelectorLabels" (dict "a" (list $state)))) "r") "ports" (list $port)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

