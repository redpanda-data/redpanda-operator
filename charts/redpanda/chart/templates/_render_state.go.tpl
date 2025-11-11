{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/render_state.go" */ -}}

{{- define "redpanda.RenderState.FetchBootstrapUser" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (or (eq (toJson $r.Values.auth.sasl) "null") (not $r.Values.auth.sasl.enabled)) (ne (toJson $r.Values.auth.sasl.bootstrapUser.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secretName := (printf "%s-bootstrap-user" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r")) -}}
{{- $_64_existing_1_ok_2 := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Secret" $r.Release.Namespace $secretName)))) "r") -}}
{{- $existing_1 := (index $_64_existing_1_ok_2 0) -}}
{{- $ok_2 := (index $_64_existing_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $_ := (set $existing_1 "immutable" true) -}}
{{- $_ := (set $r "BootstrapUserSecret" $existing_1) -}}
{{- $selector := (get (fromJson (include "redpanda.BootstrapUser.SecretKeySelector" (dict "a" (list $r.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $_81_data_3_found_4 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $existing_1.data $selector.key (coalesce nil))))) "r") -}}
{{- $data_3 := (index $_81_data_3_found_4 0) -}}
{{- $found_4 := (index $_81_data_3_found_4 1) -}}
{{- if $found_4 -}}
{{- $_ := (set $r "BootstrapUserPassword" (toString $data_3)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RenderState.FetchStatefulSetPodSelector" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if $r.Release.IsUpgrade -}}
{{- $_96_existing_5_ok_6 := (get (fromJson (include "_shims.lookup" (dict "a" (list "apps/v1" "StatefulSet" $r.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $existing_5 := (index $_96_existing_5_ok_6 0) -}}
{{- $ok_6 := (index $_96_existing_5_ok_6 1) -}}
{{- if (and $ok_6 (gt ((get (fromJson (include "_shims.len" (dict "a" (list $existing_5.spec.template.metadata.labels)))) "r") | int) (0 | int))) -}}
{{- $_ := (set $r "StatefulSetPodLabels" $existing_5.spec.template.metadata.labels) -}}
{{- $_ := (set $r "StatefulSetSelector" $existing_5.spec.selector.matchLabels) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RenderState.AsStaticConfigSource" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $username := (get (fromJson (include "redpanda.BootstrapUser.Username" (dict "a" (list $r.Values.auth.sasl.bootstrapUser)))) "r") -}}
{{- $passwordRef := (get (fromJson (include "redpanda.BootstrapUser.SecretKeySelector" (dict "a" (list $r.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $kafkaSpec := (mustMergeOverwrite (dict "brokers" (coalesce nil)) (dict "brokers" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $r ($r.Values.listeners.kafka.port | int))))) "r"))) -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $r.Values.listeners.kafka.tls $r.Values.tls)))) "r") -}}
{{- $_ := (set $kafkaSpec "tls" (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $r.Values.listeners.kafka.tls $r $r.Values.tls)))) "r")) -}}
{{- end -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $r.Values.auth)))) "r") -}}
{{- $_ := (set $kafkaSpec "sasl" (mustMergeOverwrite (dict "mechanism" "") (dict "username" $username "passwordSecretRef" (mustMergeOverwrite (dict) (dict "namespace" $r.Release.Namespace "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $passwordRef.name)) (dict "key" $passwordRef.key)))) "mechanism" (toString (get (fromJson (include "redpanda.BootstrapUser.GetMechanism" (dict "a" (list $r.Values.auth.sasl.bootstrapUser)))) "r"))))) -}}
{{- end -}}
{{- $adminTLS := (coalesce nil) -}}
{{- $adminSchema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $r.Values.listeners.admin.tls $r.Values.tls)))) "r") -}}
{{- $adminSchema = "https" -}}
{{- $adminTLS = (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $r.Values.listeners.admin.tls $r $r.Values.tls)))) "r") -}}
{{- end -}}
{{- $adminAuth := (coalesce nil) -}}
{{- $_144_adminAuthEnabled__ := (get (fromJson (include "_shims.typetest" (dict "a" (list "bool" (index $r.Values.config.cluster "admin_api_require_auth") false)))) "r") -}}
{{- $adminAuthEnabled := (index $_144_adminAuthEnabled__ 0) -}}
{{- $_ := (index $_144_adminAuthEnabled__ 1) -}}
{{- if $adminAuthEnabled -}}
{{- $adminAuth = (mustMergeOverwrite (dict) (dict "username" $username "passwordSecretRef" (mustMergeOverwrite (dict) (dict "namespace" $r.Release.Namespace "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $passwordRef.name)) (dict "key" $passwordRef.key)))))) -}}
{{- end -}}
{{- $adminSpec := (mustMergeOverwrite (dict "urls" (coalesce nil)) (dict "tls" $adminTLS "sasl" $adminAuth "urls" (list (printf "%s://%s:%d" $adminSchema (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $r)))) "r") ($r.Values.listeners.admin.port | int))))) -}}
{{- $schemaRegistrySpec := (coalesce nil) -}}
{{- if $r.Values.listeners.schemaRegistry.enabled -}}
{{- $schemaTLS := (coalesce nil) -}}
{{- $schemaSchema := "http" -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $r.Values.listeners.schemaRegistry.tls $r.Values.tls)))) "r") -}}
{{- $schemaSchema = "https" -}}
{{- $schemaTLS = (get (fromJson (include "redpanda.InternalTLS.ToCommonTLS" (dict "a" (list $r.Values.listeners.schemaRegistry.tls $r $r.Values.tls)))) "r") -}}
{{- end -}}
{{- $schemaURLs := (coalesce nil) -}}
{{- $brokers := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $r ($r.Values.listeners.schemaRegistry.port | int))))) "r") -}}
{{- range $_, $broker := $brokers -}}
{{- $schemaURLs = (concat (default (list) $schemaURLs) (list (printf "%s://%s" $schemaSchema $broker))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $schemaRegistrySpec = (mustMergeOverwrite (dict "urls" (coalesce nil)) (dict "urls" $schemaURLs "tls" $schemaTLS)) -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $r.Values.auth)))) "r") -}}
{{- $_ := (set $schemaRegistrySpec "sasl" (mustMergeOverwrite (dict) (dict "username" $username "password" (mustMergeOverwrite (dict) (dict "namespace" $r.Release.Namespace "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $passwordRef.name)) (dict "key" $passwordRef.key))))))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict) (dict "kafka" $kafkaSpec "admin" $adminSpec "schemaRegistry" $schemaRegistrySpec))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

