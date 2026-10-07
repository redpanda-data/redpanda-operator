{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/service.go" */ -}}

{{- define "_redpanda.Network.Service" -}}
{{- $n := (index .a 0) -}}
{{- $kind := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- range $_, $config := $n.Services -}}
{{- if (eq $config.Kind $kind) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $config) | toJson -}}
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

{{- define "_redpanda.Network.Render" -}}
{{- $n := (index .a 0) -}}
{{- $kind := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $services := (coalesce nil) -}}
{{- range $_, $config := $n.Services -}}
{{- if (eq $config.Kind $kind) -}}
{{- $services = (concat (default (list) $services) (default (list) (get (fromJson (include "_redpanda.ServiceConfig.Render" (dict "a" (list $config)))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $services) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.Network.Route" -}}
{{- $n := (index .a 0) -}}
{{- $kind := (index .a 1) -}}
{{- $name := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_57_byName_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $n.Routes $kind (coalesce nil))))) "r") -}}
{{- $byName := (index $_57_byName_ok 0) -}}
{{- $ok := (index $_57_byName_ok 1) -}}
{{- if (not $ok) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_61_route_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $byName $name (dict "Host" "" "BrokerHosts" (coalesce nil)))))) "r") -}}
{{- $route := (index $_61_route_ok 0) -}}
{{- $ok := (index $_61_route_ok 1) -}}
{{- if (not $ok) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $route) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceKind.order" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (eq $k "headless") (eq $k "broker")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "admin" "http" "kafka" "rpc" "schema")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "admin" "kafka" "http" "schema")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceKind.rendersTemplate" -}}
{{- $k := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (or (or (eq $k "headless") (eq $k "nodeport")) (eq $k "gateway"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceConfig.Render" -}}
{{- $c := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (get (fromJson (include "_redpanda.ServiceConfig.Ports" (dict "a" (list $c)))) "r") -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $ports)))) "r") | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $base := (deepCopy $c.Template) -}}
{{- $_ := (set $base.spec "ports" $ports) -}}
{{- $services := (coalesce nil) -}}
{{- if (get (fromJson (include "_redpanda.ServiceKind.rendersTemplate" (dict "a" (list (deepCopy $c.Kind))))) "r") -}}
{{- $services = (concat (default (list) $services) (list $base)) -}}
{{- end -}}
{{- range $_, $broker := $c.Brokers -}}
{{- $svc := (deepCopy $base) -}}
{{- $_ := (set $svc.metadata "name" $broker.Name) -}}
{{- if (and (gt ((get (fromJson (include "_shims.len" (dict "a" (list $broker.Annotations)))) "r") | int) (0 | int)) (eq (toJson $svc.metadata.annotations) "null")) -}}
{{- $_ := (set $svc.metadata "annotations" (dict)) -}}
{{- end -}}
{{- if (and (gt ((get (fromJson (include "_shims.len" (dict "a" (list $broker.Selector)))) "r") | int) (0 | int)) (eq (toJson $svc.spec.selector) "null")) -}}
{{- $_ := (set $svc.spec "selector" (dict)) -}}
{{- end -}}
{{- $_ := (get (fromJson (include "_shims.maps_Copy" (dict "a" (list $svc.metadata.annotations $broker.Annotations)))) "r") -}}
{{- $_ := (get (fromJson (include "_shims.maps_Copy" (dict "a" (list $svc.spec.selector $broker.Selector)))) "r") -}}
{{- $services = (concat (default (list) $services) (list $svc)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $services) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceConfig.Ports" -}}
{{- $c := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $ports := (coalesce nil) -}}
{{- range $_, $listener := (get (fromJson (include "_redpanda.Listeners.ListenersInOrder" (dict "a" (list $c.Listeners (get (fromJson (include "_redpanda.ServiceKind.order" (dict "a" (list (deepCopy $c.Kind))))) "r"))))) "r") -}}
{{- $port := (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" $listener.PortName "protocol" "TCP" "appProtocol" $listener.AppProtocol "port" ($listener.Port | int) "targetPort" ($listener.Port | int))) -}}
{{- if (eq $c.Kind "nodeport") -}}
{{- $_ := (set $port "nodePort" ($listener.Port | int)) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $port "nodePort" (index $listener.AdvertisedPorts (0 | int))) -}}
{{- end -}}
{{- else -}}{{- if (eq $c.Kind "loadbalancer") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.AdvertisedPorts)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $port "port" (index $listener.AdvertisedPorts (0 | int))) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $ports = (concat (default (list) $ports) (list $port)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $ports) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

