{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/tcproute.go" */ -}}

{{- define "redpanda.tcpRouteListeners" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $defaults := $state.Values.external.gateway.parentRefs -}}
{{- $out := (coalesce nil) -}}
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "NetworkPort" 0 "Base" 0 "ParentRefs" (coalesce nil)) (dict "Tag" "kafka" "Name" $name "Port" ($l.port | int) "NetworkPort" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) "Base" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "NetworkPort" 0 "Base" 0 "ParentRefs" (coalesce nil)) (dict "Tag" "http" "Name" $name "Port" ($l.port | int) "NetworkPort" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) "Base" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "NetworkPort" 0 "Base" 0 "ParentRefs" (coalesce nil)) (dict "Tag" "admin" "Name" $name "Port" ($l.port | int) "NetworkPort" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) "Base" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "NetworkPort" 0 "Base" 0 "ParentRefs" (coalesce nil)) (dict "Tag" "schema" "Name" $name "Port" ($l.port | int) "NetworkPort" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) "Base" ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.tcpRoutePorts" -}}
{{- $state := (index .a 0) -}}
{{- $pods := (index .a 1) -}}
{{- $l := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $section := (printf "%s-%s-bootstrap" $l.Tag $l.Name) -}}
{{- $ports := (list (mustMergeOverwrite (dict "Name" "" "Section" "" "NetworkPort" 0 "Backend" "") (dict "Name" (printf "%s-%s" $fullname $section) "Section" $section "NetworkPort" ($l.NetworkPort | int) "Backend" (printf "%s-gateway-bootstrap" $fullname)))) -}}
{{- if (eq ($l.Base | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $section := (printf "%s-%s-%d" $l.Tag $l.Name $i) -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "Name" "" "Section" "" "NetworkPort" 0 "Backend" "") (dict "Name" (printf "%s-%s" $fullname $section) "Section" $section "NetworkPort" ((add ($l.Base | int) ($i | int)) | int) "Backend" (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.TCPRoutes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $gw := $state.Values.external.gateway -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $routes := (coalesce nil) -}}
{{- range $_, $l := (get (fromJson (include "redpanda.tcpRouteListeners" (dict "a" (list $state)))) "r") -}}
{{- range $_, $p := (get (fromJson (include "redpanda.tcpRoutePorts" (dict "a" (list $state $pods $l)))) "r") -}}
{{- $parentRefs := (get (fromJson (include "redpanda.tcpParentRefs" (dict "a" (list $l.ParentRefs ($p.NetworkPort | int))))) "r") -}}
{{- if (get (fromJson (include "redpanda.GatewayConfig.IsListenerSetEnabled" (dict "a" (list $gw)))) "r") -}}
{{- $lsRefs := (get (fromJson (include "redpanda.listenerSetParentRefs" (dict "a" (list $state $l.ParentRefs $p.Section)))) "r") -}}
{{- if (get (fromJson (include "redpanda.GatewayConfig.AttachesRoutesToGateway" (dict "a" (list $gw)))) "r") -}}
{{- $parentRefs = (concat (default (list) $parentRefs) (default (list) $lsRefs)) -}}
{{- else -}}
{{- $parentRefs = $lsRefs -}}
{{- end -}}
{{- end -}}
{{- $routes = (concat (default (list) $routes) (list (get (fromJson (include "redpanda.tcpRoute" (dict "a" (list $state $p.Name $parentRefs $p.Backend ($l.Port | int))))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ListenerSets" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r")) (not (get (fromJson (include "redpanda.GatewayConfig.IsListenerSetEnabled" (dict "a" (list $state.Values.external.gateway)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $listeners := (get (fromJson (include "redpanda.tcpRouteListeners" (dict "a" (list $state)))) "r") -}}
{{- $seen := (dict) -}}
{{- $gateways := (coalesce nil) -}}
{{- range $_, $l := $listeners -}}
{{- range $_, $ref := $l.ParentRefs -}}
{{- $key := (get (fromJson (include "redpanda.listenerSetName" (dict "a" (list $state $ref)))) "r") -}}
{{- if (not (hasKey $seen $key)) -}}
{{- $_ := (set $seen $key true) -}}
{{- $gateways = (concat (default (list) $gateways) (list $ref)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sets := (coalesce nil) -}}
{{- range $_, $gw := $gateways -}}
{{- $name := (get (fromJson (include "redpanda.listenerSetName" (dict "a" (list $state $gw)))) "r") -}}
{{- $entries := (coalesce nil) -}}
{{- range $_, $l := $listeners -}}
{{- $onGateway := false -}}
{{- range $_, $ref := $l.ParentRefs -}}
{{- if (eq (get (fromJson (include "redpanda.listenerSetName" (dict "a" (list $state $ref)))) "r") $name) -}}
{{- $onGateway = true -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if (not $onGateway) -}}
{{- continue -}}
{{- end -}}
{{- range $_, $p := (get (fromJson (include "redpanda.tcpRoutePorts" (dict "a" (list $state $pods $l)))) "r") -}}
{{- $entries = (concat (default (list) $entries) (list (mustMergeOverwrite (dict) (dict "name" (toString $p.Section) "port" (($p.NetworkPort | int) | int) "protocol" (toString "TCP") "allowedRoutes" (mustMergeOverwrite (dict) (dict "namespaces" (mustMergeOverwrite (dict) (dict "from" (toString "Same"))) "kinds" (list (mustMergeOverwrite (dict "kind" "") (dict "kind" (toString "TCPRoute")))))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sets = (concat (default (list) $sets) (list (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "ListenerSet")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $name "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict "parentRef" (dict "name" "")) (dict "parentRef" (mustMergeOverwrite (dict "name" "") (dict "group" $gw.group "kind" $gw.kind "name" $gw.name "namespace" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $gw.namespace (toString $state.Release.Namespace))))) "r"))) "listeners" $entries)))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $sets) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.listenerSetName" -}}
{{- $state := (index .a 0) -}}
{{- $ref := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s-%s-%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $ref.namespace (toString $state.Release.Namespace))))) "r") $ref.name)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.tcpRoute" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $parentRefs := (index .a 2) -}}
{{- $backend := (index .a 3) -}}
{{- $port := (index .a 4) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TCPRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $name "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $backend) "port" ($port | int))) (dict))))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.tcpParentRefs" -}}
{{- $parentRefs := (index .a 0) -}}
{{- $networkPort := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (list) -}}
{{- range $_, $ref := $parentRefs -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "name" "") (dict "group" $ref.group "kind" $ref.kind "namespace" $ref.namespace "name" $ref.name "port" ($networkPort | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.listenerSetParentRefs" -}}
{{- $state := (index .a 0) -}}
{{- $parentRefs := (index .a 1) -}}
{{- $section := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (list) -}}
{{- range $_, $ref := $parentRefs -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "name" "") (dict "group" (toString "gateway.networking.k8s.io") "kind" (toString "ListenerSet") "name" (toString (get (fromJson (include "redpanda.listenerSetName" (dict "a" (list $state $ref)))) "r")) "sectionName" (toString $section))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

