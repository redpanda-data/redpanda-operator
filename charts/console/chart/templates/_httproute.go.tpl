{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/httproute.go" */ -}}

{{- define "console.HTTPRoutes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.httpRoute.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $hostnames := (coalesce nil) -}}
{{- range $_, $host := $state.Values.httpRoute.hostnames -}}
{{- $hostnames = (concat (default (list) $hostnames) (list (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $host))))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $route := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "parentRefs" (coalesce nil) "rules" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "HTTPRoute" "apiVersion" "gateway.networking.k8s.io/v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state $state.Values.httpRoute.labels)))) "r") "namespace" $state.Namespace "annotations" $state.Values.httpRoute.annotations)) "spec" (mustMergeOverwrite (dict "parentRefs" (coalesce nil) "rules" (coalesce nil)) (dict "parentRefs" $state.Values.httpRoute.parentRefs "hostnames" $hostnames "rules" (list (mustMergeOverwrite (dict) (dict "matches" $state.Values.httpRoute.matches "backendRefs" (list (mustMergeOverwrite (dict "name" "" "port" 0) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "port" ($state.Values.service.port | int))))))))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $route)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

