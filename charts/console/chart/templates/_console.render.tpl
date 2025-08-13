{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/render.go" */ -}}

{{- define "console.RenderState.ChartName" -}}
{{- $s := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $name := "console" -}}
{{- if (ne $s.Values.nameOverride "") -}}
{{- $name = $s.Values.nameOverride -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.cleanForK8s" (dict "a" (list $name)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.RenderState.FullName" -}}
{{- $s := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne $s.Values.fullnameOverride "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.cleanForK8s" (dict "a" (list $s.Values.fullnameOverride)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $name := (get (fromJson (include "console.RenderState.ChartName" (dict "a" (list $s)))) "r") -}}
{{- if (not (contains $s.ReleaseName $name)) -}}
{{- $name = (printf "%s-%s" $s.ReleaseName $name) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.cleanForK8s" (dict "a" (list $name)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.RenderState.SelectorLabels" -}}
{{- $s := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "app.kubernetes.io/name" (get (fromJson (include "console.RenderState.ChartName" (dict "a" (list $s)))) "r") "app.kubernetes.io/instance" $s.ReleaseName)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.RenderState.Labels" -}}
{{- $s := (index .a 0) -}}
{{- $labels := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $labels) "null") -}}
{{- $labels = (dict) -}}
{{- end -}}
{{- range $key, $value := (get (fromJson (include "console.RenderState.SelectorLabels" (dict "a" (list $s)))) "r") -}}
{{- $_ := (set $labels $key $value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $key, $value := $s.CommonLabels -}}
{{- $_ := (set $labels $key $value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $key, $value := $s.Values.commonLabels -}}
{{- $_ := (set $labels $key $value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $labels) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.Render" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $manifests := (list (get (fromJson (include "console.ServiceAccount" (dict "a" (list $state)))) "r") (get (fromJson (include "console.Secret" (dict "a" (list $state)))) "r") (get (fromJson (include "console.ConfigMap" (dict "a" (list $state)))) "r") (get (fromJson (include "console.Service" (dict "a" (list $state)))) "r") (get (fromJson (include "console.Ingress" (dict "a" (list $state)))) "r") (get (fromJson (include "console.Deployment" (dict "a" (list $state)))) "r") (get (fromJson (include "console.HorizontalPodAutoscaler" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.Types" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) "status" (dict)) (dict)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "scaleTargetRef" (dict "kind" "" "name" "") "maxReplicas" 0) "status" (dict "desiredReplicas" 0 "currentMetrics" (coalesce nil))) (dict)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.cleanForK8s" -}}
{{- $s := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (trimSuffix "-" (trunc (63 | int) $s))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

