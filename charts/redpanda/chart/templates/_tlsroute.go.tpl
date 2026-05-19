{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/tlsroute.go" */ -}}

{{- define "redpanda.TLSRoutes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $gw := $state.Values.external.gateway -}}
{{- $parentRefs := (get (fromJson (include "redpanda.toTLSRouteParentRefs" (dict "a" (list $gw.parentRefs)))) "r") -}}
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $routes := (coalesce nil) -}}
{{- range $name, $listener := $state.Values.listeners.kafka.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "kafka" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.http.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "http" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.admin.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "admin" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.schemaRegistry.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "schema" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.tlsRoutesForListener" -}}
{{- $fullname := (index .a 0) -}}
{{- $namespace := (index .a 1) -}}
{{- $labels := (index .a 2) -}}
{{- $parentRefs := (index .a 3) -}}
{{- $pods := (index .a 4) -}}
{{- $host := (index .a 5) -}}
{{- $hostTemplate := (index .a 6) -}}
{{- $name := (index .a 7) -}}
{{- $listenerTag := (index .a 8) -}}
{{- $port := (index .a 9) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $routes := (coalesce nil) -}}
{{- $bootstrapSvcName := (printf "%s-gateway-bootstrap" $fullname) -}}
{{- $bootstrap := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "parentRefs" (coalesce nil) "rules" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-bootstrap" $fullname $listenerTag $name) "namespace" $namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict "parentRefs" (coalesce nil) "rules" (coalesce nil)) (dict "parentRefs" $parentRefs "hostnames" (list $host) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "" "port" 0) (dict "name" $bootstrapSvcName "port" $port)))))))))) -}}
{{- $routes = (concat (default (list) $routes) (list $bootstrap)) -}}
{{- if (eq $hostTemplate "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $brokerHost := (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $i $podname)))) "r") -}}
{{- $brokerSvcName := (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") -}}
{{- $route := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "parentRefs" (coalesce nil) "rules" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-%d" $fullname $listenerTag $name $i) "namespace" $namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict "parentRefs" (coalesce nil) "rules" (coalesce nil)) (dict "parentRefs" $parentRefs "hostnames" (list $brokerHost) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "" "port" 0) (dict "name" $brokerSvcName "port" $port)))))))))) -}}
{{- $routes = (concat (default (list) $routes) (list $route)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.toTLSRouteParentRefs" -}}
{{- $refs := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $parentRefs := (coalesce nil) -}}
{{- range $_, $ref := $refs -}}
{{- $parentRefs = (concat (default (list) $parentRefs) (list $ref)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $parentRefs) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.renderBrokerHost" -}}
{{- $tmpl := (index .a 0) -}}
{{- $ordinal := (index .a 1) -}}
{{- $podName := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $result := (replace "$POD_ORDINAL" (printf "%d" $ordinal) $tmpl) -}}
{{- $result = (replace "$POD_NAME" $podName $result) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.gatewayPodNames" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $pods := (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $pods = (concat (default (list) $pods) (default (list) (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $pods) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.gatewayBrokerServiceName" -}}
{{- $podName := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $name := (printf "gw-%s" $podName) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $name)))) "r") | int) (63 | int)) -}}
{{- $_ := (fail (printf "gateway per-broker service name %q exceeds the 63-character RFC 1035 limit for Service names; shorten fullnameOverride/nameOverride or the node-pool suffix so that \"gw-\"+<pod name> fits" $name)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $name) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.validateGatewayListeners" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $replicas := ((get (fromJson (include "_shims.len" (dict "a" (list (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r"))))) "r") | int) -}}
{{- $gatewayConfigured := (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") -}}
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "kafka" $name (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $l)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas true)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "http" $name (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $l)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "admin" $name (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $l)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "schema" $name (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $l)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.validateGatewayListener" -}}
{{- $tag := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $isGateway := (index .a 2) -}}
{{- $enabled := (index .a 3) -}}
{{- $gatewayConfigured := (index .a 4) -}}
{{- $host := (index .a 5) -}}
{{- $hostTemplate := (index .a 6) -}}
{{- $replicas := (index .a 7) -}}
{{- $requirePerBroker := (index .a 8) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $enabled) (not $isGateway)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not $gatewayConfigured) -}}
{{- $_ := (fail (printf "external listener %s/%s sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs" $tag $name)) -}}
{{- end -}}
{{- if (eq $host "") -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute" $tag $name)) -}}
{{- end -}}
{{- if (and (and $requirePerBroker (gt $replicas (1 | int))) (eq $hostTemplate "")) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname" $tag $name)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

