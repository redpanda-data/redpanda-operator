{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/secret.go" */ -}}

{{- define "console.Secret" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.secret.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "namespace" $state.Namespace)) "type" "Opaque" "stringData" (dict "kafka-sasl-password" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.kafka.saslPassword "")))) "r") "kafka-sasl-aws-msk-iam-secret-key" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.kafka.awsMskIamSecretKey "")))) "r") "kafka-tls-ca" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.kafka.tlsCa "")))) "r") "kafka-tls-cert" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.kafka.tlsCert "")))) "r") "kafka-tls-key" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.kafka.tlsKey "")))) "r") "schema-registry-bearertoken" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.schemaRegistry.bearerToken "")))) "r") "schema-registry-password" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.schemaRegistry.password "")))) "r") "schemaregistry-tls-ca" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.schemaRegistry.tlsCa "")))) "r") "schemaregistry-tls-cert" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.schemaRegistry.tlsCert "")))) "r") "schemaregistry-tls-key" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.schemaRegistry.tlsKey "")))) "r") "authentication-jwt-signingkey" $state.Values.secret.authentication.jwtSigningKey "authentication-oidc-client-secret" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.authentication.oidc.clientSecret "")))) "r") "license" $state.Values.secret.license "redpanda-admin-api-password" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.redpanda.adminApi.password "")))) "r") "redpanda-admin-api-tls-ca" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.redpanda.adminApi.tlsCa "")))) "r") "redpanda-admin-api-tls-cert" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.redpanda.adminApi.tlsCert "")))) "r") "redpanda-admin-api-tls-key" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.redpanda.adminApi.tlsKey "")))) "r") "serde-protobuf-git-basicauth-password" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.secret.serde.protobufGitBasicAuthPassword "")))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

