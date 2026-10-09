{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/serviceaccount.go" */ -}}

{{- define "redpanda.ServiceAccountName" -}}
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
{{- (dict "r" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) | toJson -}}
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

{{- define "redpanda.ServiceAccount" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.serviceAccount.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.ServiceAccount" (dict "a" (list (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r") $state.Release.Namespace (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") (merge (dict) $state.Values.serviceAccount.annotations (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r")))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

