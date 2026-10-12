{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/config.go" */ -}}

{{- define "console.StaticConfigurationSourceToPartialRenderValues" -}}
{{- $src := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mapper := (mustMergeOverwrite (dict "Volumes" (coalesce nil) "Env" (coalesce nil)) (dict "Volumes" (mustMergeOverwrite (dict "Name" "" "Dir" "" "Secrets" (coalesce nil) "ConfigMaps" (coalesce nil)) (dict "Name" "redpanda-certificates" "Dir" "/etc/tls/certs" "Secrets" (dict) "ConfigMaps" (dict))))) -}}
{{- $cfg := (get (fromJson (include "console.configMapper.toConfig" (dict "a" (list $mapper $src)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict) (dict "config" $cfg "extraEnv" $mapper.Env "extraVolumes" (get (fromJson (include "console.volumes.Volumes" (dict "a" (list $mapper.Volumes)))) "r") "extraVolumeMounts" (get (fromJson (include "console.volumes.VolumeMounts" (dict "a" (list $mapper.Volumes)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.toConfig" -}}
{{- $m := (index .a 0) -}}
{{- $src := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "redpanda" (mustMergeOverwrite (dict) (dict)))) -}}
{{- $kafka_1 := (get (fromJson (include "console.configMapper.configureKafka" (dict "a" (list $m $src.kafka)))) "r") -}}
{{- if (ne (toJson $kafka_1) "null") -}}
{{- $_ := (set $cfg "kafka" $kafka_1) -}}
{{- end -}}
{{- $admin_2 := (get (fromJson (include "console.configMapper.configureAdmin" (dict "a" (list $m $src.admin)))) "r") -}}
{{- if (ne (toJson $admin_2) "null") -}}
{{- $_ := (set $cfg.redpanda "adminApi" $admin_2) -}}
{{- end -}}
{{- $schema_3 := (get (fromJson (include "console.configMapper.configureSchemaRegistry" (dict "a" (list $m $src.schemaRegistry)))) "r") -}}
{{- if (ne (toJson $schema_3) "null") -}}
{{- $_ := (set $cfg "schemaRegistry" $schema_3) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureAdmin" -}}
{{- $m := (index .a 0) -}}
{{- $admin := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $admin) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "enabled" true "urls" $admin.urls)) -}}
{{- $tls_4 := (get (fromJson (include "console.configMapper.configureTLS" (dict "a" (list $m $admin.tls)))) "r") -}}
{{- if (ne (toJson $tls_4) "null") -}}
{{- $_ := (set $cfg "tls" $tls_4) -}}
{{- end -}}
{{- $auth_5 := $admin.sasl -}}
{{- if (ne (toJson $auth_5) "null") -}}
{{- if (ne (toJson $auth_5.passwordSecretRef) "null") -}}
{{- $_ := (set $cfg "authentication" (mustMergeOverwrite (dict) (dict "basic" (mustMergeOverwrite (dict) (dict "username" $auth_5.username))))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD" $auth_5.passwordSecretRef)))) "r") -}}
{{- end -}}
{{- if (and (eq (toJson $auth_5.passwordSecretRef) "null") (ne (toJson $auth_5.token) "null")) -}}
{{- $_ := (set $cfg "authentication" (mustMergeOverwrite (dict) (dict))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "REDPANDA_ADMINAPI_AUTHENTICATION_BEARERTOKEN" $auth_5.token)))) "r") -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureKafka" -}}
{{- $m := (index .a 0) -}}
{{- $kafka := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $kafka) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "brokers" $kafka.brokers)) -}}
{{- $tls_6 := (get (fromJson (include "console.configMapper.configureTLS" (dict "a" (list $m $kafka.tls)))) "r") -}}
{{- if (ne (toJson $tls_6) "null") -}}
{{- $_ := (set $cfg "tls" $tls_6) -}}
{{- end -}}
{{- if (ne (toJson $kafka.sasl) "null") -}}
{{- $_ := (set $cfg "sasl" (get (fromJson (include "console.configMapper.configureKafkaSASL" (dict "a" (list $m $kafka.sasl)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureKafkaSASL" -}}
{{- $m := (index .a 0) -}}
{{- $sasl := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "enabled" true "username" $sasl.username "mechanism" (toString $sasl.mechanism))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_PASSWORD" $sasl.passwordSecretRef)))) "r") -}}
{{- if (ne (toJson $sasl.oauth) "null") -}}
{{- $_ := (set $cfg "oauth" (mustMergeOverwrite (dict) (dict))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_OAUTH_TOKEN" $sasl.oauth.token)))) "r") -}}
{{- end -}}
{{- $gssapi_7 := $sasl.gssapi -}}
{{- if (ne (toJson $gssapi_7) "null") -}}
{{- $_ := (set $cfg "gssapi" (mustMergeOverwrite (dict) (dict "authType" $gssapi_7.authType "keyTabPath" $gssapi_7.keyTabPath "kerberosConfigPath" $gssapi_7.kerberosConfigPath "serviceName" $gssapi_7.serviceName "username" $gssapi_7.username "realm" $gssapi_7.realm "enableFast" $gssapi_7.enableFast))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_GSSAPI_PASSWORD" $gssapi_7.password)))) "r") -}}
{{- end -}}
{{- $msk_8 := $sasl.awsMskIam -}}
{{- if (ne (toJson $msk_8) "null") -}}
{{- $_ := (set $cfg "awsMskIam" (mustMergeOverwrite (dict) (dict "accessKey" $msk_8.accessKey))) -}}
{{- if (ne $msk_8.userAgent "") -}}
{{- $_ := (set $cfg.awsMskIam "userAgent" $msk_8.userAgent) -}}
{{- end -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_AWSMSKIAM_SECRETKEY" $msk_8.secretKey)))) "r") -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_AWSMSKIAM_SESSIONTOKEN" $msk_8.sessionToken)))) "r") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureSchemaRegistry" -}}
{{- $m := (index .a 0) -}}
{{- $schema := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $schema) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "enabled" true "urls" $schema.urls)) -}}
{{- $tls_9 := (get (fromJson (include "console.configMapper.configureTLS" (dict "a" (list $m $schema.tls)))) "r") -}}
{{- if (ne (toJson $tls_9) "null") -}}
{{- $_ := (set $cfg "tls" $tls_9) -}}
{{- end -}}
{{- if (ne (toJson $schema.sasl) "null") -}}
{{- $_ := (set $cfg "authentication" (mustMergeOverwrite (dict) (dict "basic" (mustMergeOverwrite (dict) (dict "username" $schema.sasl.username))))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD" $schema.sasl.password)))) "r") -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN" $schema.sasl.token)))) "r") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureTLS" -}}
{{- $m := (index .a 0) -}}
{{- $tls := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $tls) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $out := (mustMergeOverwrite (dict) (dict "enabled" true)) -}}
{{- if $tls.insecureSkipTlsVerify -}}
{{- $_ := (set $out "insecureSkipTlsVerify" $tls.insecureSkipTlsVerify) -}}
{{- end -}}
{{- $ca_10 := (get (fromJson (include "console.volumes.MaybeAdd" (dict "a" (list $m.Volumes $tls.caCert)))) "r") -}}
{{- if (ne (toJson $ca_10) "null") -}}
{{- $_ := (set $out "caFilepath" $ca_10) -}}
{{- end -}}
{{- $cert_11 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $tls.cert)))) "r") -}}
{{- if (ne (toJson $cert_11) "null") -}}
{{- $_ := (set $out "certFilepath" $cert_11) -}}
{{- end -}}
{{- $key_12 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $tls.key)))) "r") -}}
{{- if (ne (toJson $key_12) "null") -}}
{{- $_ := (set $out "keyFilepath" $key_12) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.addEnv" -}}
{{- $m := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $src := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $src) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne (toJson $src.inline) "null") -}}
{{- $_ := (set $m "Env" (concat (default (list) $m.Env) (list (mustMergeOverwrite (dict "name" "") (dict "name" $name "value" $src.inline))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $ref_13 := $src.secretKeyRef -}}
{{- if (and (and (ne (toJson $ref_13) "null") (ne $ref_13.name "")) (ne $ref_13.key "")) -}}
{{- $_ := (set $m "Env" (concat (default (list) $m.Env) (list (mustMergeOverwrite (dict "name" "") (dict "name" $name "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $ref_13.name)) (dict "key" $ref_13.key))))))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $ref_14 := $src.configMapKeyRef -}}
{{- if (and (and (ne (toJson $ref_14) "null") (ne $ref_14.name "")) (ne $ref_14.key "")) -}}
{{- $_ := (set $m "Env" (concat (default (list) $m.Env) (list (mustMergeOverwrite (dict "name" "") (dict "name" $name "valueFrom" (mustMergeOverwrite (dict) (dict "configMapKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $ref_14.name)) (dict "key" $ref_14.key))))))))) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "console.volumes.MaybeAdd" -}}
{{- $v := (index .a 0) -}}
{{- $ref := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $ref) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cmr_15 := $ref.configMapKeyRef -}}
{{- if (ne (toJson $cmr_15) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddConfigMap" (dict "a" (list $v $cmr_15)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $skr_16 := $ref.secretKeyRef -}}
{{- if (ne (toJson $skr_16) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $v (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $skr_16.name)) (dict "key" $skr_16.key)))))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.volumes.MaybeAddConfigMap" -}}
{{- $v := (index .a 0) -}}
{{- $ref := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (eq (toJson $ref) "null") ((and (eq $ref.key "") (eq $ref.name "")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_308___ok_17 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.ConfigMaps $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_308___ok_17 0) -}}
{{- $ok_17 := (index $_308___ok_17 1) -}}
{{- if (not $ok_17) -}}
{{- $_ := (set $v.ConfigMaps $ref.name (dict)) -}}
{{- end -}}
{{- $_ := (set (index $v.ConfigMaps $ref.name) $ref.key true) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/configmaps/%s/%s" $v.Dir $ref.name $ref.key)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.volumes.MaybeAddSecret" -}}
{{- $v := (index .a 0) -}}
{{- $secretRef := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (eq (toJson $secretRef) "null") (eq (toJson $secretRef.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $ref := $secretRef.secretKeyRef -}}
{{- if (or (eq (toJson $ref) "null") ((and (eq $ref.key "") (eq $ref.name "")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_325___ok_18 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.Secrets $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_325___ok_18 0) -}}
{{- $ok_18 := (index $_325___ok_18 1) -}}
{{- if (not $ok_18) -}}
{{- $_ := (set $v.Secrets $ref.name (dict)) -}}
{{- end -}}
{{- $_ := (set (index $v.Secrets $ref.name) $ref.key true) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/secrets/%s/%s" $v.Dir $ref.name $ref.key)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.volumes.VolumeMounts" -}}
{{- $v := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (eq ((get (fromJson (include "_shims.len" (dict "a" (list $v.Secrets)))) "r") | int) (0 | int)) (eq ((get (fromJson (include "_shims.len" (dict "a" (list $v.ConfigMaps)))) "r") | int) (0 | int))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $v.Name "mountPath" $v.Dir)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.volumes.Volumes" -}}
{{- $v := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (eq ((get (fromJson (include "_shims.len" (dict "a" (list $v.Secrets)))) "r") | int) (0 | int)) (eq ((get (fromJson (include "_shims.len" (dict "a" (list $v.ConfigMaps)))) "r") | int) (0 | int))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $sources := (coalesce nil) -}}
{{- range $_, $secret := (sortAlpha (keys $v.Secrets)) -}}
{{- $items := (coalesce nil) -}}
{{- range $_, $key := (sortAlpha (keys (index $v.Secrets $secret))) -}}
{{- $items = (concat (default (list) $items) (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" $key "path" (printf "secrets/%s/%s" $secret $key))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sources = (concat (default (list) $sources) (list (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $secret)) (dict "items" $items)))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $configmap := (sortAlpha (keys $v.ConfigMaps)) -}}
{{- $items := (coalesce nil) -}}
{{- range $_, $key := (sortAlpha (keys (index $v.ConfigMaps $configmap))) -}}
{{- $items = (concat (default (list) $items) (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" $key "path" (printf "configmaps/%s/%s" $configmap $key))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sources = (concat (default (list) $sources) (list (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $configmap)) (dict "items" $items)))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "projected" (mustMergeOverwrite (dict "sources" (coalesce nil)) (dict "sources" $sources)))) (dict "name" $v.Name)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

