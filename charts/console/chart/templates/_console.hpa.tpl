{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/hpa.go" */ -}}

{{- define "console.HorizontalPodAutoscaler" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.autoscaling.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $metrics := (list) -}}
{{- if (ne (toJson $state.Values.autoscaling.targetCPUUtilizationPercentage) "null") -}}
{{- $metrics = (concat (default (list) $metrics) (list (mustMergeOverwrite (dict "type" "") (dict "type" "Resource" "resource" (mustMergeOverwrite (dict "name" "" "target" (dict "type" "")) (dict "name" "cpu" "target" (mustMergeOverwrite (dict "type" "") (dict "type" "Utilization" "averageUtilization" $state.Values.autoscaling.targetCPUUtilizationPercentage)))))))) -}}
{{- end -}}
{{- if (ne (toJson $state.Values.autoscaling.targetMemoryUtilizationPercentage) "null") -}}
{{- $metrics = (concat (default (list) $metrics) (list (mustMergeOverwrite (dict "type" "") (dict "type" "Resource" "resource" (mustMergeOverwrite (dict "name" "" "target" (dict "type" "")) (dict "name" "memory" "target" (mustMergeOverwrite (dict "type" "") (dict "type" "Utilization" "averageUtilization" $state.Values.autoscaling.targetMemoryUtilizationPercentage)))))))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "scaleTargetRef" (dict "kind" "" "name" "") "maxReplicas" 0) "status" (dict "desiredReplicas" 0 "currentMetrics" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "autoscaling/v2" "kind" "HorizontalPodAutoscaler")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace)) "spec" (mustMergeOverwrite (dict "scaleTargetRef" (dict "kind" "" "name" "") "maxReplicas" 0) (dict "scaleTargetRef" (mustMergeOverwrite (dict "kind" "" "name" "") (dict "apiVersion" "apps/v1" "kind" "Deployment" "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) "minReplicas" ($state.Values.autoscaling.minReplicas | int) "maxReplicas" ($state.Values.autoscaling.maxReplicas | int) "metrics" $metrics))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

