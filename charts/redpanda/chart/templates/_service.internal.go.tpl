{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/service_internal.go" */ -}}

{{- define "redpanda.MonitoringEnabledLabel" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "monitoring.redpanda.com/enabled" (printf "%t" $state.Values.monitoring.enabled))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.internalServiceConfig" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $annotations := (dict) -}}
{{- if (ne (toJson $state.Values.service) "null") -}}
{{- $annotations = $state.Values.service.internal.annotations -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "ByKind" (coalesce nil)) "Template" (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) "Brokers" (coalesce nil)) (dict "Kind" "headless" "Listeners" $listeners "Template" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (merge (dict) (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.MonitoringEnabledLabel" (dict "a" (list $state)))) "r")) "annotations" (merge (dict) $annotations (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r")))) "spec" (mustMergeOverwrite (dict) (dict "type" "ClusterIP" "publishNotReadyAddresses" true "clusterIP" "None" "selector" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

