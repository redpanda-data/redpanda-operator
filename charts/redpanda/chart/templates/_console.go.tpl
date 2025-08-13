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
{{- $consoleState := (get (fromJson (include "chart.DotToState" (dict "a" (list (index $state.Dot.Subcharts "console"))))) "r") -}}
{{- $staticCfg := (get (fromJson (include "redpanda.RenderState.ToStaticConfig" (dict "a" (list $state)))) "r") -}}
{{- $overlay := (get (fromJson (include "console.StaticConfigurationSourceToPartialRenderValues" (dict "a" (list $staticCfg)))) "r") -}}
{{- $_ := (set $consoleState.Values.configmap "create" true) -}}
{{- $_ := (set $consoleState.Values.deployment "create" true) -}}
{{- $_ := (set $consoleState.Values "extraEnv" (concat (default (list) $overlay.extraEnv) (default (list) $consoleState.Values.extraEnv))) -}}
{{- $_ := (set $consoleState.Values "extraVolumes" (concat (default (list) $overlay.extraVolumes) (default (list) $consoleState.Values.extraVolumes))) -}}
{{- $_ := (set $consoleState.Values "extraVolumeMounts" (concat (default (list) $overlay.extraVolumeMounts) (default (list) $consoleState.Values.extraVolumeMounts))) -}}
{{- $_ := (set $consoleState.Values "config" (merge (dict) $consoleState.Values.config $overlay.config)) -}}
{{- if (ne (toJson $state.Values.enterprise.licenseSecretRef) "null") -}}
{{- $_ := (set $consoleState.Values "licenseSecretRef" $state.Values.enterprise.licenseSecretRef) -}}
{{- end -}}
{{- $license_1 := $state.Values.enterprise.license -}}
{{- if (and (ne $license_1 "") (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.secret.create false)))) "r"))) -}}
{{- $_ := (set $consoleState.Values.secret "create" true) -}}
{{- $_ := (set $consoleState.Values.secret "license" $license_1) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "console.Secret" (dict "a" (list $consoleState)))) "r") (get (fromJson (include "console.ConfigMap" (dict "a" (list $consoleState)))) "r") (get (fromJson (include "console.Deployment" (dict "a" (list $consoleState)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RenderState.ToStaticConfig" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $username := (get (fromJson (include "redpanda.BootstrapUser.Username" (dict "a" (list $state.Values.auth.sasl.bootstrapUser)))) "r") -}}
{{- $passwordRef := (get (fromJson (include "redpanda.BootstrapUser.SecretKeySelector" (dict "a" (list $state.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r"))))) "r") -}}
{{- $kafkaSpec := (mustMergeOverwrite (dict "brokers" (coalesce nil)) (dict "brokers" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.kafka.port | int))))) "r"))) -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.kafka.tls $state.Values.tls)))) "r") -}}
{{- $_ := (set $kafkaSpec "tls" (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $state.Values.listeners.kafka.tls $state $state.Values.tls)))) "r")) -}}
{{- end -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $_ := (set $kafkaSpec "sasl" (mustMergeOverwrite (dict "passwordSecretRef" (dict "name" "") "mechanism" "" "oauth" (dict "tokenSecretRef" (dict "name" "")) "gssapi" (dict "authType" "" "keyTabPath" "" "kerberosConfigPath" "" "serviceName" "" "username" "" "passwordSecretRef" (dict "name" "") "realm" "" "enableFast" false) "awsMskIam" (dict "accessKey" "" "secretKeySecretRef" (dict "name" "") "sessionTokenSecretRef" (dict "name" "") "userAgent" "")) (dict "username" $username "passwordSecretRef" (mustMergeOverwrite (dict "name" "") (dict "name" $passwordRef.name "key" $passwordRef.key)) "mechanism" (toString (get (fromJson (include "redpanda.BootstrapUser.GetMechanism" (dict "a" (list $state.Values.auth.sasl.bootstrapUser)))) "r"))))) -}}
{{- end -}}
{{- $adminTLS := (coalesce nil) -}}
{{- $adminSchema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $adminSchema = "https" -}}
{{- $adminTLS = (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $state.Values.listeners.admin.tls $state $state.Values.tls)))) "r") -}}
{{- end -}}
{{- $adminAuth := (coalesce nil) -}}
{{- $_102_adminAuthEnabled__ := (get (fromJson (include "_shims.typetest" (dict "a" (list "bool" (index $state.Values.config.cluster "admin_api_require_auth") false)))) "r") -}}
{{- $adminAuthEnabled := (index $_102_adminAuthEnabled__ 0) -}}
{{- $_ := (index $_102_adminAuthEnabled__ 1) -}}
{{- if $adminAuthEnabled -}}
{{- $adminAuth = (mustMergeOverwrite (dict "passwordSecretRef" (dict "name" "")) (dict "username" $username "passwordSecretRef" (mustMergeOverwrite (dict "name" "") (dict "name" $passwordRef.name "key" $passwordRef.key)))) -}}
{{- end -}}
{{- $adminSpec := (mustMergeOverwrite (dict "urls" (coalesce nil)) (dict "tls" $adminTLS "sasl" $adminAuth "urls" (list (printf "%s://%s:%d" $adminSchema (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") ($state.Values.listeners.admin.port | int))))) -}}
{{- $schemaRegistrySpec := (coalesce nil) -}}
{{- if $state.Values.listeners.schemaRegistry.enabled -}}
{{- $schemaTLS := (coalesce nil) -}}
{{- $schemaSchema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.schemaRegistry.tls $state.Values.tls)))) "r") -}}
{{- $schemaSchema = "https" -}}
{{- $schemaTLS = (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $state.Values.listeners.schemaRegistry.tls $state $state.Values.tls)))) "r") -}}
{{- end -}}
{{- $schemaURLs := (coalesce nil) -}}
{{- $brokers := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.schemaRegistry.port | int))))) "r") -}}
{{- range $_, $broker := $brokers -}}
{{- $schemaURLs = (concat (default (list) $schemaURLs) (list (printf "%s://%s" $schemaSchema $broker))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $schemaRegistrySpec = (mustMergeOverwrite (dict "urls" (coalesce nil)) (dict "urls" $schemaURLs "tls" $schemaTLS)) -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $_ := (set $schemaRegistrySpec "sasl" (mustMergeOverwrite (dict "passwordSecretRef" (dict "name" "") "token" (dict "name" "")) (dict "username" $username "passwordSecretRef" (mustMergeOverwrite (dict "name" "") (dict "name" $passwordRef.name "key" $passwordRef.key))))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict) (dict "kafka" $kafkaSpec "admin" $adminSpec "schemaRegistry" $schemaRegistrySpec))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

