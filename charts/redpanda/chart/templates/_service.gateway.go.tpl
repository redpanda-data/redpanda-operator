{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/service.gateway.go" */ -}}

{{- define "redpanda.GatewayServices" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") -}}
{{- $selector := (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r") -}}
{{- $ports := (get (fromJson (include "redpanda.gatewayServicePorts" (dict "a" (list $state)))) "r") -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $services := (coalesce nil) -}}
{{- $bootstrap := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-gateway-bootstrap" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict) (dict "ports" $ports "publishNotReadyAddresses" true "selector" $selector "sessionAffinity" "None" "type" "ClusterIP")))) -}}
{{- $services = (concat (default (list) $services) (list $bootstrap)) -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- range $_, $podname := $pods -}}
{{- $podSelector := (dict) -}}
{{- range $k, $v := $selector -}}
{{- $_ := (set $podSelector $k $v) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $podSelector "statefulset.kubernetes.io/pod-name" $podname) -}}
{{- $svc := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") "namespace" $state.Release.Namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict) (dict "ports" $ports "publishNotReadyAddresses" true "selector" $podSelector "sessionAffinity" "None" "type" "ClusterIP")))) -}}
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

{{- define "redpanda.gatewayServicePorts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $name, $listener := $state.Values.listeners.admin.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" (printf "admin-%s" $name) "protocol" "TCP" "port" ($listener.port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.kafka.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" (printf "kafka-%s" $name) "protocol" "TCP" "port" ($listener.port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.http.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" (printf "http-%s" $name) "protocol" "TCP" "port" ($listener.port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.schemaRegistry.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" (printf "schema-%s" $name) "protocol" "TCP" "port" ($listener.port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

