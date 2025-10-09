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
{{- if (ne (toJson $admin.sasl) "null") -}}
{{- $_ := (set $cfg "authentication" (mustMergeOverwrite (dict) (dict "basic" (mustMergeOverwrite (dict) (dict "username" $admin.sasl.username))))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD" $admin.sasl.passwordSecretRef)))) "r") -}}
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
{{- $tls_5 := (get (fromJson (include "console.configMapper.configureTLS" (dict "a" (list $m $kafka.tls)))) "r") -}}
{{- if (ne (toJson $tls_5) "null") -}}
{{- $_ := (set $cfg "tls" $tls_5) -}}
{{- end -}}
{{- if (ne (toJson $kafka.sasl) "null") -}}
{{- $_ := (set $cfg "sasl" (mustMergeOverwrite (dict) (dict "enabled" true "username" $kafka.sasl.username "mechanism" (toString $kafka.sasl.mechanism)))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_PASSWORD" $kafka.sasl.passwordSecretRef)))) "r") -}}
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
{{- $tls_6 := (get (fromJson (include "console.configMapper.configureTLS" (dict "a" (list $m $schema.tls)))) "r") -}}
{{- if (ne (toJson $tls_6) "null") -}}
{{- $_ := (set $cfg "tls" $tls_6) -}}
{{- end -}}
{{- if (ne (toJson $schema.sasl) "null") -}}
{{- $_ := (set $cfg "authentication" (mustMergeOverwrite (dict) (dict "basic" (mustMergeOverwrite (dict) (dict "username" $schema.sasl.username))))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD" $schema.sasl.passwordSecretRef)))) "r") -}}
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
{{- $ca_7 := (get (fromJson (include "console.volumes.MaybeAdd" (dict "a" (list $m.Volumes $tls.caCertSecretRef)))) "r") -}}
{{- if (ne (toJson $ca_7) "null") -}}
{{- $_ := (set $out "caFilepath" $ca_7) -}}
{{- end -}}
{{- $cert_8 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $tls.certSecretRef)))) "r") -}}
{{- if (ne (toJson $cert_8) "null") -}}
{{- $_ := (set $out "certFilepath" $cert_8) -}}
{{- end -}}
{{- $key_9 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $tls.keySecretRef)))) "r") -}}
{{- if (ne (toJson $key_9) "null") -}}
{{- $_ := (set $out "keyFilepath" $key_9) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.addEnv" -}}
{{- $m := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $ref := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (eq $ref.key "") (eq $ref.name "")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $m "Env" (concat (default (list) $m.Env) (list (mustMergeOverwrite (dict "name" "") (dict "name" $name "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $ref.name)) (dict "key" $ref.key))))))))) -}}
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
{{- $cmr_10 := $ref.configMapKeyRef -}}
{{- if (ne (toJson $cmr_10) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddConfigMap" (dict "a" (list $v $cmr_10)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $skr_11 := $ref.secretKeyRef -}}
{{- if (ne (toJson $skr_11) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $v (mustMergeOverwrite (dict "name" "") (dict "name" $skr_11.name "key" $skr_11.key)))))) "r")) | toJson -}}
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
{{- $_236___ok_12 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.ConfigMaps $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_236___ok_12 0) -}}
{{- $ok_12 := (index $_236___ok_12 1) -}}
{{- if (not $ok_12) -}}
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
{{- $ref := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (eq (toJson $ref) "null") ((and (eq $ref.key "") (eq $ref.name "")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_248___ok_13 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.Secrets $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_248___ok_13 0) -}}
{{- $ok_13 := (index $_248___ok_13 1) -}}
{{- if (not $ok_13) -}}
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

