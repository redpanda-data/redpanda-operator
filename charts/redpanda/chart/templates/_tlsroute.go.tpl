{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/tlsroute.go" */ -}}

{{- define "redpanda.TLSRoutes" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
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
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.Gateways" (dict "a" (list $listeners)))) "r") -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $api)))) "r") -}}
{{- if (or (not $listener.Exposed) (eq (toJson $listener.Gateway) "null")) -}}
{{- continue -}}
{{- end -}}
{{- $routes = (concat (default (list) $routes) (default (list) (get (fromJson (include "redpanda.tlsRoutesForListener" (dict "a" (list $fullname $state.Release.Namespace $labels $annotations $gw.parentRefs $pods $api.Kind $listener)))) "r"))) -}}
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

{{- define "redpanda.tlsRoutesForListener" -}}
{{- $fullname := (index .a 0) -}}
{{- $namespace := (index .a 1) -}}
{{- $labels := (index .a 2) -}}
{{- $annotations := (index .a 3) -}}
{{- $parentRefs := (index .a 4) -}}
{{- $pods := (index .a 5) -}}
{{- $kind := (index .a 6) -}}
{{- $listener := (index .a 7) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $gateway := $listener.Gateway -}}
{{- $routes := (coalesce nil) -}}
{{- $bootstrapSvcName := (printf "%s-gateway-bootstrap" $fullname) -}}
{{- $bootstrap := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-bootstrap" $fullname $kind $listener.Name) "namespace" $namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" (list (toString $gateway.Host)) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $bootstrapSvcName) "port" (($listener.Port | int) | int))) (dict)))))))))) -}}
{{- $routes = (concat (default (list) $routes) (list $bootstrap)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $gateway.BrokerHosts)))) "r") | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $routes) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $brokerHost := (index $gateway.BrokerHosts $i) -}}
{{- $brokerSvcName := (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r") -}}
{{- $route := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-%s-%d" $fullname $kind $listener.Name $i) "namespace" $namespace "labels" $labels "annotations" $annotations)) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" (list (toString $brokerHost)) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $brokerSvcName) "port" (($listener.Port | int) | int))) (dict)))))))))) -}}
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
{{- $replicas := ((get (fromJson (include "_shims.len" (dict "a" (list (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r"))))) "r") | int) -}}
{{- $gatewayConfigured := (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") -}}
{{- range $_, $entry := (get (fromJson (include "redpanda.gatewayListenerConfigs" (dict "a" (list $state)))) "r") -}}
{{- $requirePerBroker := (eq $entry.Kind "kafka") -}}
{{- range $name, $external := $entry.Listeners.external -}}
{{- $exposed := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list $entry.Kind $name (get (fromJson (include "redpanda.resolveGateway" (dict "a" (list $state $external)))) "r") $exposed $gatewayConfigured $replicas $requirePerBroker)))) "r") -}}
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

{{- define "redpanda.validateGatewayListener" -}}
{{- $tag := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $gateway := (index .a 2) -}}
{{- $exposed := (index .a 3) -}}
{{- $gatewayConfigured := (index .a 4) -}}
{{- $replicas := (index .a 5) -}}
{{- $requirePerBroker := (index .a 6) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $exposed) (eq (toJson $gateway) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not $gatewayConfigured) -}}
{{- $_ := (fail (printf "external listener %s/%s sets type: tlsroute but external.gateway is not enabled with at least one parentRef; refusing to fall back to a NodePort/LoadBalancer Service. Set external.gateway.enabled: true and external.gateway.parentRefs" $tag $name)) -}}
{{- end -}}
{{- if (eq $gateway.Host "") -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `host` (the bootstrap SNI hostname) when type: tlsroute" $tag $name)) -}}
{{- end -}}
{{- if (and (and $requirePerBroker (gt $replicas (1 | int))) (eq ((get (fromJson (include "_shims.len" (dict "a" (list $gateway.BrokerHosts)))) "r") | int) (0 | int))) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s requires `hostTemplate` when replicas > 1: Kafka clients reconnect to individual brokers by SNI, so each broker needs its own per-broker hostname" $tag $name)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

