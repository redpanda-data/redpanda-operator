{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/listeners.go" */ -}}

{{- define "redpanda.resolveListeners" -}}
{{- $state := (index .a 0) -}}
{{- $pki := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tls := $state.Values.tls -}}
{{- $kafkaAuth := "" -}}
{{- $httpAuth := "" -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $kafkaAuth = (toString "sasl") -}}
{{- $httpAuth = (toString "http_basic") -}}
{{- end -}}
{{- $admin := (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.admin)))) "r") -}}
{{- $kafka := (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.kafka)))) "r") -}}
{{- $http := (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.http)))) "r") -}}
{{- $schemaRegistry := (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.schemaRegistry)))) "r") -}}
{{- $rpc := $state.Values.listeners.rpc -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.NewListeners" (dict "a" (list (list (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" "admin" "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list "admin" $admin "" $tls $pki)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" "kafka" "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list "kafka" $kafka $kafkaAuth $tls $pki)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" "http" "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list "http" $http $httpAuth $tls $pki)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" "schema" "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list "schema" $schemaRegistry "" $tls $pki)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" "rpc" "Listeners" (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "AppProtocol" (coalesce nil) "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil)) (dict "Name" "internal" "Port" ($rpc.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $rpc.address "0.0.0.0")))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy "rpc"))))) "r") "TLS" (get (fromJson (include "redpanda.resolveInternalTLS" (dict "a" (list $rpc.tls $tls $pki)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy "rpc"))))) "r"))))))))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveAPIListeners" -}}
{{- $kind := (index .a 0) -}}
{{- $listener := (index .a 1) -}}
{{- $defaultAuth := (index .a 2) -}}
{{- $tls := (index .a 3) -}}
{{- $pki := (index .a 4) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listeners := (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "AppProtocol" (coalesce nil) "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil)) (dict "Name" "internal" "Port" ($listener.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.address "0.0.0.0")))) "r") "AuthenticationMethod" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.authenticationMethod $defaultAuth)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy $kind))))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy $kind))))) "r") "AppProtocol" $listener.appProtocol "TLS" (get (fromJson (include "redpanda.resolveInternalTLS" (dict "a" (list $listener.tls $tls $pki)))) "r")))) -}}
{{- range $name, $external := $listener.external -}}
{{- if (not (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $listeners = (concat (default (list) $listeners) (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "AppProtocol" (coalesce nil) "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil)) (dict "Name" $name "Port" ($external.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.address "0.0.0.0")))) "r") "AuthenticationMethod" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.authenticationMethod $defaultAuth)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.PortName" (dict "a" (list (deepCopy $kind) $name)))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.ContainerPortName" (dict "a" (list (deepCopy $kind) $name)))) "r") "AppProtocol" $listener.appProtocol "TLS" (get (fromJson (include "redpanda.resolveExternalTLS" (dict "a" (list $external.tls $listener.tls $tls $pki)))) "r") "PrefixTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.prefixTemplate "")))) "r") "AdvertisedPorts" $external.advertisedPorts)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $listeners) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveInternalTLS" -}}
{{- $internal := (index .a 0) -}}
{{- $tls := (index .a 1) -}}
{{- $pki := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $internal $tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $resolved := (get (fromJson (include "_redpanda.NewListenerTLS" (dict "a" (list $pki $internal.cert $internal.requireClientAuth (get (fromJson (include "redpanda.resolveTrustStore" (dict "a" (list $internal.trustStore)))) "r"))))) "r") -}}
{{- $_ := (set $resolved "TrustStoreFallback" "/etc/ssl/certs/ca-certificates.crt") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $resolved) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveExternalTLS" -}}
{{- $external := (index .a 0) -}}
{{- $internal := (index .a 1) -}}
{{- $tls := (index .a 2) -}}
{{- $pki := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalTLS.IsEnabled" (dict "a" (list $external $internal $tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $resolved := (get (fromJson (include "_redpanda.NewListenerTLS" (dict "a" (list $pki (get (fromJson (include "redpanda.ExternalTLS.GetCertName" (dict "a" (list $external $internal)))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.requireClientAuth false)))) "r") (get (fromJson (include "redpanda.resolveTrustStore" (dict "a" (list $external.trustStore)))) "r"))))) "r") -}}
{{- $_ := (set $resolved "TrustStoreFallback" "/etc/ssl/certs/ca-certificates.crt") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $resolved) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveTrustStore" -}}
{{- $trustStore := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $trustStore) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "configMapKeyRef" (coalesce nil) "secretKeyRef" (coalesce nil)) (dict "configMapKeyRef" $trustStore.configMapKeyRef "secretKeyRef" $trustStore.secretKeyRef))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveGateway" -}}
{{- $state := (index .a 0) -}}
{{- $external := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $external)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $brokerHosts := (coalesce nil) -}}
{{- $template_1 := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.hostTemplate "")))) "r") -}}
{{- if (ne $template_1 "") -}}
{{- range $i, $podname := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $brokerHosts = (concat (default (list) $brokerHosts) (list (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $template_1 $i $podname)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Host" "" "BrokerHosts" (coalesce nil)) (dict "Host" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.host "")))) "r") "BrokerHosts" $brokerHosts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveNetwork" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $network := (mustMergeOverwrite (dict "Listeners" (dict "ByKind" (coalesce nil)) "Services" (coalesce nil) "Routes" (coalesce nil)) (dict "Listeners" $listeners "Routes" (get (fromJson (include "redpanda.resolveRoutes" (dict "a" (list $state)))) "r"))) -}}
{{- $internal := (get (fromJson (include "_redpanda.Listeners.InCluster" (dict "a" (list $listeners)))) "r") -}}
{{- if (not $state.Values.listeners.http.enabled) -}}
{{- $_ := (unset $internal.ByKind "http") -}}
{{- end -}}
{{- if (not $state.Values.listeners.schemaRegistry.enabled) -}}
{{- $_ := (unset $internal.ByKind "schema") -}}
{{- end -}}
{{- $external := (coalesce nil) -}}
{{- $gateway := (coalesce nil) -}}
{{- $allExternal := (get (fromJson (include "_redpanda.Listeners.External" (dict "a" (list $listeners)))) "r") -}}
{{- range $_, $kind := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $allExternal.ByKind))))) "r") -}}
{{- $publishedExternal := (coalesce nil) -}}
{{- $publishedGateway := (coalesce nil) -}}
{{- range $_, $listener := (index $allExternal.ByKind $kind).Listeners -}}
{{- if (ne (toJson (get (fromJson (include "_redpanda.Network.Route" (dict "a" (list $network $kind $listener.Name)))) "r")) "null") -}}
{{- $publishedGateway = (concat (default (list) $publishedGateway) (list $listener)) -}}
{{- else -}}
{{- $publishedExternal = (concat (default (list) $publishedExternal) (list $listener)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $publishedExternal)))) "r") | int) (0 | int)) -}}
{{- $external = (concat (default (list) $external) (list (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" $kind "Listeners" $publishedExternal)))) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $publishedGateway)))) "r") | int) (0 | int)) -}}
{{- $gateway = (concat (default (list) $gateway) (list (mustMergeOverwrite (dict "Kind" "" "Listeners" (coalesce nil)) (dict "Kind" $kind "Listeners" $publishedGateway)))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $network "Services" (concat (default (list) $network.Services) (list (get (fromJson (include "redpanda.internalServiceConfig" (dict "a" (list $state $internal)))) "r")))) -}}
{{- $serviceType := (get (fromJson (include "redpanda.externalServiceType" (dict "a" (list $state)))) "r") -}}
{{- if (eq $serviceType "NodePort") -}}
{{- $_ := (set $network "Services" (concat (default (list) $network.Services) (list (get (fromJson (include "redpanda.nodePortServiceConfig" (dict "a" (list $state (get (fromJson (include "_redpanda.NewListeners" (dict "a" (list $external)))) "r"))))) "r")))) -}}
{{- end -}}
{{- if (eq $serviceType "LoadBalancer") -}}
{{- $_ := (set $network "Services" (concat (default (list) $network.Services) (list (get (fromJson (include "redpanda.loadBalancerServiceConfig" (dict "a" (list $state (get (fromJson (include "_redpanda.NewListeners" (dict "a" (list $external)))) "r"))))) "r")))) -}}
{{- end -}}
{{- if (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") -}}
{{- $_ := (set $network "Services" (concat (default (list) $network.Services) (list (get (fromJson (include "redpanda.gatewayServiceConfig" (dict "a" (list $state (get (fromJson (include "_redpanda.NewListeners" (dict "a" (list $gateway)))) "r"))))) "r")))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $network) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.externalServiceType" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.external.enabled) (not $state.Values.external.service.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.Values.external.type) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveRoutes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $routes := (dict) -}}
{{- range $_, $entry := (get (fromJson (include "redpanda.gatewayListenerConfigs" (dict "a" (list $state)))) "r") -}}
{{- range $name, $external := $entry.Listeners.external -}}
{{- if (not (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $gateway := (get (fromJson (include "redpanda.resolveGateway" (dict "a" (list $state $external)))) "r") -}}
{{- if (eq (toJson $gateway) "null") -}}
{{- continue -}}
{{- end -}}
{{- $_266_byName_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $routes $entry.Kind (coalesce nil))))) "r") -}}
{{- $byName := (index $_266_byName_ok 0) -}}
{{- $ok := (index $_266_byName_ok 1) -}}
{{- if (not $ok) -}}
{{- $byName = (dict) -}}
{{- $_ := (set $routes $entry.Kind $byName) -}}
{{- end -}}
{{- $_ := (set $byName $name $gateway) -}}
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

