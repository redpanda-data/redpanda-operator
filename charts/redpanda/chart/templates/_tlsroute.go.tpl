{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/tlsroute.go" */ -}}

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
{{- $labels := (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") -}}
{{- $annotations := (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r") -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $routes := (coalesce nil) -}}
{{- range $name, $listener := $state.Values.listeners.kafka.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $annotations (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $listener $gw.parentRefs)))) "r") $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "kafka" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.http.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $annotations (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $listener $gw.parentRefs)))) "r") $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "http" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.admin.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $annotations (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $listener $gw.parentRefs)))) "r") $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "admin" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.schemaRegistry.external -}}
{{- if (or (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) (not (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $listener)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $annotations (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $listener $gw.parentRefs)))) "r") $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "schema" ($listener.port | int))))) "r") -}}
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
{{- $annotations := (index .a 3) -}}
{{- $parentRefs := (index .a 4) -}}
{{- $pods := (index .a 5) -}}
{{- $host := (index .a 6) -}}
{{- $hostTemplate := (index .a 7) -}}
{{- $name := (index .a 8) -}}
{{- $listenerTag := (index .a 9) -}}
{{- $port := (index .a 10) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $routes := (coalesce nil) -}}
{{- $bootstrapSvcName := (printf "%s-gateway-bootstrap" $fullname) -}}
{{- $bootstrap := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-bootstrap" $fullname $listenerTag $name) "namespace" $namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" (list (toString $host)) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $bootstrapSvcName) "port" ($port | int))) (dict)))))))))) -}}
{{- $routes = (concat (default (list) $routes) (list $bootstrap)) -}}
{{- if (eq $hostTemplate "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $brokerHost := (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $i $podname)))) "r") -}}
{{- $brokerSvcName := (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") -}}
{{- $route := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-%d" $fullname $listenerTag $name $i) "namespace" $namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" (list (toString $brokerHost)) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $brokerSvcName) "port" ($port | int))) (dict)))))))))) -}}
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
{{- $pods := (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
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
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $replicas := ((get (fromJson (include "_shims.len" (dict "a" (list $pods)))) "r") | int) -}}
{{- $gatewayConfigured := (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") -}}
{{- $defaultRefs := (coalesce nil) -}}
{{- if (ne (toJson $state.Values.external.gateway) "null") -}}
{{- $defaultRefs = $state.Values.external.gateway.parentRefs -}}
{{- end -}}
{{- $claimed := (dict) -}}
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "kafka" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas true)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "kafka" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas true)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "http" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "http" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "admin" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "admin" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "schema" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "schema" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.validateGatewayListener" -}}
{{- $tag := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $listenerType := (index .a 2) -}}
{{- $enabled := (index .a 3) -}}
{{- $gatewayConfigured := (index .a 4) -}}
{{- $host := (index .a 5) -}}
{{- $hostTemplate := (index .a 6) -}}
{{- $replicas := (index .a 7) -}}
{{- $requirePerBroker := (index .a 8) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $enabled) ((and (ne $listenerType "tlsroute") (ne $listenerType "tcproute")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not $gatewayConfigured) -}}
{{- $_ := (fail (printf "external listener %s/%s sets type: %s but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs" $tag $name $listenerType)) -}}
{{- end -}}
{{- if (eq $listenerType "tcproute") -}}
{{- if (eq $host "") -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `host` (the advertised host every broker shares) when type: tcproute" $tag $name)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $host "") -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute" $tag $name)) -}}
{{- end -}}
{{- if (and (and $requirePerBroker (gt $replicas (1 | int))) (eq $hostTemplate "")) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname" $tag $name)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.validateTCPRouteListener" -}}
{{- $claimed := (index .a 0) -}}
{{- $parentRefs := (index .a 1) -}}
{{- $namespace := (index .a 2) -}}
{{- $tag := (index .a 3) -}}
{{- $name := (index .a 4) -}}
{{- $active := (index .a 5) -}}
{{- $networkPort := (index .a 6) -}}
{{- $base := (index .a 7) -}}
{{- $replicas := (index .a 8) -}}
{{- $requirePerBroker := (index .a 9) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $active) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $networkPort (0 | int)) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `networkPort` (the Gateway listener port of the bootstrap TCPRoute) when type: tcproute" $tag $name)) -}}
{{- end -}}
{{- if (and (and $requirePerBroker (gt $replicas (1 | int))) (eq $base (0 | int))) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `brokerNetworkPortBase` when replicas > 1: TCPRoutes carry no hostname, so each broker needs its own Gateway port" $tag $name)) -}}
{{- end -}}
{{- range $_, $ref := $parentRefs -}}
{{- $gw := (printf "%s %s/%s" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $ref.kind (toString "Gateway"))))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $ref.namespace (toString $namespace))))) "r") $ref.name) -}}
{{- $_ := (get (fromJson (include "redpanda.claimNetworkPort" (dict "a" (list $claimed $gw $tag $name "bootstrap" $networkPort)))) "r") -}}
{{- if (eq $base (0 | int)) -}}
{{- continue -}}
{{- end -}}
{{- range $_, $i := (until $replicas) -}}
{{- $_ := (get (fromJson (include "redpanda.claimNetworkPort" (dict "a" (list $claimed $gw $tag $name (printf "broker %d" $i) ((add $base ($i | int)) | int))))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.claimNetworkPort" -}}
{{- $claimed := (index .a 0) -}}
{{- $gw := (index .a 1) -}}
{{- $tag := (index .a 2) -}}
{{- $name := (index .a 3) -}}
{{- $what := (index .a 4) -}}
{{- $port := (index .a 5) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (lt $port (1 | int)) (gt $port (65535 | int))) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: %s network port %d is outside 1-65535" $tag $name $what $port)) -}}
{{- end -}}
{{- if (not (hasKey $claimed $gw)) -}}
{{- $_ := (set $claimed $gw (dict)) -}}
{{- end -}}
{{- $ports := (index $claimed $gw) -}}
{{- $key := (printf "%d" $port) -}}
{{- if (hasKey $ports $key) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: %s network port %d is already used by %s; every TCPRoute needs its own Gateway listener port" $tag $name $what $port (ternary (index $ports $key) "" (hasKey $ports $key)))) -}}
{{- end -}}
{{- $_ := (set $ports $key (printf "%s/%s %s" $tag $name $what)) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) (64 | int)) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: %s needs more than %d TCPRoute ports on %s, the Gateway API listener limit; move listeners to another Gateway with per-listener parentRefs" $tag $name $what (64 | int) $gw)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

