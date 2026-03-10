{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/servicemonitor.go" */ -}}

{{- define "console.ServiceMonitor" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.monitoring.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $endpoint := (mustMergeOverwrite (dict) (dict "interval" $state.Values.monitoring.scrapeInterval "path" "/admin/metrics" "port" "http" "scheme" "HTTP")) -}}
{{- $tlsCertFilepath := (dig "server" "tls" "certFilepath" "" $state.Values.config) -}}
{{- $tlsKeyFilepath := (dig "server" "tls" "keyFilepath" "" $state.Values.config) -}}
{{- $tlsEnabled := (dig "server" "tls" "enabled" false $state.Values.config) -}}
{{- if (and (and (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" $tlsEnabled)))) "r") (ne $tlsCertFilepath "")) (ne $tlsKeyFilepath "")) -}}
{{- $tlsConfig := (mustMergeOverwrite (dict "ca" (dict) "cert" (dict)) (mustMergeOverwrite (dict) (dict "certFile" (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $tlsCertFilepath)))) "r") "keyFile" (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $tlsKeyFilepath)))) "r"))) (dict)) -}}
{{- $_ := (set $endpoint "tlsConfig" $tlsConfig) -}}
{{- $_ := (set $endpoint "scheme" "HTTPS") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "monitoring.coreos.com/v1" "kind" "ServiceMonitor")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (merge (dict) (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") $state.Values.monitoring.labels))) "spec" (mustMergeOverwrite (dict "endpoints" (coalesce nil) "selector" (dict) "namespaceSelector" (dict)) (dict "endpoints" (list $endpoint) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

