{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/console.tpl.go" */ -}}

{{- define "redpanda.consoleChartIntegration" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.enabled true)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $consoleDot := (index $state.Dot.Subcharts "console") -}}
{{- $loadedValues := $consoleDot.Values -}}
{{- $consoleValue := $consoleDot.Values -}}
{{- $license_1 := $state.Values.enterprise.license -}}
{{- if (and (ne $license_1 "") (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.secret.create false)))) "r"))) -}}
{{- $_ := (set $consoleValue.secret "create" true) -}}
{{- $_ := (set $consoleValue.secret "license" $license_1) -}}
{{- end -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.configmap.create false)))) "r")) -}}
{{- $_ := (set $consoleValue.configmap "create" true) -}}
{{- $_ := (set $consoleValue "config" (get (fromJson (include "redpanda.ConsoleConfig" (dict "a" (list $state)))) "r")) -}}
{{- end -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.deployment.create false)))) "r")) -}}
{{- $_ := (set $consoleValue.deployment "create" true) -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $command := (list "sh" "-c" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" "set -e; IFS=':' read -r KAFKA_SASL_USERNAME KAFKA_SASL_PASSWORD KAFKA_SASL_MECHANISM < <(grep \"\" $(find /mnt/users/* -print));" (printf " KAFKA_SASL_MECHANISM=${KAFKA_SASL_MECHANISM:-%s};" (get (fromJson (include "redpanda.GetSASLMechanism" (dict "a" (list $state)))) "r"))) " export KAFKA_SASL_USERNAME KAFKA_SASL_PASSWORD KAFKA_SASL_MECHANISM;") " export KAFKA_SCHEMAREGISTRY_USERNAME=$KAFKA_SASL_USERNAME;") " export KAFKA_SCHEMAREGISTRY_PASSWORD=$KAFKA_SASL_PASSWORD;") " export REDPANDA_ADMINAPI_USERNAME=$KAFKA_SASL_USERNAME;") " export REDPANDA_ADMINAPI_PASSWORD=$KAFKA_SASL_PASSWORD;") " /app/console $@") " --") -}}
{{- $_ := (set $consoleValue.deployment "command" $command) -}}
{{- end -}}
{{- $secret_2 := $state.Values.enterprise.licenseSecretRef -}}
{{- if (ne (toJson $secret_2) "null") -}}
{{- $_ := (set $consoleValue "licenseSecretRef" $secret_2) -}}
{{- end -}}
{{- $_ := (set $consoleValue "extraVolumes" (get (fromJson (include "redpanda.consoleTLSVolumes" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $consoleValue "extraVolumeMounts" (get (fromJson (include "redpanda.consoleTLSVolumesMounts" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $consoleDot "Values" $consoleValue) -}}
{{- $cfg := (get (fromJson (include "console.ConfigMap" (dict "a" (list $consoleDot)))) "r") -}}
{{- if (eq (toJson $consoleValue.podAnnotations) "null") -}}
{{- $_ := (set $consoleValue "podAnnotations" (dict)) -}}
{{- end -}}
{{- $_ := (set $consoleValue.podAnnotations "checksum-redpanda-chart/config" (sha256sum (toYaml $cfg))) -}}
{{- end -}}
{{- $_ := (set $consoleDot "Values" $consoleValue) -}}
{{- $manifests := (list (get (fromJson (include "console.Secret" (dict "a" (list $consoleDot)))) "r") (get (fromJson (include "console.ConfigMap" (dict "a" (list $consoleDot)))) "r") (get (fromJson (include "console.Deployment" (dict "a" (list $consoleDot)))) "r")) -}}
{{- $_ := (set $consoleDot "Values" $loadedValues) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.consoleTLSVolumesMounts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mounts := (list) -}}
{{- $sasl_3 := $state.Values.auth.sasl -}}
{{- if (and $sasl_3.enabled (ne $sasl_3.secretRef "")) -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf "%s-users" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "mountPath" "/mnt/users" "readOnly" true)))) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list (get (fromJson (include "redpanda.Listeners.TrustStores" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r"))))) "r") | int) (0 | int)) -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "truststores" "mountPath" "/etc/truststores" "readOnly" true)))) -}}
{{- end -}}
{{- $visitedCert := (dict) -}}
{{- range $_, $tlsCfg := (list $state.Values.listeners.kafka.tls $state.Values.listeners.schemaRegistry.tls $state.Values.listeners.admin.tls) -}}
{{- $_127___visited := (get (fromJson (include "_shims.dicttest" (dict "a" (list $visitedCert $tlsCfg.cert false)))) "r") -}}
{{- $_ := (index $_127___visited 0) -}}
{{- $visited := (index $_127___visited 1) -}}
{{- if (or (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $tlsCfg $state.Values.tls)))) "r")) $visited) -}}
{{- continue -}}
{{- end -}}
{{- $_ := (set $visitedCert $tlsCfg.cert true) -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf "redpanda-%s-cert" $tlsCfg.cert) "mountPath" (printf "%s/%s" "/etc/tls/certs" $tlsCfg.cert))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $mounts) (default (list) $state.Values.console.extraVolumeMounts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.consoleTLSVolumes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volumes := (list) -}}
{{- $sasl_4 := $state.Values.auth.sasl -}}
{{- if (and $sasl_4.enabled (ne $sasl_4.secretRef "")) -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" $state.Values.auth.sasl.secretRef)))) (dict "name" (printf "%s-users" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")))))) -}}
{{- end -}}
{{- $vol_5 := (get (fromJson (include "redpanda.Listeners.TrustStoreVolume" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- if (ne (toJson $vol_5) "null") -}}
{{- $volumes = (concat (default (list) $volumes) (list $vol_5)) -}}
{{- end -}}
{{- $visitedCert := (dict) -}}
{{- range $_, $tlsCfg := (list $state.Values.listeners.kafka.tls $state.Values.listeners.schemaRegistry.tls $state.Values.listeners.admin.tls) -}}
{{- $_166___visited := (get (fromJson (include "_shims.dicttest" (dict "a" (list $visitedCert $tlsCfg.cert false)))) "r") -}}
{{- $_ := (index $_166___visited 0) -}}
{{- $visited := (index $_166___visited 1) -}}
{{- if (or (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $tlsCfg $state.Values.tls)))) "r")) $visited) -}}
{{- continue -}}
{{- end -}}
{{- $_ := (set $visitedCert $tlsCfg.cert true) -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "defaultMode" (0o420 | int) "secretName" (get (fromJson (include "redpanda.CertSecretName" (dict "a" (list $state $tlsCfg.cert (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $tlsCfg.cert)))) "r"))))) "r"))))) (dict "name" (printf "redpanda-%s-cert" $tlsCfg.cert))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $volumes) (default (list) $state.Values.console.extraVolumes))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ConsoleConfig" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $schemaURLs := (coalesce nil) -}}
{{- if $state.Values.listeners.schemaRegistry.enabled -}}
{{- $schema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.schemaRegistry.tls $state.Values.tls)))) "r") -}}
{{- $schema = "https" -}}
{{- end -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($state.Values.statefulset.replicas | int)|int) (1|int) -}}
{{- $schemaURLs = (concat (default (list) $schemaURLs) (list (printf "%s://%s-%d.%s:%d" $schema (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $i (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") ($state.Values.listeners.schemaRegistry.port | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $schema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $schema = "https" -}}
{{- end -}}
{{- $c := (dict "kafka" (dict "brokers" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.kafka.port | int))))) "r") "sasl" (dict "enabled" (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r")) "tls" (get (fromJson (include "redpanda.ListenerConfig.ConsoleTLS" (dict "a" (list $state.Values.listeners.kafka $state.Values.tls)))) "r")) "redpanda" (dict "adminApi" (dict "enabled" true "urls" (list (printf "%s://%s:%d" $schema (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") ($state.Values.listeners.admin.port | int))) "tls" (get (fromJson (include "redpanda.ListenerConfig.ConsoleTLS" (dict "a" (list $state.Values.listeners.admin $state.Values.tls)))) "r"))) "schemaRegistry" (dict "enabled" $state.Values.listeners.schemaRegistry.enabled "urls" $schemaURLs "tls" (get (fromJson (include "redpanda.ListenerConfig.ConsoleTLS" (dict "a" (list $state.Values.listeners.schemaRegistry $state.Values.tls)))) "r"))) -}}
{{- if (eq (toJson $state.Values.console.config) "null") -}}
{{- $_ := (set $state.Values.console "config" (dict)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $state.Values.console.config $c)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

