{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/service.loadbalancer.go" */ -}}

{{- define "redpanda.loadBalancerServiceConfig" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $externalDNS := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.externalDns (mustMergeOverwrite (dict "enabled" false) (dict)))))) "r") -}}
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") -}}
{{- $_ := (set $labels "redpanda.com/type" "loadbalancer") -}}
{{- $_ := (set $labels "repdanda.com/type" "loadbalancer") -}}
{{- $pods := (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $pods = (concat (default (list) $pods) (default (list) (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $brokers := (coalesce nil) -}}
{{- range $i, $podname := $pods -}}
{{- $annotations := (dict) -}}
{{- if $externalDNS.enabled -}}
{{- $prefix := $podname -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (1 | int)) -}}
{{- $prefix = (index $state.Values.external.addresses (0 | int)) -}}
{{- else -}}
{{- $prefix = (index $state.Values.external.addresses $i) -}}
{{- end -}}
{{- end -}}
{{- $address := (printf "%s.%s" $prefix (tpl $state.Values.external.domain $state.Dot)) -}}
{{- $_ := (set $annotations "external-dns.alpha.kubernetes.io/hostname" $address) -}}
{{- end -}}
{{- $brokers = (concat (default (list) $brokers) (list (mustMergeOverwrite (dict "Name" "" "Selector" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (printf "lb-%s" $podname) "Selector" (dict "statefulset.kubernetes.io/pod-name" $podname) "Annotations" $annotations)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $annotations := (dict) -}}
{{- $_ := (get (fromJson (include "_shims.maps_Copy" (dict "a" (list $annotations (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))))) "r") -}}
{{- $_ := (get (fromJson (include "_shims.maps_Copy" (dict "a" (list $annotations $state.Values.external.annotations)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "ByKind" (coalesce nil)) "Template" (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) "Brokers" (coalesce nil)) (dict "Kind" "loadbalancer" "Listeners" $listeners "Template" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "namespace" $state.Release.Namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (dict "externalTrafficPolicy" "Local" "loadBalancerSourceRanges" $state.Values.external.sourceRanges "publishNotReadyAddresses" true "selector" (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") "sessionAffinity" "None" "type" "LoadBalancer")))) "Brokers" $brokers))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

