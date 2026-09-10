{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/certs.go" */ -}}

{{- define "redpanda.PKI" -}}
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
{{- $_ := (set $cert "Client" (mustMergeOverwrite (dict "Name" "" "Secret" (dict) "Request" (coalesce nil) "CA" (coalesce nil)) (dict "Name" (get (fromJson (include "_redpanda.ClientKeypairName" (dict "a" (list $name)))) "r") "Secret" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.TLSCert.ClientSecretName" (dict "a" (list $data $state $name)))) "r"))) "Request" $request))) -}}
{{- $_ := (set $certs $name $cert) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Namespace" "" "Labels" (coalesce nil) "Certificates" (coalesce nil)) (dict "Namespace" $ns "Labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "Certificates" $certs))) | toJson -}}
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
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $names := (coalesce nil) -}}
{{- range $_, $listener := $state.Values.listeners.kafka.external -}}
{{- $names = (get (fromJson (include "redpanda.appendGatewayCertHosts" (dict "a" (list $state $names $certName (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r") $listener.tls $state.Values.listeners.kafka.tls (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $pods)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $listener := $state.Values.listeners.http.external -}}
{{- $names = (get (fromJson (include "redpanda.appendGatewayCertHosts" (dict "a" (list $state $names $certName (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r") $listener.tls $state.Values.listeners.http.tls (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $pods)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $listener := $state.Values.listeners.admin.external -}}
{{- $names = (get (fromJson (include "redpanda.appendGatewayCertHosts" (dict "a" (list $state $names $certName (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r") $listener.tls $state.Values.listeners.admin.tls (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $pods)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $listener := $state.Values.listeners.schemaRegistry.external -}}
{{- $names = (get (fromJson (include "redpanda.appendGatewayCertHosts" (dict "a" (list $state $names $certName (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.enabled $state.Values.external.enabled)))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $listener)))) "r") $listener.tls $state.Values.listeners.schemaRegistry.tls (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $listener.hostTemplate "")))) "r") $pods)))) "r") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.appendGatewayCertHosts" -}}
{{- $state := (index .a 0) -}}
{{- $names := (index .a 1) -}}
{{- $certName := (index .a 2) -}}
{{- $enabled := (index .a 3) -}}
{{- $isGateway := (index .a 4) -}}
{{- $extTLS := (index .a 5) -}}
{{- $listenerTLS := (index .a 6) -}}
{{- $host := (index .a 7) -}}
{{- $hostTemplate := (index .a 8) -}}
{{- $pods := (index .a 9) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $enabled) (not $isGateway)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not (get (fromJson (include "redpanda.ExternalTLS.IsEnabled" (dict "a" (list $extTLS $listenerTLS $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne (get (fromJson (include "redpanda.ExternalTLS.GetCertName" (dict "a" (list $extTLS $listenerTLS)))) "r") $certName) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne $host "") -}}
{{- $names = (concat (default (list) $names) (list $host)) -}}
{{- end -}}
{{- if (ne $hostTemplate "") -}}
{{- range $i, $podname := $pods -}}
{{- $names = (concat (default (list) $names) (list (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $i $podname)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $names) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

