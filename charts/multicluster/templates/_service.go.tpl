{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/multicluster/v25/service.go" */ -}}

{{- define "multicluster.ServiceName" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.Service" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "kind" "Service" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "multicluster.ServiceName" (dict "a" (list $dot)))) "r") "labels" (get (fromJson (include "multicluster.Labels" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace)) "spec" (mustMergeOverwrite (dict) (dict "ports" (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "https" "port" (9443 | int) "targetPort" (9443 | int)))) "selector" (get (fromJson (include "multicluster.SelectorLabels" (dict "a" (list $dot)))) "r") "type" "LoadBalancer"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

