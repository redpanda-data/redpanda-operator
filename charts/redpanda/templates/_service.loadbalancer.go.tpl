{{- /* Generated from "service.loadbalancer.go" */ -}}

{{- define "redpanda.LoadBalancerServices" -}}
{{- $dot := (index .a 0) -}}
{{- $_pools := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (or (not $values.external.enabled) (not $values.external.service.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne $values.external.type "LoadBalancer") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $externalDNS := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $values.external.externalDns (mustMergeOverwrite (dict "enabled" false) (dict)))))) "r") -}}
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $dot)))) "r") -}}
{{- $_ := (set $labels "repdanda.com/type" "loadbalancer") -}}
{{- $selector := (get (fromJson (include "redpanda.StatefulSetPodLabelsSelector" (dict "a" (list $dot)))) "r") -}}
{{- $services := (coalesce nil) -}}
{{- $replicas := ($values.statefulset.replicas | int) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($values.statefulset.replicas | int)|int) (1|int) -}}
{{- $podname := (printf "%s-%d" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $dot)))) "r") $i) -}}
{{- $annotations := (dict) -}}
{{- range $k, $v := $values.external.annotations -}}
{{- $_ := (set $annotations $k $v) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if $externalDNS.enabled -}}
{{- $prefix := $podname -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $values.external.addresses)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $values.external.addresses)))) "r") | int) (1 | int)) -}}
{{- $prefix = (index $values.external.addresses (0 | int)) -}}
{{- else -}}
{{- $prefix = (index $values.external.addresses $i) -}}
{{- end -}}
{{- end -}}
{{- $address := (printf "%s.%s" $prefix (tpl $values.external.domain $dot)) -}}
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
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $values.listeners.admin "admin" $values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $values.listeners.kafka "kafka" $values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $values.listeners.http "http" $values.external)))) "r"))) -}}
{{- $ports = (concat (default (list) $ports) (default (list) (get (fromJson (include "redpanda.ListenerConfig.ServicePorts" (dict "a" (list $values.listeners.schemaRegistry "schema" $values.external)))) "r"))) -}}
{{- $svc := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "lb-%s" $podname) "namespace" $dot.Release.Namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (dict "externalTrafficPolicy" "Local" "loadBalancerSourceRanges" $values.external.sourceRanges "ports" $ports "publishNotReadyAddresses" true "selector" $podSelector "sessionAffinity" "None" "type" "LoadBalancer")))) -}}
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

