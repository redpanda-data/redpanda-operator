{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/servicemonitor.go" */ -}}

{{- define "operator.ServiceMonitor" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.monitoring.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $endpoint := (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "tlsConfig" (mustMergeOverwrite (dict "ca" (dict) "cert" (dict)) (mustMergeOverwrite (dict "ca" (dict) "cert" (dict)) (dict "insecureSkipVerify" true)) (dict)))) (dict)) (dict "port" "https" "path" "/metrics" "scheme" "HTTPS" "bearerTokenFile" "/var/run/secrets/kubernetes.io/serviceaccount/token")) -}}
{{- if (ne $values.monitoring.scrapeInterval "") -}}
{{- $_ := (set $endpoint "interval" (toString $values.monitoring.scrapeInterval)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict))) (mustMergeOverwrite (dict) (dict "kind" "ServiceMonitor" "apiVersion" "monitoring.coreos.com/v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "metrics-monitor")))) "r") "labels" (merge (dict) (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") $values.monitoring.labels) "namespace" $dot.Release.Namespace "annotations" $values.annotations)) "spec" (mustMergeOverwrite (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict)) (dict "endpoints" (list $endpoint) "namespaceSelector" (mustMergeOverwrite (dict) (dict "matchNames" (list $dot.Release.Namespace))) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

