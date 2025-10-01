{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/service.loadbalancer.go" */ -}}

{{- define "redpanda.LoadBalancerServices" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.external.enabled) (not $state.Values.external.service.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne $state.Values.external.type "LoadBalancer") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $externalDNS := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.externalDns (mustMergeOverwrite (dict "enabled" false) (dict)))))) "r") -}}
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") -}}
{{- $_ := (set $labels "repdanda.com/type" "loadbalancer") -}}
{{- $selector := (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") -}}
{{- $services := (coalesce nil) -}}
{{- $pods := (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" ""))) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $pods = (concat (default (list) $pods) (default (list) (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $annotations := (dict) -}}
{{- range $k, $v := $state.Values.external.annotations -}}
{{- $_ := (set $annotations $k $v) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
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
{{- $podSelector := (dict) -}}
{{- range $k, $v := $selector -}}
{{- $_ := (set $podSelector $k $v) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $podSelector "statefulset.kubernetes.io/pod-name" $podname) -}}
{{- $ports := (coalesce nil) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $state.Values.listeners.admin "admin" $state.Values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $state.Values.listeners.kafka "kafka" $state.Values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $state.Values.listeners.http "http" $state.Values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $state.Values.listeners.schemaRegistry "schema" $state.Values.external)))) "r"))) -}}
{{- $svc := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "lb-%s" $podname) "namespace" $state.Release.Namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (dict "externalTrafficPolicy" "Local" "loadBalancerSourceRanges" $state.Values.external.sourceRanges "ports" $ports "publishNotReadyAddresses" true "selector" $podSelector "sessionAffinity" "None" "type" "LoadBalancer")))) -}}
{{- $services = (concat (default (list) $services) (list $svc)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $services) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

