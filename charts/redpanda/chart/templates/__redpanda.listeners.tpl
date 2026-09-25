{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/listeners.go" */ -}}

{{- define "_redpanda.APIKind.ConfigKey" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $k "admin") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "admin") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $k "http") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "pandaproxy_api") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $k "schema") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "schema_registry_api") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $k "rpc") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "rpc_server") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $k "kafka") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "kafka_api") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.TLSConfigKey" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $k "admin") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "admin_api_tls") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s_tls" (get (fromJson (include "_redpanda.APIKind.ConfigKey" (dict "a" (list (deepCopy $k))))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.ConfigSection" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $k "http") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "pandaproxy") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $k "schema") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "schema_registry") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "redpanda") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.AdvertisedConfigKey" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne $k "kafka") (ne $k "http")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "advertised_%s" (get (fromJson (include "_redpanda.APIKind.ConfigKey" (dict "a" (list (deepCopy $k))))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.AdvertisedConfigSection" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (get (fromJson (include "_redpanda.APIKind.AdvertisedConfigKey" (dict "a" (list (deepCopy $k))))) "r") "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.APIKind.ConfigSection" (dict "a" (list (deepCopy $k))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.InternalPortName" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $k "schema") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "schemaregistry") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (toString $k)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.PortName" -}}
{{- $k := (index .a 0) -}}
{{- $listener := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s-%s" $k $listener)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.APIKind.ContainerPortName" -}}
{{- $k := (index .a 0) -}}
{{- $listener := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s-%.8s" $k (lower $listener))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.NewListeners" -}}
{{- $apis := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $byKind := (dict) -}}
{{- range $_, $api := $apis -}}
{{- $_ := (set $byKind $api.Kind $api) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "ByKind" (coalesce nil)) (dict "ByKind" $byKind))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.Admin" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.ByKind "admin")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.Kafka" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.ByKind "kafka")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.HTTP" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.ByKind "http")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.SchemaRegistry" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.ByKind "schema")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.RPC" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.ByKind "rpc")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.APIs" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Listeners.inOrder" (dict "a" (list $l (list "admin" "kafka" "http" "schema"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.All" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Listeners.inOrder" (dict "a" (list $l (list "kafka" "admin" "http" "schema" "rpc"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.Ports" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Listeners.inOrder" (dict "a" (list $l (list "admin" "http" "kafka" "rpc" "schema"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.Gateways" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Listeners.inOrder" (dict "a" (list $l (list "kafka" "http" "admin" "schema"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.inOrder" -}}
{{- $l := (index .a 0) -}}
{{- $kinds := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $apis := (coalesce nil) -}}
{{- range $_, $kind := $kinds -}}
{{- $_188_api_1_ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $l.ByKind $kind (coalesce nil))))) "r") -}}
{{- $api_1 := (index $_188_api_1_ok_2 0) -}}
{{- $ok_2 := (index $_188_api_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $apis = (concat (default (list) $apis) (list $api_1)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $apis) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.ConfigSections" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $sections := (dict "redpanda" (dict) "pandaproxy" (dict) "schema_registry" (dict)) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.APIs" (dict "a" (list $l)))) "r") -}}
{{- $_ := (get (fromJson (include "_redpanda.addEntries" (dict "a" (list (index $sections (get (fromJson (include "_redpanda.APIKind.ConfigSection" (dict "a" (list (deepCopy $api.Kind))))) "r")) (get (fromJson (include "_redpanda.API.configEntries" (dict "a" (list $api)))) "r"))))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (get (fromJson (include "_redpanda.addEntries" (dict "a" (list (index $sections (get (fromJson (include "_redpanda.APIKind.ConfigSection" (dict "a" (list (deepCopy "rpc"))))) "r")) (get (fromJson (include "_redpanda.Listeners.rpcConfigEntries" (dict "a" (list $l)))) "r"))))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $sections) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.addEntries" -}}
{{- $section := (index .a 0) -}}
{{- $entries := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- range $_, $key := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $entries))))) "r") -}}
{{- $_ := (set $section $key (index $entries $key)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.rpcConfigEntries" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listener := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.RPC" (dict "a" (list $l)))) "r"))))) "r") -}}
{{- $entries := (dict (get (fromJson (include "_redpanda.APIKind.ConfigKey" (dict "a" (list (deepCopy "rpc"))))) "r") (dict "address" $listener.Address "port" ($listener.Port | int))) -}}
{{- $tls_3 := $listener.TLS -}}
{{- if (ne (toJson $tls_3) "null") -}}
{{- $_ := (set $entries (get (fromJson (include "_redpanda.APIKind.TLSConfigKey" (dict "a" (list (deepCopy "rpc"))))) "r") (get (fromJson (include "_redpanda.ListenerTLS.configEntry" (dict "a" (list $tls_3)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $entries) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.ContainerPorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.Ports" (dict "a" (list $l)))) "r") -}}
{{- range $_, $listener := $api.Listeners -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" $listener.ContainerPortName "containerPort" ($listener.Port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.InternalServicePorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.Ports" (dict "a" (list $l)))) "r") -}}
{{- $listener := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $api)))) "r") -}}
{{- if (not $listener.Exposed) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $api.AppProtocol "port" ($listener.Port | int) "targetPort" ($listener.Port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.NodePortServicePorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.APIs" (dict "a" (list $l)))) "r") -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $api)))) "r") -}}
{{- if (or (not $listener.Exposed) (ne (toJson $listener.Gateway) "null")) -}}
{{- continue -}}
{{- end -}}
{{- $nodePort := ($listener.Port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (0 | int)) -}}
{{- $nodePort = (index $listener.AdvertisedPorts (0 | int)) -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $api.AppProtocol "port" ($listener.Port | int) "targetPort" ($listener.Port | int) "nodePort" $nodePort)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.LoadBalancerServicePorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.APIs" (dict "a" (list $l)))) "r") -}}
{{- $inCluster := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $api)))) "r") -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $api)))) "r") -}}
{{- if (or (not $listener.Exposed) (ne (toJson $listener.Gateway) "null")) -}}
{{- continue -}}
{{- end -}}
{{- $port := ($inCluster.Port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (0 | int)) -}}
{{- $port = (index $listener.AdvertisedPorts (0 | int)) -}}
{{- end -}}
{{- if (ne (toJson $listener.NodePort) "null") -}}
{{- $port = $listener.NodePort -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $api.AppProtocol "port" $port "targetPort" ($listener.Port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.ExternalServicePorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.APIs" (dict "a" (list $l)))) "r") -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $api)))) "r") -}}
{{- if (or (not $listener.Exposed) (ne (toJson $listener.Gateway) "null")) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $api.AppProtocol "port" ($listener.Port | int) "targetPort" ($listener.Port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.GatewayServicePorts" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.APIs" (dict "a" (list $l)))) "r") -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $api)))) "r") -}}
{{- if (or (not $listener.Exposed) (eq (toJson $listener.Gateway) "null")) -}}
{{- continue -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $api.AppProtocol "port" ($listener.Port | int) "targetPort" ($listener.Port | int))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.TrustStores" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $stores := (coalesce nil) -}}
{{- range $_, $api := (get (fromJson (include "_redpanda.Listeners.All" (dict "a" (list $l)))) "r") -}}
{{- range $_, $listener := $api.Listeners -}}
{{- if (eq (toJson $listener.TLS) "null") -}}
{{- continue -}}
{{- end -}}
{{- if (eq (toJson $listener.TLS.TrustStore) "null") -}}
{{- continue -}}
{{- end -}}
{{- $stores = (concat (default (list) $stores) (list $listener.TLS.TrustStore)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $stores) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.TrustStoreVolume" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $cmSources := (dict) -}}
{{- $secretSources := (dict) -}}
{{- range $_, $store := (get (fromJson (include "_redpanda.Listeners.TrustStores" (dict "a" (list $l)))) "r") -}}
{{- $projection := (get (fromJson (include "_redpanda.TrustStore.VolumeProjection" (dict "a" (list $store)))) "r") -}}
{{- if (ne (toJson $projection.secret) "null") -}}
{{- $_ := (set $secretSources $projection.secret.name (concat (default (list) (index $secretSources $projection.secret.name)) (default (list) $projection.secret.items))) -}}
{{- else -}}
{{- $_ := (set $cmSources $projection.configMap.name (concat (default (list) (index $cmSources $projection.configMap.name)) (default (list) $projection.configMap.items))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sources := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $cmSources))))) "r") -}}
{{- $sources = (concat (default (list) $sources) (list (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $name)) (dict "items" (get (fromJson (include "_redpanda.dedupKeyToPaths" (dict "a" (list (index $cmSources $name))))) "r"))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $secretSources))))) "r") -}}
{{- $sources = (concat (default (list) $sources) (list (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $name)) (dict "items" (get (fromJson (include "_redpanda.dedupKeyToPaths" (dict "a" (list (index $secretSources $name))))) "r"))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if (lt ((get (fromJson (include "_shims.len" (dict "a" (list $sources)))) "r") | int) (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "projected" (mustMergeOverwrite (dict "sources" (coalesce nil)) (dict "sources" $sources)))) (dict "name" "truststores"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listeners.TrustStoreMount" -}}
{{- $l := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (lt ((get (fromJson (include "_shims.len" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.TrustStores" (dict "a" (list $l)))) "r"))))) "r") | int) (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "truststores" "mountPath" "/etc/truststores" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.InCluster" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- range $_, $listener := $a.Listeners -}}
{{- if (eq $listener.Name "internal") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $listener) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.External" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $external := (coalesce nil) -}}
{{- range $_, $listener := $a.Listeners -}}
{{- if (eq $listener.Name "internal") -}}
{{- continue -}}
{{- end -}}
{{- $external = (concat (default (list) $external) (list $listener)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $external) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.RPKClientTLS" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listener := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $a)))) "r") -}}
{{- $tls := $listener.TLS -}}
{{- if (eq (toJson $tls) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (dict "ca_file" (get (fromJson (include "_redpanda.ListenerTLS.ServerCAFile" (dict "a" (list $tls)))) "r")) -}}
{{- $kp_4 := $tls.Client -}}
{{- if (ne (toJson $kp_4) "null") -}}
{{- $_ := (set $cfg "cert_file" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $kp_4)))) "r")) -}}
{{- $_ := (set $cfg "key_file" (get (fromJson (include "_redpanda.Keypair.KeyFile" (dict "a" (list $kp_4)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.BrokerClientTLS" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listener := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $a)))) "r") -}}
{{- $tls := $listener.TLS -}}
{{- if (eq (toJson $tls) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (dict "enabled" true "require_client_auth" $tls.RequireClientAuth "truststore_file" (get (fromJson (include "_redpanda.ListenerTLS.ServerCAFile" (dict "a" (list $tls)))) "r")) -}}
{{- $kp_5 := $tls.Client -}}
{{- if (ne (toJson $kp_5) "null") -}}
{{- $_ := (set $cfg "cert_file" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $kp_5)))) "r")) -}}
{{- $_ := (set $cfg "key_file" (get (fromJson (include "_redpanda.Keypair.KeyFile" (dict "a" (list $kp_5)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.CurlFlags" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listener := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $a)))) "r") -}}
{{- $tls := $listener.TLS -}}
{{- if (eq (toJson $tls) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- $kp_6 := $tls.Client -}}
{{- if (ne (toJson $kp_6) "null") -}}
{{- $path := (get (fromJson (include "_redpanda.Keypair.MountPath" (dict "a" (list $kp_6)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key" $path $path $path)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s" (get (fromJson (include "_redpanda.ListenerTLS.ServerCAFile" (dict "a" (list $tls)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.ProfileAdvertisedPort" -}}
{{- $a := (index .a 0) -}}
{{- $replica := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $inCluster := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list $a)))) "r") -}}
{{- $port := ($inCluster.Port | int) -}}
{{- $external := (get (fromJson (include "_redpanda.API.External" (dict "a" (list $a)))) "r") -}}
{{- if (lt ((get (fromJson (include "_shims.len" (dict "a" (list $external)))) "r") | int) (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $port) | toJson -}}
{{- break -}}
{{- end -}}
{{- $listener := (index $external (0 | int)) -}}
{{- if (gt ($listener.Port | int) (1 | int)) -}}
{{- $port = ($listener.Port | int) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $listener.AdvertisedPorts $replica) -}}
{{- else -}}{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $listener.AdvertisedPorts (0 | int)) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $port) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.API.configEntries" -}}
{{- $a := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listeners := (coalesce nil) -}}
{{- $tlsEntries := (coalesce nil) -}}
{{- range $_, $listener := $a.Listeners -}}
{{- $entry := (dict "name" $listener.Name "address" $listener.Address "port" ($listener.Port | int)) -}}
{{- if (ne $listener.AuthenticationMethod "") -}}
{{- $_ := (set $entry "authentication_method" $listener.AuthenticationMethod) -}}
{{- end -}}
{{- $listeners = (concat (default (list) $listeners) (list $entry)) -}}
{{- if (ne (toJson $listener.TLS) "null") -}}
{{- $tlsEntry := (get (fromJson (include "_redpanda.ListenerTLS.configEntry" (dict "a" (list $listener.TLS)))) "r") -}}
{{- $_ := (set $tlsEntry "name" $listener.Name) -}}
{{- $tlsEntries = (concat (default (list) $tlsEntries) (list $tlsEntry)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $entries := (dict (get (fromJson (include "_redpanda.APIKind.ConfigKey" (dict "a" (list (deepCopy $a.Kind))))) "r") $listeners) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tlsEntries)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $entries (get (fromJson (include "_redpanda.APIKind.TLSConfigKey" (dict "a" (list (deepCopy $a.Kind))))) "r") $tlsEntries) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $entries) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Listener.AdvertisedPort" -}}
{{- $l := (index .a 0) -}}
{{- $replica := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $l.AdvertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.AdvertisedPorts (0 | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $l.AdvertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (index $l.AdvertisedPorts $replica)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" ($l.Port | int)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.NewListenerTLS" -}}
{{- $pki := (index .a 0) -}}
{{- $cert := (index .a 1) -}}
{{- $requireClientAuth := (index .a 2) -}}
{{- $trustStore := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $client := (coalesce nil) -}}
{{- if $requireClientAuth -}}
{{- $client = (get (fromJson (include "_redpanda.PKI.ClientKeypair" (dict "a" (list $pki $cert)))) "r") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil) "RequireClientAuth" false "TrustStore" (coalesce nil) "TrustStoreFallback" "") (dict "Server" (get (fromJson (include "_redpanda.PKI.ServerKeypair" (dict "a" (list $pki $cert)))) "r") "Client" $client "RequireClientAuth" $requireClientAuth "TrustStore" $trustStore))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ListenerTLS.TrustStoreFile" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne (toJson $t.TrustStore) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.TrustStore.AbsolutePath" (dict "a" (list $t.TrustStore)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne (toJson $t.Server.CA) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Keypair.CAFile" (dict "a" (list $t.Server)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne $t.TrustStoreFallback "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $t.TrustStoreFallback) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $t.Server)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ListenerTLS.ServerCAFile" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $file_7 := (get (fromJson (include "_redpanda.ListenerTLS.TrustStoreFile" (dict "a" (list $t)))) "r") -}}
{{- if (ne $file_7 $t.TrustStoreFallback) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $file_7) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $t.Server)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ListenerTLS.configEntry" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "enabled" true "cert_file" (get (fromJson (include "_redpanda.Keypair.CertFile" (dict "a" (list $t.Server)))) "r") "key_file" (get (fromJson (include "_redpanda.Keypair.KeyFile" (dict "a" (list $t.Server)))) "r") "require_client_auth" $t.RequireClientAuth "truststore_file" (get (fromJson (include "_redpanda.ListenerTLS.TrustStoreFile" (dict "a" (list $t)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.TrustStore.AbsolutePath" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" "/etc/truststores" (get (fromJson (include "_redpanda.TrustStore.RelativePath" (dict "a" (list $t)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.TrustStore.RelativePath" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne (toJson $t.configMapKeyRef) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "configmaps/%s-%s" $t.configMapKeyRef.name $t.configMapKeyRef.key)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "secrets/%s-%s" $t.secretKeyRef.name $t.secretKeyRef.key)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.TrustStore.VolumeProjection" -}}
{{- $t := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (ne (toJson $t.configMapKeyRef) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $t.configMapKeyRef.name)) (dict "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" $t.configMapKeyRef.key "path" (get (fromJson (include "_redpanda.TrustStore.RelativePath" (dict "a" (list $t)))) "r"))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $t.secretKeyRef.name)) (dict "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" $t.secretKeyRef.key "path" (get (fromJson (include "_redpanda.TrustStore.RelativePath" (dict "a" (list $t)))) "r"))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.dedupKeyToPaths" -}}
{{- $items := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $seen := (dict) -}}
{{- $deduped := (coalesce nil) -}}
{{- range $_, $item := $items -}}
{{- $_870___ok_8 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $seen $item.key false)))) "r") -}}
{{- $_ := (index $_870___ok_8 0) -}}
{{- $ok_8 := (index $_870___ok_8 1) -}}
{{- if $ok_8 -}}
{{- continue -}}
{{- end -}}
{{- $deduped = (concat (default (list) $deduped) (list $item)) -}}
{{- $_ := (set $seen $item.key true) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $deduped) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

