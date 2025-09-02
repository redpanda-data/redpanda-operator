{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/ingress.go" */ -}}

{{- define "console.Ingress" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.ingress.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $tls := (coalesce nil) -}}
{{- range $_, $t := $state.Values.ingress.tls -}}
{{- $hosts := (coalesce nil) -}}
{{- range $_, $host := $t.hosts -}}
{{- $hosts = (concat (default (list) $hosts) (list (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $host))))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $tls = (concat (default (list) $tls) (list (mustMergeOverwrite (dict) (dict "secretName" $t.secretName "hosts" $hosts)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $rules := (coalesce nil) -}}
{{- range $_, $host := $state.Values.ingress.hosts -}}
{{- $paths := (coalesce nil) -}}
{{- range $_, $path := $host.paths -}}
{{- $paths = (concat (default (list) $paths) (list (mustMergeOverwrite (dict "pathType" (coalesce nil) "backend" (dict)) (dict "path" $path.path "pathType" $path.pathType "backend" (mustMergeOverwrite (dict) (dict "service" (mustMergeOverwrite (dict "name" "" "port" (dict)) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "port" (mustMergeOverwrite (dict) (dict "number" ($state.Values.service.port | int))))))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $rules = (concat (default (list) $rules) (list (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "http" (mustMergeOverwrite (dict "paths" (coalesce nil)) (dict "paths" $paths)))) (dict "host" (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $host.host))))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "kind" "Ingress" "apiVersion" "networking.k8s.io/v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "namespace" $state.Namespace "annotations" $state.Values.ingress.annotations)) "spec" (mustMergeOverwrite (dict) (dict "ingressClassName" $state.Values.ingress.className "tls" $tls "rules" $rules))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

