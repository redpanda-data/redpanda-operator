{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/tlsroute.go" */ -}}

{{- define "redpanda.tlsRouteListeners" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $defaults := (coalesce nil) -}}
{{- if (ne (toJson $state.Values.external.gateway) "null") -}}
{{- $defaults = $state.Values.external.gateway.parentRefs -}}
{{- end -}}
{{- $out := (coalesce nil) -}}
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "Host" "" "HostTemplate" "" "ParentRefs" (coalesce nil)) (dict "Tag" "kafka" "Name" $name "Port" ($l.port | int) "Host" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") "HostTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "Host" "" "HostTemplate" "" "ParentRefs" (coalesce nil)) (dict "Tag" "http" "Name" $name "Port" ($l.port | int) "Host" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") "HostTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "Host" "" "HostTemplate" "" "ParentRefs" (coalesce nil)) (dict "Tag" "admin" "Name" $name "Port" ($l.port | int) "Host" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") "HostTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- if (and (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsTLSRouteListener" (dict "a" (list $l)))) "r")) -}}
{{- $out = (concat (default (list) $out) (list (mustMergeOverwrite (dict "Tag" "" "Name" "" "Port" 0 "Host" "" "HostTemplate" "" "ParentRefs" (coalesce nil)) (dict "Tag" "schema" "Name" $name "Port" ($l.port | int) "Host" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") "HostTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") "ParentRefs" (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaults)))) "r"))))) -}}
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

{{- define "redpanda.tlsRouteHosts" -}}
{{- $state := (index .a 0) -}}
{{- $pods := (index .a 1) -}}
{{- $l := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $section := (printf "%s-%s-bootstrap" $l.Tag $l.Name) -}}
{{- $hosts := (list (mustMergeOverwrite (dict "Name" "" "Section" "" "Hostname" "" "Backend" "") (dict "Name" (printf "%s-%s" $fullname $section) "Section" $section "Hostname" $l.Host "Backend" (printf "%s-gateway-bootstrap" $fullname)))) -}}
{{- if (eq $l.HostTemplate "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $hosts) | toJson -}}
{{- break -}}
{{- end -}}
{{- range $i, $podname := $pods -}}
{{- $section := (printf "%s-%s-%d" $l.Tag $l.Name $i) -}}
{{- $hosts = (concat (default (list) $hosts) (list (mustMergeOverwrite (dict "Name" "" "Section" "" "Hostname" "" "Backend" "") (dict "Name" (printf "%s-%s" $fullname $section) "Section" $section "Hostname" (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $l.HostTemplate $i $podname)))) "r") "Backend" (get (fromJson (include "redpanda.gatewayBrokerServiceName" (dict "a" (list $podname)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $hosts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

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
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $routes := (coalesce nil) -}}
{{- range $_, $l := (get (fromJson (include "redpanda.tlsRouteListeners" (dict "a" (list $state)))) "r") -}}
{{- range $_, $h := (get (fromJson (include "redpanda.tlsRouteHosts" (dict "a" (list $state $pods $l)))) "r") -}}
{{- $parentRefs := $l.ParentRefs -}}
{{- if (get (fromJson (include "redpanda.GatewayConfig.IsListenerSetEnabled" (dict "a" (list $gw)))) "r") -}}
{{- $lsRefs := (get (fromJson (include "redpanda.listenerSetParentRefs" (dict "a" (list $state $l.ParentRefs $h.Section)))) "r") -}}
{{- if (get (fromJson (include "redpanda.GatewayConfig.AttachesRoutesToGateway" (dict "a" (list $gw)))) "r") -}}
{{- $parentRefs = (concat (default (list) (concat (default (list) (list)) (default (list) $l.ParentRefs))) (default (list) $lsRefs)) -}}
{{- else -}}
{{- $parentRefs = $lsRefs -}}
{{- end -}}
{{- end -}}
{{- $routes = (concat (default (list) $routes) (list (get (fromJson (include "redpanda.tlsRoute" (dict "a" (list $state $h $parentRefs ($l.Port | int))))) "r"))) -}}
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

{{- define "redpanda.tlsRoute" -}}
{{- $state := (index .a 0) -}}
{{- $h := (index .a 1) -}}
{{- $parentRefs := (index .a 2) -}}
{{- $port := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "parents" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "gateway.networking.k8s.io/v1" "kind" "TLSRoute")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $h.Name "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "parentRefs" $parentRefs)) (dict "hostnames" (list (toString $h.Hostname)) "rules" (list (mustMergeOverwrite (dict) (dict "backendRefs" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict "name" "") (dict "name" (toString $h.Backend) "port" ($port | int))) (dict))))))))))) | toJson -}}
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
{{- $maxPorts := ((64 | int) | int) -}}
{{- if (ne (toJson $state.Values.external.gateway) "null") -}}
{{- $defaultRefs = $state.Values.external.gateway.parentRefs -}}
{{- $maxPorts = ((get (fromJson (include "redpanda.GatewayConfig.GatewayMaxPorts" (dict "a" (list $state.Values.external.gateway)))) "r") | int) -}}
{{- end -}}
{{- if (or (lt $maxPorts (1 | int)) (gt $maxPorts (64 | int))) -}}
{{- $_ := (fail (printf "external.gateway.maxPorts must be between 1 and %d, got %d" (64 | int) $maxPorts)) -}}
{{- end -}}
{{- $claimed := (dict) -}}
{{- range $name, $l := $state.Values.listeners.kafka.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "kafka" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas true)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed $maxPorts (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "kafka" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas true)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.http.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "http" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed $maxPorts (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "http" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.admin.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "admin" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed $maxPorts (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "admin" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $name, $l := $state.Values.listeners.schemaRegistry.external -}}
{{- $enabled := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.enabled $state.Values.external.enabled)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListener" (dict "a" (list "schema" $name (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.type "")))) "r") $enabled $gatewayConfigured (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.hostTemplate "")))) "r") $replicas false)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateTCPRouteListener" (dict "a" (list $claimed $maxPorts (get (fromJson (include "redpanda.ExternalListener.GatewayParentRefs" (dict "a" (list $l $defaultRefs)))) "r") $state.Release.Namespace "schema" $name (and $enabled (get (fromJson (include "redpanda.ExternalListener.IsTCPRouteListener" (dict "a" (list $l)))) "r")) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.networkPort (0 | int))))) "r") | int) ((get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $l.brokerNetworkPortBase (0 | int))))) "r") | int) $replicas false)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if (and $gatewayConfigured (get (fromJson (include "redpanda.GatewayConfig.IsListenerSetEnabled" (dict "a" (list $state.Values.external.gateway)))) "r")) -}}
{{- $_ := (get (fromJson (include "redpanda.validateTLSRouteListenerSet" (dict "a" (list $state $claimed $maxPorts $pods)))) "r") -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.validateTLSRouteListenerSet" -}}
{{- $state := (index .a 0) -}}
{{- $claimed := (index .a 1) -}}
{{- $maxPorts := (index .a 2) -}}
{{- $pods := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tlsPort := ((get (fromJson (include "redpanda.GatewayConfig.GatewayAdvertisedPort" (dict "a" (list $state.Values.external.gateway)))) "r") | int) -}}
{{- $hosts := (dict) -}}
{{- range $_, $l := (get (fromJson (include "redpanda.tlsRouteListeners" (dict "a" (list $state)))) "r") -}}
{{- range $_, $ref := $l.ParentRefs -}}
{{- $gw := (get (fromJson (include "redpanda.gatewayKey" (dict "a" (list $ref $state.Release.Namespace)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.claimTLSPort" (dict "a" (list $claimed $maxPorts $gw $l.Tag $l.Name $tlsPort)))) "r") -}}
{{- if (not (hasKey $hosts $gw)) -}}
{{- $_ := (set $hosts $gw (dict)) -}}
{{- end -}}
{{- $gwHosts := (index $hosts $gw) -}}
{{- range $_, $h := (get (fromJson (include "redpanda.tlsRouteHosts" (dict "a" (list $state $pods $l)))) "r") -}}
{{- if (hasKey $gwHosts $h.Hostname) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: hostname %s is already used by %s on %s; each ListenerSet TLS entry needs its own hostname" $l.Tag $l.Name $h.Hostname (ternary (index $gwHosts $h.Hostname) "" (hasKey $gwHosts $h.Hostname)) $gw)) -}}
{{- end -}}
{{- $_ := (set $gwHosts $h.Hostname (printf "%s/%s" $l.Tag $l.Name)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $gw, $ports := $claimed -}}
{{- $entries := ((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) -}}
{{- if (hasKey $hosts $gw) -}}
{{- $entries = ((add ((sub $entries (1 | int)) | int) ((get (fromJson (include "_shims.len" (dict "a" (list (index $hosts $gw))))) "r") | int)) | int) -}}
{{- end -}}
{{- if (gt $entries (64 | int)) -}}
{{- $_ := (fail (printf "external.gateway.listenerSet: the ListenerSet for %s would need %d entries, more than the %d a ListenerSet allows; move listeners to another Gateway with per-listener parentRefs" $gw $entries (64 | int))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.gatewayKey" -}}
{{- $ref := (index .a 0) -}}
{{- $namespace := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s %s/%s" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $ref.kind (toString "Gateway"))))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $ref.namespace (toString $namespace))))) "r") $ref.name)) | toJson -}}
{{- break -}}
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
{{- $maxPorts := (index .a 1) -}}
{{- $parentRefs := (index .a 2) -}}
{{- $namespace := (index .a 3) -}}
{{- $tag := (index .a 4) -}}
{{- $name := (index .a 5) -}}
{{- $active := (index .a 6) -}}
{{- $networkPort := (index .a 7) -}}
{{- $base := (index .a 8) -}}
{{- $replicas := (index .a 9) -}}
{{- $requirePerBroker := (index .a 10) -}}
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
{{- $gw := (get (fromJson (include "redpanda.gatewayKey" (dict "a" (list $ref $namespace)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.claimNetworkPort" (dict "a" (list $claimed $maxPorts $gw $tag $name "bootstrap" $networkPort)))) "r") -}}
{{- if (eq $base (0 | int)) -}}
{{- continue -}}
{{- end -}}
{{- range $_, $i := (until $replicas) -}}
{{- $_ := (get (fromJson (include "redpanda.claimNetworkPort" (dict "a" (list $claimed $maxPorts $gw $tag $name (printf "broker %d" $i) ((add $base ($i | int)) | int))))) "r") -}}
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

{{- define "redpanda.claimTLSPort" -}}
{{- $claimed := (index .a 0) -}}
{{- $maxPorts := (index .a 1) -}}
{{- $gw := (index .a 2) -}}
{{- $tag := (index .a 3) -}}
{{- $name := (index .a 4) -}}
{{- $port := (index .a 5) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (hasKey $claimed $gw)) -}}
{{- $_ := (set $claimed $gw (dict)) -}}
{{- end -}}
{{- $ports := (index $claimed $gw) -}}
{{- $key := (printf "%d" $port) -}}
{{- if (hasKey $ports $key) -}}
{{- if (not (hasPrefix "tlsroute " (ternary (index $ports $key) "" (hasKey $ports $key)))) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: the Gateway TLS port %d (external.gateway.advertisedPort) is already used by %s; every TCPRoute needs its own Gateway listener port" $tag $name $port (ternary (index $ports $key) "" (hasKey $ports $key)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $ports $key (printf "tlsroute %s/%s" $tag $name)) -}}
{{- if (gt (((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) | int) $maxPorts) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: the TLS port needs more than %d ports on %s (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs" $tag $name $maxPorts $gw)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.claimNetworkPort" -}}
{{- $claimed := (index .a 0) -}}
{{- $maxPorts := (index .a 1) -}}
{{- $gw := (index .a 2) -}}
{{- $tag := (index .a 3) -}}
{{- $name := (index .a 4) -}}
{{- $what := (index .a 5) -}}
{{- $port := (index .a 6) -}}
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
{{- if (gt (((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) | int) $maxPorts) -}}
{{- $_ := (fail (printf "external gateway listener %s/%s: %s needs more than %d TCPRoute ports on %s (external.gateway.maxPorts); move listeners to another Gateway with per-listener parentRefs" $tag $name $what $maxPorts $gw)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

