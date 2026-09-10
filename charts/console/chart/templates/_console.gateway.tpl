{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/gateway.go" */ -}}

{{- define "console.HTTPRoute" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.gateway.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $parentRefs := (coalesce nil) -}}
{{- range $_, $parentRef := $state.Values.gateway.parentRefs -}}
{{- $ref := (mustMergeOverwrite (dict "name" "") (dict "name" (toString (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $parentRef.name))))) "r")))) -}}
{{- if (ne (toJson $parentRef.namespace) "null") -}}
{{- $namespace := (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $parentRef.namespace))))) "r") -}}
{{- $_ := (set $ref "namespace" (toString $namespace)) -}}
{{- end -}}
{{- if (ne (toJson $parentRef.sectionName) "null") -}}
{{- $sectionName := (toString (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list (toString $parentRef.sectionName)))))) "r")) -}}
{{- $_ := (set $ref "sectionName" $sectionName) -}}
{{- end -}}
{{- $parentRefs = (concat (default (list) $parentRefs) (list $ref)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $hostnames := (coalesce nil) -}}
{{- range $_, $hostname := $state.Values.gateway.hostnames -}}
{{- $hostnames = (concat (default (list) $hostnames) (list (toString (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $hostname))))) "r")))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $pathType := "PathPrefix" -}}
{{- if (ne (toJson $state.Values.gateway.pathType) "null") -}}
{{- $pathType = $state.Values.gateway.pathType -}}
{{- end -}}
{{- $path := (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $state.Values.gateway.path))))) "r") -}}
{{- $port := (($state.Values.service.port | int) | int) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "HTTPRoute" "apiVersion" "gateway.networking.k8s.io/v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "namespace" $state.Namespace "annotations" $state.Values.gateway.annotations)) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" $hostnames "rules" (list (mustMergeOverwrite (dict) (dict "matches" (list (mustMergeOverwrite (dict) (dict "path" (mustMergeOverwrite (dict) (dict "type" $pathType "value" $path))))) "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")) "port" $port)) (dict)) (dict))))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

