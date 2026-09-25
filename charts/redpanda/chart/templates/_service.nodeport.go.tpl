{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/service.nodeport.go" */ -}}

{{- define "redpanda.NodePortService" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.external.enabled) (not $state.Values.external.service.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne $state.Values.external.type "NodePort") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $ports := (get (fromJson (include "_redpanda.Listeners.NodePortServicePorts" (dict "a" (list $listeners)))) "r") -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $annotations := $state.Values.external.annotations -}}
{{- if (eq (toJson $annotations) "null") -}}
{{- $annotations = (dict) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-external" (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (merge (dict) $annotations (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r")))) "spec" (mustMergeOverwrite (dict) (dict "externalTrafficPolicy" "Local" "ports" $ports "publishNotReadyAddresses" true "selector" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") "sessionAffinity" "None" "type" "NodePort"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

