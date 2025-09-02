{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/poddisruptionbudget.go" */ -}}

{{- define "redpanda.PodDisruptionBudget" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $budget := ($state.Values.statefulset.budget.maxUnavailable | int) -}}
{{- $replicas := ($state.Values.statefulset.replicas | int) -}}
{{- range $_, $set := $state.Pools -}}
{{- $replicas = ((add $replicas ($set.Statefulset.replicas | int)) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $minReplicas := ((div $replicas (2 | int)) | int) -}}
{{- if (and (gt $budget (1 | int)) (gt $budget $minReplicas)) -}}
{{- $_ := (fail (printf "statefulset.budget.maxUnavailable is set too high to maintain quorum: %d > %d" $budget $minReplicas)) -}}
{{- end -}}
{{- $maxUnavailable := ($budget | int) -}}
{{- $matchLabels := (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") -}}
{{- $_ := (set $matchLabels "redpanda.com/poddisruptionbudget" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "disruptionsAllowed" 0 "currentHealthy" 0 "desiredHealthy" 0 "expectedPods" 0)) (mustMergeOverwrite (dict) (dict "apiVersion" "policy/v1" "kind" "PodDisruptionBudget")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict) (dict "selector" (mustMergeOverwrite (dict) (dict "matchLabels" $matchLabels)) "maxUnavailable" $maxUnavailable))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

