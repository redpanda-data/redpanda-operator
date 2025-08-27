{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/servicemonitor.go" */ -}}

{{- define "redpanda.ServiceMonitor" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.monitoring.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $endpoint := (mustMergeOverwrite (dict) (dict "interval" $state.Values.monitoring.scrapeInterval "path" "/public_metrics" "port" "admin" "enableHttp2" $state.Values.monitoring.enableHttp2 "scheme" "http")) -}}
{{- if (or (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") (ne (toJson $state.Values.monitoring.tlsConfig) "null")) -}}
{{- $_ := (set $endpoint "scheme" "https") -}}
{{- $_ := (set $endpoint "tlsConfig" $state.Values.monitoring.tlsConfig) -}}
{{- if (eq (toJson $endpoint.tlsConfig) "null") -}}
{{- $_ := (set $endpoint "tlsConfig" (mustMergeOverwrite (dict "ca" (dict) "cert" (dict)) (mustMergeOverwrite (dict "ca" (dict) "cert" (dict)) (dict "insecureSkipVerify" true)) (dict))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "monitoring.coreos.com/v1" "kind" "ServiceMonitor")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (merge (dict) (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") $state.Values.monitoring.labels))) "spec" (mustMergeOverwrite (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict)) (dict "endpoints" (list $endpoint) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (dict "monitoring.redpanda.com/enabled" "true" "app.kubernetes.io/name" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") "app.kubernetes.io/instance" $state.Release.Name)))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

