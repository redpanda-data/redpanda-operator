{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/serviceaccount.go" */ -}}

{{- define "console.ServiceAccountName" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if $state.Values.serviceAccount.create -}}
{{- if (ne $state.Values.serviceAccount.name "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.Values.serviceAccount.name) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (default "default" $state.Values.serviceAccount.name)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.ServiceAccount" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.serviceAccount.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "ServiceAccount" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "namespace" $state.Namespace "annotations" $state.Values.serviceAccount.annotations)) "automountServiceAccountToken" $state.Values.serviceAccount.automountServiceAccountToken))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

