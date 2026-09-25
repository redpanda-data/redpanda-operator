{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/certs.go" */ -}}

{{- define "redpanda.resolvePKI" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $service := (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") -}}
{{- $ns := $state.Release.Namespace -}}
{{- $domain := (trimSuffix "." $state.Values.clusterDomain) -}}
{{- $externalDomain := "" -}}
{{- if (ne (toJson $state.Values.external.domain) "null") -}}
{{- $externalDomain = (tpl $state.Values.external.domain $state.Dot) -}}
{{- end -}}
{{- $certs := (dict) -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseServerCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $data := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- $ca := (coalesce nil) -}}
{{- if $data.caEnabled -}}
{{- $ca = "ca.crt" -}}
{{- end -}}
{{- $request := (coalesce nil) -}}
{{- if (eq (toJson $data.secretRef) "null") -}}
{{- $request = (mustMergeOverwrite (dict "objectName" "" "issuerRef" (dict "name" "")) (dict "objectName" (printf "%s-%s-cert" $fullname $name) "dnsNames" (get (fromJson (include "redpanda.serverSANs" (dict "a" (list $state $name $fullname $service $ns $domain $externalDomain)))) "r") "duration" (get (fromJson (include "_shims.time_Duration_String" (dict "a" (list (get (fromJson (include "_shims.time_ParseDuration" (dict "a" (list (default "43800h" $data.duration))))) "r"))))) "r") "issuerRef" (get (fromJson (include "redpanda.certIssuerRef" (dict "a" (list $fullname $name $data)))) "r"))) -}}
{{- end -}}
{{- $_ := (set $certs $name (mustMergeOverwrite (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (dict "Server" (mustMergeOverwrite (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) (dict "Name" $name "CA" $ca "Secret" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.TLSCert.ServerSecretName" (dict "a" (list $data $state $name)))) "r"))) "Request" $request))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseClientCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $data := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- if (and (ne (toJson $data.secretRef) "null") (eq (toJson $data.clientSecretRef) "null")) -}}
{{- $_ := (fail (printf ".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q" $name)) -}}
{{- end -}}
{{- $request := (coalesce nil) -}}
{{- if (eq (toJson $data.clientSecretRef) "null") -}}
{{- $request = (mustMergeOverwrite (dict "objectName" "" "issuerRef" (dict "name" "")) (dict "objectName" (printf "%s-%s-client" $fullname $name) "commonName" (printf "%s--%s-client" $fullname $name) "duration" (get (fromJson (include "_shims.time_Duration_String" (dict "a" (list (get (fromJson (include "_shims.time_ParseDuration" (dict "a" (list (default "43800h" $data.duration))))) "r"))))) "r") "issuerRef" (get (fromJson (include "redpanda.certIssuerRef" (dict "a" (list $fullname $name $data)))) "r"))) -}}
{{- end -}}
{{- $cert := (ternary (index $certs $name) (dict "Server" (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) "Client" (coalesce nil)) (hasKey $certs $name)) -}}
{{- $_ := (set $cert "Client" (mustMergeOverwrite (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) (dict "Name" (printf "%s-client" $name) "Secret" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.TLSCert.ClientSecretName" (dict "a" (list $data $state $name)))) "r"))) "Request" $request))) -}}
{{- $_ := (set $certs $name $cert) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Namespace" "" "Labels" (coalesce nil) "Annotations" (coalesce nil) "Certificates" (coalesce nil)) (dict "Namespace" $ns "Labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "Annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r") "Certificates" $certs))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.serverSANs" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $fullname := (index .a 2) -}}
{{- $service := (index .a 3) -}}
{{- $ns := (index .a 4) -}}
{{- $domain := (index .a 5) -}}
{{- $externalDomain := (index .a 6) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $names := (coalesce nil) -}}
{{- $data := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- if (or (eq (toJson $data.issuerRef) "null") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $data.applyInternalDNSNames false)))) "r")) -}}
{{- $names = (concat (default (list) $names) (default (list) (get (fromJson (include "_redpanda.ServiceSANs" (dict "a" (list $fullname $service $ns $domain)))) "r"))) -}}
{{- end -}}
{{- $names = (concat (default (list) $names) (default (list) (get (fromJson (include "_redpanda.DomainSANs" (dict "a" (list $externalDomain)))) "r"))) -}}
{{- $names = (concat (default (list) $names) (default (list) (get (fromJson (include "redpanda.gatewayServerCertDNSNames" (dict "a" (list $state $name)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.certIssuerRef" -}}
{{- $fullname := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $data := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $data.issuerRef (mustMergeOverwrite (dict "name" "") (dict "kind" "Issuer" "group" "cert-manager.io" "name" (printf "%s-%s-root-issuer" $fullname $name))))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.gatewayServerCertDNSNames" -}}
{{- $state := (index .a 0) -}}
{{- $certName := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $names := (coalesce nil) -}}
{{- range $_, $entry := (get (fromJson (include "redpanda.gatewayListenerConfigs" (dict "a" (list $state)))) "r") -}}
{{- $listener := $entry.Listeners -}}
{{- range $_, $external := $listener.external -}}
{{- if (or (not (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r")) (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $external.enabled $state.Values.external.enabled)))) "r"))) -}}
{{- continue -}}
{{- end -}}
{{- if (or (not (get (fromJson (include "redpanda.ExternalTLS.IsEnabled" (dict "a" (list $external.tls $listener.tls $state.Values.tls)))) "r")) (ne (get (fromJson (include "redpanda.ExternalTLS.GetCertName" (dict "a" (list $external.tls $listener.tls)))) "r") $certName)) -}}
{{- continue -}}
{{- end -}}
{{- $gateway := (get (fromJson (include "redpanda.resolveGateway" (dict "a" (list $state $external)))) "r") -}}
{{- if (eq (toJson $gateway) "null") -}}
{{- continue -}}
{{- end -}}
{{- if (ne $gateway.Host "") -}}
{{- $names = (concat (default (list) $names) (list $gateway.Host)) -}}
{{- end -}}
{{- $names = (concat (default (list) $names) (default (list) $gateway.BrokerHosts)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.gatewayListenerConfigs" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) (dict "Kind" "kafka" "Listeners" (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.kafka)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) (dict "Kind" "http" "Listeners" (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.http)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) (dict "Kind" "admin" "Listeners" (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.admin)))) "r"))) (mustMergeOverwrite (dict "Kind" "" "Listeners" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) (dict "Kind" "schema" "Listeners" (get (fromJson (include "redpanda.ListenerConfig.AsString" (dict "a" (list $state.Values.listeners.schemaRegistry)))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

