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
{{- (dict "r" (get (fromJson (include "_redpanda.NewListeners" (dict "a" (list (list (mustMergeOverwrite (dict "Kind" "" "AppProtocol" (coalesce nil) "Listeners" (coalesce nil)) (dict "Kind" "admin" "AppProtocol" $admin.appProtocol "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list $state "admin" $admin "" $tls $pki true)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "AppProtocol" (coalesce nil) "Listeners" (coalesce nil)) (dict "Kind" "kafka" "AppProtocol" $kafka.appProtocol "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list $state "kafka" $kafka $kafkaAuth $tls $pki true)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "AppProtocol" (coalesce nil) "Listeners" (coalesce nil)) (dict "Kind" "http" "AppProtocol" $http.appProtocol "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list $state "http" $http $httpAuth $tls $pki $http.enabled)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "AppProtocol" (coalesce nil) "Listeners" (coalesce nil)) (dict "Kind" "schema" "AppProtocol" $schemaRegistry.appProtocol "Listeners" (get (fromJson (include "redpanda.resolveAPIListeners" (dict "a" (list $state "schema" $schemaRegistry "" $tls $pki $schemaRegistry.enabled)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "AppProtocol" (coalesce nil) "Listeners" (coalesce nil)) (dict "Kind" "rpc" "Listeners" (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil) "NodePort" (coalesce nil) "Gateway" (coalesce nil) "Exposed" false) (dict "Name" "internal" "Port" ($rpc.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $rpc.address "0.0.0.0")))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy "rpc"))))) "r") "TLS" (get (fromJson (include "redpanda.resolveInternalTLS" (dict "a" (list $rpc.tls $tls $pki)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy "rpc"))))) "r") "Exposed" true)))))))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.resolveAPIListeners" -}}
{{- $state := (index .a 0) -}}
{{- $kind := (index .a 1) -}}
{{- $listener := (index .a 2) -}}
{{- $defaultAuth := (index .a 3) -}}
{{- $tls := (index .a 4) -}}
{{- $pki := (index .a 5) -}}
{{- $serviceEnabled := (index .a 6) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listeners := (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil) "NodePort" (coalesce nil) "Gateway" (coalesce nil) "Exposed" false) (dict "Name" "internal" "Port" ($listener.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.address "0.0.0.0")))) "r") "AuthenticationMethod" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.authenticationMethod $defaultAuth)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy $kind))))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.InternalPortName" (dict "a" (list (deepCopy $kind))))) "r") "TLS" (get (fromJson (include "redpanda.resolveInternalTLS" (dict "a" (list $listener.tls $tls $pki)))) "r") "Exposed" $serviceEnabled))) -}}
{{- range $name, $external := $listener.external -}}
{{- if (not (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r")) -}}
{{- continue -}}
{{- end -}}
{{- $listeners = (concat (default (list) $listeners) (list (mustMergeOverwrite (dict "Name" "" "Port" 0 "Address" "" "AuthenticationMethod" "" "PortName" "" "ContainerPortName" "" "TLS" (coalesce nil) "PrefixTemplate" "" "AdvertisedPorts" (coalesce nil) "NodePort" (coalesce nil) "Gateway" (coalesce nil) "Exposed" false) (dict "Name" $name "Port" ($external.port | int) "Address" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.address "0.0.0.0")))) "r") "AuthenticationMethod" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.authenticationMethod $defaultAuth)))) "r") "PortName" (get (fromJson (include "_redpanda.APIKind.PortName" (dict "a" (list (deepCopy $kind) $name)))) "r") "ContainerPortName" (get (fromJson (include "_redpanda.APIKind.ContainerPortName" (dict "a" (list (deepCopy $kind) $name)))) "r") "TLS" (get (fromJson (include "redpanda.resolveExternalTLS" (dict "a" (list $external.tls $listener.tls $tls $pki)))) "r") "PrefixTemplate" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.prefixTemplate "")))) "r") "AdvertisedPorts" $external.advertisedPorts "NodePort" $external.nodePort "Gateway" (get (fromJson (include "redpanda.resolveGateway" (dict "a" (list $state $external)))) "r") "Exposed" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.enabled $state.Values.external.enabled)))) "r"))))) -}}
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

