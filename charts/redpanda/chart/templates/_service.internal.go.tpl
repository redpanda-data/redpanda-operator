{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/service_internal.go" */ -}}

{{- define "redpanda.MonitoringEnabledLabel" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "monitoring.redpanda.com/enabled" (printf "%t" $state.Values.monitoring.enabled))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ServiceInternal" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (list) -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "admin" "protocol" "TCP" "appProtocol" $state.Values.listeners.admin.appProtocol "port" ($state.Values.listeners.admin.port | int) "targetPort" ($state.Values.listeners.admin.port | int))))) -}}
{{- if $state.Values.listeners.http.enabled -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "http" "protocol" "TCP" "port" ($state.Values.listeners.http.port | int) "targetPort" ($state.Values.listeners.http.port | int))))) -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "kafka" "protocol" "TCP" "port" ($state.Values.listeners.kafka.port | int) "targetPort" ($state.Values.listeners.kafka.port | int))))) -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "rpc" "protocol" "TCP" "port" ($state.Values.listeners.rpc.port | int) "targetPort" ($state.Values.listeners.rpc.port | int))))) -}}
{{- if $state.Values.listeners.schemaRegistry.enabled -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "schemaregistry" "protocol" "TCP" "port" ($state.Values.listeners.schemaRegistry.port | int) "targetPort" ($state.Values.listeners.schemaRegistry.port | int))))) -}}
{{- end -}}
{{- $annotations := (dict) -}}
{{- if (ne (toJson $state.Values.service) "null") -}}
{{- $annotations = $state.Values.service.internal.annotations -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (merge (dict) (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.MonitoringEnabledLabel" (dict "a" (list $state)))) "r")) "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (dict "type" "ClusterIP" "publishNotReadyAddresses" true "clusterIP" "None" "selector" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") "ports" $ports))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

