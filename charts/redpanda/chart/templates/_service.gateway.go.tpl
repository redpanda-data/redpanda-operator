{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/service.gateway.go" */ -}}

{{- define "redpanda.gatewayServiceConfig" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokers := (coalesce nil) -}}
{{- range $_, $podname := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $brokers = (concat (default (list) $brokers) (list (mustMergeOverwrite (dict "Name" "" "Selector" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") "Selector" (dict "statefulset.kubernetes.io/pod-name" $podname))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "ByKind" (coalesce nil)) "Template" (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) "Brokers" (coalesce nil)) (dict "Kind" "gateway" "Listeners" $listeners "Template" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-gateway-bootstrap" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict) (dict "publishNotReadyAddresses" true "selector" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") "sessionAffinity" "None" "type" "ClusterIP")))) "Brokers" $brokers))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

