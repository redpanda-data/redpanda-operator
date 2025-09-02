{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/serviceaccount.go" */ -}}

{{- define "redpandav25.ServiceAccountName" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $serviceAccount := $state.Values.serviceAccount -}}
{{- if (and $serviceAccount.create (ne $serviceAccount.name "")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $serviceAccount.name) | toJson -}}
{{- break -}}
{{- else -}}{{- if $serviceAccount.create -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpandav25.Fullname" (dict "a" (list $state)))) "r")) | toJson -}}
{{- break -}}
{{- else -}}{{- if (ne $serviceAccount.name "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $serviceAccount.name) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "default") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpandav25.ServiceAccount" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.serviceAccount.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "ServiceAccount")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "redpandav25.ServiceAccountName" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpandav25.FullLabels" (dict "a" (list $state)))) "r") "annotations" $state.Values.serviceAccount.annotations)) "automountServiceAccountToken" false))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

