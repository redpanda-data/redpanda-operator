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
{{- $pods := (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $pods = (concat (default (list) $pods) (default (list) (get (fromJson (include "redpanda.PodNames" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $routes := (coalesce nil) -}}
{{- range $name, $listener := $state.Values.listeners.kafka.external -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "kafka" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.http.external -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "http" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.admin.external -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $rs := (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $parentRefs $pods (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $name "admin" ($listener.port | int))))) "r") -}}
{{- $routes = (concat (default (list) $routes) (default (list) $rs)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $listener := $state.Values.listeners.schemaRegistry.external -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r")) -}}
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
{{- if (eq $host "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $bootstrapSvcName := (printf "%s-gateway-bootstrap" $fullname) -}}
{{- $bootstrap := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "parentRefs" (coalesce nil) "rules" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1alpha2" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-bootstrap" $fullname $listenerTag $name) "namespace" $namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict "parentRefs" (coalesce nil) "rules" (coalesce nil)) (dict "parentRefs" $parentRefs "hostnames" (list $host) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "" "port" 0) (dict "name" $bootstrapSvcName "port" $port)))))))))) -}}
{{- $routes = (concat (default (list) $routes) (list $bootstrap)) -}}
{{- if (eq $hostTemplate "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $brokerHost := (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $i $podname)))) "r") -}}
{{- $brokerSvcName := (printf "gw-%s" $podname) -}}
{{- $route := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "parentRefs" (coalesce nil) "rules" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1alpha2" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-%d" $fullname $listenerTag $name $i) "namespace" $namespace "labels" $labels)) "spec" (mustMergeOverwrite (dict "parentRefs" (coalesce nil) "rules" (coalesce nil)) (dict "parentRefs" $parentRefs "hostnames" (list $brokerHost) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "" "port" 0) (dict "name" $brokerSvcName "port" $port)))))))))) -}}
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
{{- $pr := (mustMergeOverwrite (dict "name" "") (dict "name" $ref.name "group" $ref.group "kind" $ref.kind "namespace" $ref.namespace "sectionName" $ref.sectionName)) -}}
{{- $parentRefs = (concat (default (list) $parentRefs) (list $pr)) -}}
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

