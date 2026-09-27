{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/tcproute.go" */ -}}

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
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $routes = (concat (default (list) $routes) (default (list) (get (fromJson (include "redpanda.tcpRoutesForListener" (dict "a" (list $state (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $gw.parentRefs)))) "r") $pods "kafka" $name ($l.port | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int))))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $routes = (concat (default (list) $routes) (default (list) (get (fromJson (include "redpanda.tcpRoutesForListener" (dict "a" (list $state (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $gw.parentRefs)))) "r") $pods "http" $name ($l.port | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int))))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $routes = (concat (default (list) $routes) (default (list) (get (fromJson (include "redpanda.tcpRoutesForListener" (dict "a" (list $state (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $gw.parentRefs)))) "r") $pods "admin" $name ($l.port | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int))))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $routes = (concat (default (list) $routes) (default (list) (get (fromJson (include "redpanda.tcpRoutesForListener" (dict "a" (list $state (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $gw.parentRefs)))) "r") $pods "schema" $name ($l.port | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int))))) "r"))) -}}
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

{{- define "redpanda.tcpRoutesForListener" -}}
{{- $state := (index .a 0) -}}
{{- $parentRefs := (index .a 1) -}}
{{- $pods := (index .a 2) -}}
{{- $tag := (index .a 3) -}}
{{- $name := (index .a 4) -}}
{{- $port := (index .a 5) -}}
{{- $networkPort := (index .a 6) -}}
{{- $base := (index .a 7) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $routes := (list (get (fromJson (include "redpanda.tcpRoute" (dict "a" (list $state (printf "%s-%s-%s-bootstrap" $fullname $tag $name) $parentRefs $networkPort (printf "%s-gateway-bootstrap" $fullname) $port)))) "r")) -}}
{{- if (eq $base (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $routes = (concat (default (list) $routes) (list (get (fromJson (include "redpanda.tcpRoute" (dict "a" (list $state (printf "%s-%s-%s-%d" $fullname $tag $name $i) $parentRefs ((add $base ($i | int)) | int) (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") $port)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.tcpRoute" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $parentRefs := (index .a 2) -}}
{{- $networkPort := (index .a 3) -}}
{{- $backend := (index .a 4) -}}
{{- $port := (index .a 5) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TCPRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $name "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" (get (fromJson (include "redpanda.tcpParentRefs" (dict "a" (list $parentRefs $networkPort)))) "r"))) (dict "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $backend) "port" ($port | int))) (dict))))))))))) | toJson -}}
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

