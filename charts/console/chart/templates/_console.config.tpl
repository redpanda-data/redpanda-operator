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
{{- $cfg := (mustMergeOverwrite (dict) (dict)) -}}
{{- $kafka_1 := (get (fromJson (include "console.configMapper.configureKafka" (dict "a" (list $m $src)))) "r") -}}
{{- if (ne (toJson $kafka_1) "null") -}}
{{- $_ := (set $cfg "kafka" $kafka_1) -}}
{{- end -}}
{{- $admin_2 := (get (fromJson (include "console.configMapper.configureAdmin" (dict "a" (list $m $src.admin)))) "r") -}}
{{- if (ne (toJson $admin_2) "null") -}}
{{- $_ := (set $cfg "redpanda" (mustMergeOverwrite (dict) (dict "adminApi" $admin_2))) -}}
{{- end -}}
{{- if (eq (toJson $cfg.redpanda) "null") -}}
{{- $_ := (set $cfg "redpanda" (mustMergeOverwrite (dict) (dict))) -}}
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
{{- if (ne (toJson $admin.tls) "null") -}}
{{- $_ := (set $cfg "tls" (mustMergeOverwrite (dict) (dict "enabled" true))) -}}
{{- if $admin.tls.insecureSkipTlsVerify -}}
{{- $_ := (set $cfg.tls "insecureSkipTlsVerify" $admin.tls.insecureSkipTlsVerify) -}}
{{- end -}}
{{- $ca_3 := (get (fromJson (include "console.volumes.MaybeAdd" (dict "a" (list $m.Volumes $admin.tls.caCertSecretRef)))) "r") -}}
{{- if (ne (toJson $ca_3) "null") -}}
{{- $_ := (set $cfg.tls "caFilepath" $ca_3) -}}
{{- end -}}
{{- $cert_4 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $admin.tls.certSecretRef)))) "r") -}}
{{- if (ne (toJson $cert_4) "null") -}}
{{- $_ := (set $cfg.tls "certFilepath" $cert_4) -}}
{{- end -}}
{{- $key_5 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $admin.tls.keySecretRef)))) "r") -}}
{{- if (ne (toJson $key_5) "null") -}}
{{- $_ := (set $cfg.tls "keyFilepath" $key_5) -}}
{{- end -}}
{{- end -}}
{{- if (ne (toJson $admin.sasl) "null") -}}
{{- $_ := (set $cfg "username" $admin.sasl.username) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "REDPANDA_ADMINAPI_PASSWORD" $admin.sasl.passwordSecretRef)))) "r") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.configMapper.configureKafka" -}}
{{- $m := (index .a 0) -}}
{{- $src := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq (toJson $src.kafka) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $cfg := (mustMergeOverwrite (dict) (dict "brokers" $src.kafka.brokers "schemaRegistry" (get (fromJson (include "console.configMapper.configureSchemaRegistry" (dict "a" (list $m $src.schemaRegistry)))) "r"))) -}}
{{- if (ne (toJson $src.kafka.tls) "null") -}}
{{- $_ := (set $cfg "tls" (mustMergeOverwrite (dict) (dict "enabled" true))) -}}
{{- if $src.kafka.tls.insecureSkipTlsVerify -}}
{{- $_ := (set $cfg.tls "insecureSkipTlsVerify" $src.kafka.tls.insecureSkipTlsVerify) -}}
{{- end -}}
{{- $ca_6 := (get (fromJson (include "console.volumes.MaybeAdd" (dict "a" (list $m.Volumes $src.kafka.tls.caCertSecretRef)))) "r") -}}
{{- if (ne (toJson $ca_6) "null") -}}
{{- $_ := (set $cfg.tls "caFilepath" $ca_6) -}}
{{- end -}}
{{- $cert_7 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $src.kafka.tls.certSecretRef)))) "r") -}}
{{- if (ne (toJson $cert_7) "null") -}}
{{- $_ := (set $cfg.tls "certFilepath" $cert_7) -}}
{{- end -}}
{{- $key_8 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $src.kafka.tls.keySecretRef)))) "r") -}}
{{- if (ne (toJson $key_8) "null") -}}
{{- $_ := (set $cfg.tls "keyFilepath" $key_8) -}}
{{- end -}}
{{- end -}}
{{- if (ne (toJson $src.kafka.sasl) "null") -}}
{{- $_ := (set $cfg "sasl" (mustMergeOverwrite (dict) (dict "enabled" true "username" $src.kafka.sasl.username "mechanism" (toString $src.kafka.sasl.mechanism)))) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SASL_PASSWORD" $src.kafka.sasl.passwordSecretRef)))) "r") -}}
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
{{- if (ne (toJson $schema.tls) "null") -}}
{{- $_ := (set $cfg "tls" (mustMergeOverwrite (dict) (dict "enabled" true))) -}}
{{- if $schema.tls.insecureSkipTlsVerify -}}
{{- $_ := (set $cfg.tls "insecureSkipTlsVerify" $schema.tls.insecureSkipTlsVerify) -}}
{{- end -}}
{{- $ca_9 := (get (fromJson (include "console.volumes.MaybeAdd" (dict "a" (list $m.Volumes $schema.tls.caCertSecretRef)))) "r") -}}
{{- if (ne (toJson $ca_9) "null") -}}
{{- $_ := (set $cfg.tls "caFilepath" $ca_9) -}}
{{- end -}}
{{- $cert_10 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $schema.tls.certSecretRef)))) "r") -}}
{{- if (ne (toJson $cert_10) "null") -}}
{{- $_ := (set $cfg.tls "certFilepath" $cert_10) -}}
{{- end -}}
{{- $key_11 := (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $m.Volumes $schema.tls.keySecretRef)))) "r") -}}
{{- if (ne (toJson $key_11) "null") -}}
{{- $_ := (set $cfg.tls "keyFilepath" $key_11) -}}
{{- end -}}
{{- end -}}
{{- if (ne (toJson $schema.sasl) "null") -}}
{{- $_ := (set $cfg "username" $schema.sasl.username) -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SCHEMA_PASSWORD" $schema.sasl.passwordSecretRef)))) "r") -}}
{{- $_ := (get (fromJson (include "console.configMapper.addEnv" (dict "a" (list $m "KAFKA_SCHEMA_BEARERTOKEN" $schema.sasl.token)))) "r") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
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
{{- $cmr_12 := $ref.configMapKeyRef -}}
{{- if (ne (toJson $cmr_12) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddConfigMap" (dict "a" (list $v $cmr_12)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $skr_13 := $ref.secretKeyRef -}}
{{- if (ne (toJson $skr_13) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.volumes.MaybeAddSecret" (dict "a" (list $v (mustMergeOverwrite (dict "name" "") (dict "name" $skr_13.name "key" $skr_13.key)))))) "r")) | toJson -}}
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
{{- $_242___ok_14 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.ConfigMaps $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_242___ok_14 0) -}}
{{- $ok_14 := (index $_242___ok_14 1) -}}
{{- if (not $ok_14) -}}
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
{{- $_254___ok_15 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $v.Secrets $ref.name (coalesce nil))))) "r") -}}
{{- $_ := (index $_254___ok_15 0) -}}
{{- $ok_15 := (index $_254___ok_15 1) -}}
{{- if (not $ok_15) -}}
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

