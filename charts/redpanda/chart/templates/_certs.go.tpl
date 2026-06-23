{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/certs.go" */ -}}

{{- define "redpanda.ClientCerts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $service := (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") -}}
{{- $ns := $state.Release.Namespace -}}
{{- $domain := (trimSuffix "." $state.Values.clusterDomain) -}}
{{- $certs := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseServerCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $data := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- if (not (empty $data.secretRef)) -}}
{{- continue -}}
{{- end -}}
{{- $names := (coalesce nil) -}}
{{- if (or (eq (toJson $data.issuerRef) "null") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $data.applyInternalDNSNames false)))) "r")) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s-cluster.%s.%s.svc.%s" $fullname $service $ns $domain))) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s-cluster.%s.%s.svc" $fullname $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s-cluster.%s.%s" $fullname $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s-cluster.%s.%s.svc.%s" $fullname $service $ns $domain))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s-cluster.%s.%s.svc" $fullname $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s-cluster.%s.%s" $fullname $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s.%s.svc.%s" $service $ns $domain))) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s.%s.svc" $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "%s.%s" $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s.%s.svc.%s" $service $ns $domain))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s.%s.svc" $service $ns))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s.%s" $service $ns))) -}}
{{- end -}}
{{- if (ne (toJson $state.Values.external.domain) "null") -}}
{{- $names = (concat (default (list) $names) (list (tpl $state.Values.external.domain $state.Dot))) -}}
{{- $names = (concat (default (list) $names) (list (printf "*.%s" (tpl $state.Values.external.domain $state.Dot)))) -}}
{{- end -}}
{{- $names = (concat (default (list) $names) (default (list) (get (fromJson (include "redpanda.gatewayServerCertDNSNames" (dict "a" (list $state $name)))) "r"))) -}}
{{- $duration := (default "43800h" $data.duration) -}}
{{- $issuerRef := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $data.issuerRef (mustMergeOverwrite (dict "name" "") (dict "kind" "Issuer" "group" "cert-manager.io" "name" (printf "%s-%s-root-issuer" $fullname $name))))))) "r") -}}
{{- $certs = (concat (default (list) $certs) (list (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "secretName" "" "issuerRef" (dict "name" "")) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "cert-manager.io/v1" "kind" "Certificate")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-cert" $fullname $name) "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace)) "spec" (mustMergeOverwrite (dict "secretName" "" "issuerRef" (dict "name" "")) (dict "dnsNames" $names "duration" (get (fromJson (include "_shims.time_Duration_String" (dict "a" (list (get (fromJson (include "_shims.time_ParseDuration" (dict "a" (list $duration)))) "r"))))) "r") "isCA" false "issuerRef" $issuerRef "secretName" (get (fromJson (include "redpanda.TLSCert.ServerSecretName" (dict "a" (list $data $state $name)))) "r") "privateKey" (mustMergeOverwrite (dict) (dict "algorithm" "ECDSA" "size" (256 | int))))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseClientCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $data := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- if (and (ne (toJson $data.secretRef) "null") (eq (toJson $data.clientSecretRef) "null")) -}}
{{- $_ := (fail (printf ".clientSecretRef MUST be set if .secretRef is set and require_client_auth is true: Cert %q" $name)) -}}
{{- end -}}
{{- if (ne (toJson $data.clientSecretRef) "null") -}}
{{- continue -}}
{{- end -}}
{{- $issuerRef := (mustMergeOverwrite (dict "name" "") (dict "group" "cert-manager.io" "kind" "Issuer" "name" (printf "%s-%s-root-issuer" $fullname $name))) -}}
{{- if (ne (toJson $data.issuerRef) "null") -}}
{{- $issuerRef = $data.issuerRef -}}
{{- $_ := (set $issuerRef "group" "cert-manager.io") -}}
{{- end -}}
{{- $duration := (default "43800h" $data.duration) -}}
{{- $certs = (concat (default (list) $certs) (list (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "secretName" "" "issuerRef" (dict "name" "")) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "cert-manager.io/v1" "kind" "Certificate")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-%s-client" $fullname $name) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict "secretName" "" "issuerRef" (dict "name" "")) (dict "commonName" (printf "%s--%s-client" $fullname $name) "duration" (get (fromJson (include "_shims.time_Duration_String" (dict "a" (list (get (fromJson (include "_shims.time_ParseDuration" (dict "a" (list $duration)))) "r"))))) "r") "isCA" false "secretName" (get (fromJson (include "redpanda.TLSCert.ClientSecretName" (dict "a" (list $data $state $name)))) "r") "privateKey" (mustMergeOverwrite (dict) (dict "algorithm" "ECDSA" "size" (256 | int))) "issuerRef" $issuerRef)))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $certs) | toJson -}}
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

